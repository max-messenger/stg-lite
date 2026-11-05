package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"lite/pkg/telemetry"
)

const (
	writeStatsPeriod = 10 * time.Second
)

const (
	metricLabelAddr   = "addr"
	metricLabelAction = "action"
	metricLabelError  = "error"
)

type BeforeConnectCallback func(ctx context.Context, cc *pgx.ConnConfig) error
type AfterConnectCallback func(ctx context.Context, c *pgx.Conn) error

type Postgres struct {
	logger *zap.Logger

	beforeConnectCallbacks []BeforeConnectCallback
	afterConnectCallbacks  []AfterConnectCallback

	pool *pgxpool.Pool

	addr string
}

func NewPostgres(
	ctx context.Context,
	cfg Config,
	logger *zap.Logger,
	opts ...Option,
) (*Postgres, error) {

	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}

	pg := &Postgres{
		logger: logger,
	}
	for _, opt := range opts {
		opt(pg)
	}

	poolCfg.BeforeConnect = pg.beforeConnect
	poolCfg.AfterConnect = pg.afterConnect
	poolCfg.ConnConfig.Tracer = otelpgx.NewTracer()

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, err
	}

	pg.pool = pool
	pg.addr = pool.Config().ConnConfig.Host

	go pg.reportStats()

	return pg, nil
}

func (p *Postgres) callRaw(
	ctx context.Context,
	callFunc func(ctx context.Context, conn *pgx.Conn) error,
) (addr string, err error) {
	con, err := p.pool.Acquire(ctx)
	if err != nil {
		return "", err
	}

	addr = con.Conn().PgConn().Conn().RemoteAddr().String()
	defer con.Release()

	return addr, callFunc(ctx, con.Conn())
}

func (p *Postgres) runInTxRaw(
	ctx context.Context,
	name string,
	callFunc func(ctx context.Context, tx *pgx.Conn) error,
	txOptions pgx.TxOptions,
) (addr string, err error) {
	con, err := p.pool.Acquire(ctx)
	if err != nil {
		return "", err
	}

	addr = con.Conn().PgConn().Conn().RemoteAddr().String()

	defer con.Release()

	tx, err := con.Conn().BeginTx(ctx, txOptions)
	if err != nil {
		return addr, err
	}

	rollback := func() {
		if rErr := tx.Rollback(ctx); rErr != nil {
			p.logger.Error(
				"failed to rollback transaction", zap.Error(rErr), zap.String("name", name),
			)
		}
	}

	defer func() {
		if r := recover(); r != nil {
			p.logger.Error("panic", zap.Any("panic", r))

			rollback()

			switch x := r.(type) {
			case error:
				err = fmt.Errorf("panic recovered: %w", x)
			default:
				err = fmt.Errorf("panic recovered: %v", x)
			}
		}
	}()

	err = callFunc(ctx, tx.Conn())

	if err != nil {
		rollback()

		return addr, err
	}

	return addr, tx.Commit(ctx)
}

func (p *Postgres) Call(
	ctx context.Context,
	name string,
	callFunc func(ctx context.Context, conn *pgx.Conn) error,
) (err error) {
	var addr string

	ctx, span := otel.Tracer("postgres").Start(ctx, "call", trace.WithAttributes(
		attribute.String("postgres.query.name", name),
		attribute.String("postgres.query.type", "call"),
	))
	defer span.End()

	defer func(ts time.Time) {
		duration.With(prometheus.Labels{
			metricLabelAddr:   p.addr,
			metricLabelAction: name,
			metricLabelError:  telemetry.ErrLabelValue(err),
		}).Observe(time.Since(ts).Seconds())

		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			span.RecordError(err)
		}
	}(time.Now())

	addr, err = p.callRaw(ctx, callFunc)
	span.SetAttributes(attribute.String("postgres.addr", addr))

	return err
}

func (p *Postgres) RunInTx(
	ctx context.Context,
	name string,
	callFunc func(ctx context.Context, conn *pgx.Conn) error,
	txOptions pgx.TxOptions,
) (err error) {
	var addr string
	ctx, span := otel.Tracer("postgres").Start(ctx, "tx", trace.WithAttributes(
		attribute.String("postgres.query.name", name),
		attribute.String("postgres.query.type", "tx"),
	))
	defer span.End()

	defer func(ts time.Time) {
		duration.With(prometheus.Labels{
			metricLabelAddr:   addr,
			metricLabelAction: name,
			metricLabelError:  telemetry.ErrLabelValue(err),
		}).Observe(time.Since(ts).Seconds())

		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			span.RecordError(err)
		}
	}(time.Now())

	addr, err = p.runInTxRaw(ctx, name, callFunc, txOptions)
	span.SetAttributes(attribute.String("postgres.addr", addr))

	return err
}
func (p *Postgres) RawPool() *pgxpool.Pool {
	return p.pool
}

func (p *Postgres) Stop(_ context.Context) error {
	p.pool.Close()

	return nil
}

func (p *Postgres) Ping(ctx context.Context) error {
	return p.pool.Ping(ctx)
}

func (p *Postgres) beforeConnect(ctx context.Context, cc *pgx.ConnConfig) error {
	for _, callback := range p.beforeConnectCallbacks {
		if err := callback(ctx, cc); err != nil {
			return err
		}
	}

	return nil
}

func (p *Postgres) afterConnect(ctx context.Context, c *pgx.Conn) error {
	for _, callback := range p.afterConnectCallbacks {
		if err := callback(ctx, c); err != nil {
			return err
		}
	}

	return nil
}

func (p *Postgres) reportStats() {
	for range time.NewTicker(writeStatsPeriod).C {
		if p.pool == nil {
			return // pool is not started
		}

		stat := p.pool.Stat()

		connections.WithLabelValues(p.addr, "max").Set(float64(stat.MaxConns()))
		connections.WithLabelValues(p.addr, "acquired").Set(float64(stat.AcquiredConns()))
		connections.WithLabelValues(p.addr, "idle").Set(float64(stat.IdleConns()))
		connections.WithLabelValues(p.addr, "total").Set(float64(stat.TotalConns()))

	}
}

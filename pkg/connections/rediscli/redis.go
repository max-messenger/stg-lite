package rediscli

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
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

type FormatKey func(key string) string

type CallCallback func(
	ctx context.Context,
	keyFormatter FormatKey,
	ucl redis.UniversalClient,
) error

type Redis struct {
	name   string
	prefix string
	logger *zap.Logger
	rd     redis.UniversalClient
}

func NewRedis(
	logger *zap.Logger,
	cfg Config,
	name string,
) (*Redis, error) {
	redisClient := redis.NewUniversalClient(&redis.UniversalOptions{
		Addrs:      cfg.Addrs,
		ClientName: name,
		MasterName: cfg.MasterName,
		Username:   cfg.Username,
		Password:   cfg.Password,

		MaxRetries:      cfg.MaxRetries,
		MinRetryBackoff: cfg.MinRetryBackoff,
		MaxRetryBackoff: cfg.MaxRetryBackoff,

		MaxRedirects: cfg.MaxRedirects,

		DialTimeout:  cfg.DialTimeout,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,

		PoolSize:        cfg.PoolSize,
		PoolTimeout:     cfg.PoolTimeout,
		MinIdleConns:    cfg.MinIdleConns,
		MaxIdleConns:    cfg.MaxIdleConns,
		MaxActiveConns:  cfg.MaxActiveConns,
		ConnMaxIdleTime: cfg.ConnMaxIdleTime,
		ConnMaxLifetime: cfg.ConnMaxLifetime,

		ReadOnly: true,

		RouteRandomly:  cfg.RouteRandomly,
		RouteByLatency: cfg.RouteByLatency,
	})

	redisClient.AddHook(newMetricsHook(name)) // metrics hook

	r := &Redis{
		logger: logger,
		name:   name,
		prefix: cfg.KeyPrefix,

		rd: redisClient,
	}

	go r.reportStats()

	return r, nil
}

func (r *Redis) formatKey(key string) string {
	if r.prefix == "" {
		return key
	}

	return fmt.Sprintf("%s:%s", r.prefix, key)
}

func (r *Redis) Call(
	ctx context.Context, name string, callFunc CallCallback,
) (err error) {
	return r.call(ctx, name, callFunc)
}

func (r *Redis) call(
	ctx context.Context, name string, callFunc CallCallback,
) (err error) {

	ctx, span := otel.Tracer("redis").Start(ctx, "call", trace.WithAttributes(
		attribute.String("redis.query.name", name),
		attribute.String("redis.pool.name", r.name),
	))
	defer span.End()

	defer func(ts time.Time) {
		duration.WithLabelValues(
			r.name,
			name,
			telemetry.ErrLabelValue(err),
		).Observe(time.Since(ts).Seconds())

		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			span.RecordError(err)
		}
	}(time.Now())

	err = callFunc(ctx, r.formatKey, r.rd)
	if err != nil {
		return err
	}

	return nil
}

func (r *Redis) Stop(_ context.Context) error {
	return r.rd.Close()
}

func (r *Redis) Ping(ctx context.Context) error {
	return r.rd.Ping(ctx).Err()
}

func (r *Redis) reportStats() {
	for range time.NewTicker(writeStatsPeriod).C {
		stats := r.rd.PoolStats()
		connectionsCall.WithLabelValues(r.name, "hits").Set(float64(stats.Hits))
		connectionsCall.WithLabelValues(r.name, "misses").Set(float64(stats.Misses))
		connectionsCall.WithLabelValues(r.name, "timeouts").Set(float64(stats.Timeouts))

		connections.WithLabelValues(r.name, "idle").Set(float64(stats.IdleConns))
		connections.WithLabelValues(r.name, "stale").Set(float64(stats.StaleConns))
		connections.WithLabelValues(r.name, "total").Set(float64(stats.TotalConns))
	}
}

package app

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/max-messenger/max-bot-api-client-go/v2/model"
	"go.uber.org/zap"

	"lite/internal"
	"lite/internal/app/clients/maxbot"
	"lite/internal/app/router"
	"lite/pkg/connections/postgres"
	"lite/pkg/connections/rediscli"
	"lite/pkg/grace"
	httpclient "lite/pkg/http/client"
	"lite/pkg/logger"
	"lite/pkg/server"
	"lite/pkg/telemetry"
)

const (
	shutdownTimeout = 15 * time.Second
)

type App struct {
	logger  *zap.Logger
	servers *server.Servers
	maxbot  *maxbot.Client
	grace   *grace.Grace
}

// New creates a new application.
// nolint: cyclop
func New(ctx context.Context, name string, configFile string) (*App, error) {
	cfg, err := NewConfig(configFile)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}

	log, err := logger.NewLogger(cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("logger: %w", err)
	}

	log.Info("Starting application",
		zap.String("name", name),
		zap.String("hostname", cfg.App.Hostname),
		zap.String("version", internal.CommitTag),
		zap.String("commit", internal.CommitShortSHA),
	)

	graceful := grace.New(log.Named("grace"))
	defer func() {
		if err != nil {
			log.Error("Application failed", zap.Error(err))
			graceful.Stop(context.Background())
		}
	}()

	tele, err := initTelemetry(name, cfg, log.Named("telemetry"))
	if err != nil {
		return nil, err
	}
	graceful.AddPriority(tele, grace.PriorityLowest)

	pgSel, err := postgres.NewPostgres(ctx, cfg.PostgresSel, log.Named("postgres-sel"))
	if err != nil {
		return nil, fmt.Errorf("postgres-sel: %w", err)
	}

	if err = pgSel.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres-sel: %w", err)
	}

	graceful.AddPriority(pgSel, grace.PriorityLow)

	pgUpd, err := postgres.NewPostgres(ctx, cfg.PostgresUpd, log.Named("postgres-upd"))
	if err != nil {
		return nil, fmt.Errorf("postgres-upd: %w", err)
	}

	if err = pgUpd.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres-upd: %w", err)
	}

	graceful.AddPriority(pgUpd, grace.PriorityLow)

	redisCli, err := rediscli.NewRedis(log.Named("redis"), cfg.Redis.Prepare(), name)
	if err != nil {
		return nil, fmt.Errorf("redis: %w", err)
	}

	if err = redisCli.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	graceful.AddPriority(redisCli, grace.PriorityLow)

	maxClient, maxbotCtrl, err := initMaxbot(
		cfg.Maxbot, log.Named("maxbot"),
		func(_ context.Context, update model.Update) {
			log.Info("update", zap.Any("update", update))
		},
	)
	if err != nil {
		return nil, fmt.Errorf("maxbot: %w", err)
	}

	graceful.Add(maxClient)

	handler, err := router.NewRouter(log.Named("router"), func(r chi.Router) {
		r.Get("/", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("IT'S ALIVE")) // nolint:errcheck
		})
	}, maxbotCtrl)
	if err != nil {
		return nil, fmt.Errorf("router: %w", err)
	}

	httpServer, err := server.NewHTTPServer(log.Named("system"), cfg.Servers.HTTP, handler)
	if err != nil {
		return nil, fmt.Errorf("http server: %w", err)
	}

	systemServer := server.NewSystemServer(log, cfg.Servers.System)

	servers := server.NewServersProvider(
		httpServer, systemServer,
	)

	graceful.Add(servers)

	// КОД ПИСАТЬ СЮДА!

	return &App{
		logger:  log,
		servers: servers,
		maxbot:  maxClient,
		grace:   graceful,
	}, nil
}

func (a *App) Run() error {

	if err := a.servers.Start(context.Background()); err != nil {
		return fmt.Errorf("start servers: %w", err)
	}
	if err := a.maxbot.Start(context.Background()); err != nil {
		return fmt.Errorf("start maxbot client: %w", err)
	}

	a.logger.Info("Application started")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh

	a.logger.Info("Received signal, shutting down", zap.String("signal", sig.String()))

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	a.grace.Stop(ctx)

	a.logger.Info("Application stopped")
	a.logger.Sync() //nolint:errcheck

	return nil
}

func initMaxbot(
	cfg maxbot.Config,
	log *zap.Logger,
	handler func(ctx context.Context, update model.Update),
) (*maxbot.Client, router.Controller, error) {
	httpCli := httpclient.New("maxbot", httpclient.WithLogger(log.Named("http")))

	client, err := maxbot.New(log, cfg, httpCli)
	if err != nil {
		return nil, nil, fmt.Errorf("maxbot client: %w", err)
	}

	return client, func(r chi.Router) {
		r.Post(client.UpdateHandler(handler))
	}, nil
}

func initTelemetry(name string, cfg Config, log *zap.Logger) (*telemetry.Telemetry, error) {
	teleCfg := cfg.Telemetry
	teleCfg.ServiceName = name
	teleCfg.Hostname = cfg.App.Hostname
	teleCfg.Version = internal.CommitTag

	tele, err := telemetry.NewTelemetry(teleCfg, log)
	if err != nil {
		return nil, fmt.Errorf("telemetry: %w", err)
	}

	return tele, nil
}

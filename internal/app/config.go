package app

import (
	"fmt"
	"os"

	"go.uber.org/config"

	"lite/internal/app/clients/maxbot"
	"lite/pkg/connections/postgres"
	"lite/pkg/connections/rediscli"
	"lite/pkg/logger"
	"lite/pkg/server"
	"lite/pkg/telemetry"
)

type (
	ServersConfig struct {
		HTTP   server.Config `yaml:"http"`
		System server.Config `yaml:"system"`
	}

	MigrateConfig struct {
		PoolName string `yaml:"pool_name"`
	}

	Config struct {
		App struct {
			Hostname string `yaml:"hostname"`
		} `yaml:"app"`
		Logger      logger.Config    `yaml:"logger"`
		Servers     ServersConfig    `yaml:"servers"`
		Telemetry   telemetry.Config `yaml:"telemetry"`
		PostgresSel postgres.Config  `yaml:"postgres-sel"` //nolint:tagliatelle
		PostgresUpd postgres.Config  `yaml:"postgres-upd"` //nolint:tagliatelle
		Redis       rediscli.Config  `yaml:"redis"`
		Migrate     MigrateConfig    `yaml:"migrate"`
		Maxbot      maxbot.Config    `yaml:"maxbot"`
	}
)

func NewConfig(filePath string) (Config, error) {
	provider, err := config.NewYAML(
		config.File(filePath),
		config.Expand(os.LookupEnv),
		config.Permissive(),
	)
	if err != nil {
		return Config{}, fmt.Errorf("load config %q: %w", filePath, err)
	}

	var cfg Config
	if err := provider.Get(config.Root).Populate(&cfg); err != nil {
		return Config{}, fmt.Errorf("populate config: %w", err)
	}

	return cfg, nil
}

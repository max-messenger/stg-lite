package migrate

import (
	"database/sql"
	"fmt"

	"github.com/jackc/pgx/v5/stdlib"
	sqlmigrate "github.com/rubenv/sql-migrate"
	"go.uber.org/zap"

	"lite/db"
	"lite/pkg/connections/postgres"
)

type Migrations struct {
	logger *zap.Logger

	pgConn *postgres.Postgres
}

func New(logger *zap.Logger, pgConn *postgres.Postgres) *Migrations {
	return &Migrations{
		logger: logger,
		pgConn: pgConn,
	}
}

func (m *Migrations) Up() error {
	m.logger.Info("Starting migrations")
	var err error

	sqlDB := stdlib.OpenDBFromPool(m.pgConn.RawPool())
	defer func() {
		err = sqlDB.Close()
		if err != nil {
			m.logger.Warn("Failed to close database connection", zap.Error(err))
		}
	}()

	if err = m.runMigrations(sqlDB); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	m.logger.Info("Migrations completed")

	return nil
}

func (m *Migrations) runMigrations(dbConnection *sql.DB) error {
	migrationsList := &sqlmigrate.EmbedFileSystemMigrationSource{
		FileSystem: db.Migrations,
		Root:       "migrations",
	}

	n, err := sqlmigrate.Exec(dbConnection, "postgres", migrationsList, sqlmigrate.Up)
	if err != nil {
		return fmt.Errorf("pg migrate error: %w", err)
	}

	m.logger.Info("Migrations completed", zap.Int("n", n))

	return nil
}

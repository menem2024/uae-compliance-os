package db

import (
	"context"
	"database/sql"
	"embed"
	"io/fs"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Migrate applies all pending migrations. ownerURL must connect as compliance_owner.
func Migrate(ctx context.Context, ownerURL string) error {
	sqldb, err := sql.Open("pgx", ownerURL)
	if err != nil {
		return err
	}
	defer func() { _ = sqldb.Close() }()
	dir, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return err
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, sqldb, dir)
	if err != nil {
		return err
	}
	_, err = provider.Up(ctx)
	return err
}

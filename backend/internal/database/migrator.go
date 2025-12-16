package database

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"net"
	"net/url"
	"strconv"

	"github.com/deppfellow/go-boilerplate/internal/config"
	"github.com/jackc/pgx/v5"
	tern "github.com/jackc/tern/v2/migrate"
	"github.com/rs/zerolog"
)

// Embed all SQL files under migrations/ at compile time.
// This means your binary carries migrations inside it.
// You do not depend on the filesystem at runtime (nice for containers).
//
//go:embed migrations/*.sql
var migrations embed.FS

// Migrate runs database migrations using jackc/tern.
//
// Behavior:
//   - Build DSN from config
//   - Connect using pgx (single connection, not a pool)
//   - Create tern migrator and load embedded migrations
//   - Run migrations to latest
//   - Log whether it was already up-to-date or migrated
func Migrate(ctx context.Context, logger *zerolog.Logger, cfg *config.Config) error {
	hostPort := net.JoinHostPort(cfg.Database.Host, strconv.Itoa(cfg.Database.Port))

	// URL-encode the password to keep DSN valid.
	encodedPassword := url.QueryEscape(cfg.Database.Password)

	dsn := fmt.Sprintf("postgres://%s:%s@%s/%s?sslmode=%s",
		cfg.Database.User,
		encodedPassword,
		hostPort,
		cfg.Database.Name,
		cfg.Database.SSLMode,
	)

	// Open a direct connection for migrations.
	// Using a single connection avoids pool complexity for a one-time action.
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)

	// Create a migrator that stores migration version in the schema_version table.
	m, err := tern.NewMigrator(ctx, conn, "schema_version")
	if err != nil {
		return fmt.Errorf("constructing database migrator: %w", err)
	}

	// Get a subtree view starting at "migrations" directory within the embedded FS.
	// tern expects an fs.FS pointing at the directory containing migration files.
	subtree, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return fmt.Errorf("retrieving database migrations subtree: %w", err)
	}

	// Load migrations from the embedded filesystem.
	// tern parses filenames and orders them.
	if err := m.LoadMigrations(subtree); err != nil {
		return fmt.Errorf("loading database migrations: %w", err)
	}

	// Read current version from schema_version.
	// `from` is the version number already applied.
	from, err := m.GetCurrentVersion(ctx)
	if err != nil {
		return fmt.Errorf("retrieving current database migration version: %w", err)
	}

	// Apply migrations up to latest.
	if err := m.Migrate(ctx); err != nil {
		return err
	}

	// Log outcome:
	// If current version equals number of migrations loaded, nothing changed.
	if from == int32(len(m.Migrations)) {
		logger.Info().Msgf("database schema up to date, version %d", len(m.Migrations))
	} else {
		logger.Info().Msgf("migrated database schema, from %d to %d", from, len(m.Migrations))
	}
	return nil
}

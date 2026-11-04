//go:build integration

package tests

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	tcmysql "github.com/testcontainers/testcontainers-go/modules/mysql"
)

// integrationDSN is resolved once for the whole integration package by TestMain:
// either from BNPL_TEST_DSN/DB_DSN, or by starting a throwaway MySQL 8
// container via testcontainers.
var integrationDSN string

func TestMain(m *testing.M) {
	ctx := context.Background()

	dsn := os.Getenv("BNPL_TEST_DSN")
	if dsn == "" {
		dsn = os.Getenv("DB_DSN")
	}
	if dsn != "" {
		integrationDSN = dsn
		if err := applyMigrations(ctx, dsn); err != nil {
			fmt.Fprintf(os.Stderr, "apply migrations: %v\n", err)
			os.Exit(1)
		}
		os.Exit(m.Run())
	}

	container, err := tcmysql.Run(ctx, "mysql:8.0",
		tcmysql.WithDatabase("bnpl"),
		tcmysql.WithUsername("bnpl"),
		tcmysql.WithPassword("bnplpw"),
		testcontainers.WithCmd(
			"--transaction-isolation=READ-COMMITTED",
			"--character-set-server=utf8mb4",
			"--collation-server=utf8mb4_0900_ai_ci",
			// Required so migration 000002's append-only guard triggers can be
			// created with binary logging enabled (see IMPLEMENTATION_PROGRESS).
			"--log-bin-trust-function-creators=1",
		),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "integration tests skipped: cannot start MySQL container: %v\n", err)
		os.Exit(0)
	}

	host, err := container.Host(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve container host: %v\n", err)
		_ = container.Terminate(ctx)
		os.Exit(1)
	}
	port, err := container.MappedPort(ctx, "3306/tcp")
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve container port: %v\n", err)
		_ = container.Terminate(ctx)
		os.Exit(1)
	}

	integrationDSN = fmt.Sprintf(
		"bnpl:bnplpw@tcp(%s:%s)/bnpl?parseTime=true&loc=UTC&multiStatements=true",
		host, port.Port(),
	)
	if err := applyMigrations(ctx, integrationDSN); err != nil {
		fmt.Fprintf(os.Stderr, "apply migrations: %v\n", err)
		_ = container.Terminate(ctx)
		os.Exit(1)
	}

	code := m.Run()
	_ = container.Terminate(ctx)
	os.Exit(code)
}

// applyMigrations runs the golang-migrate up files in order when the schema is
// absent, so a freshly started container (or an empty database) is usable. It is
// a no-op when the database is already migrated (e.g. a local dev DB, or CI when
// `make migrate-up` ran first). Applying the files requires multiStatements=true
// in the DSN; the testcontainers DSN and CI's BNPL_TEST_DSN set it.
func applyMigrations(ctx context.Context, dsn string) error {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("open migration connection: %w", err)
	}
	defer func() { _ = db.Close() }()

	var existing string
	err = db.QueryRowContext(ctx, "SHOW TABLES LIKE 'customers'").Scan(&existing)
	switch {
	case err == nil:
		return nil
	case !errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("checking schema: %w", err)
	}

	files, err := filepath.Glob(filepath.Join("..", "migrations", "*.up.sql"))
	if err != nil {
		return fmt.Errorf("locating migrations: %w", err)
	}
	sort.Strings(files)
	for _, file := range files {
		contents, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("reading %s: %w", file, err)
		}
		if _, err := db.ExecContext(ctx, string(contents)); err != nil {
			return fmt.Errorf("applying %s: %w", file, err)
		}
	}
	return nil
}

//go:build integration

package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	pg "github.com/testcontainers/testcontainers-go/modules/postgres"
)

var pool *pgxpool.Pool

func TestMain(m *testing.M) {
	code, err := run(m)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error running test: %s\n", err)
		os.Exit(1)
	}

	os.Exit(code)
}

func run(m *testing.M) (int, error) {
	ctx := context.Background()

	container, err := pg.Run(ctx, "postgres:17",
		pg.WithDatabase("test"),
		pg.WithUsername("test"),
		pg.WithPassword("test"),
		pg.BasicWaitStrategies())

	if err != nil {
		return 1, fmt.Errorf("failed to start postgres: %w", err)
	}

	defer func(container testcontainers.Container) {
		_ = testcontainers.TerminateContainer(container)
	}(container)

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return 1, fmt.Errorf("failed to get database connection string: %w", err)
	}

	migration, err := migrate.New("file://../../../migrations", dsn)
	if err != nil {
		return 1, fmt.Errorf("failed to create migration instance: %w", err)
	}

	if err := migration.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return 1, fmt.Errorf("failed to run migration: %w", err)
	}

	pool, err = pgxpool.New(ctx, dsn)
	if err != nil {
		return 1, fmt.Errorf("failed to connect to postgres: %w", err)
	}

	defer pool.Close()

	return m.Run(), nil
}

func truncate(t *testing.T) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`TRUNCATE endpoints, subscriptions, events, deliveries CASCADE`)

	require.NoError(t, err)
}

func insertEndpoint(t *testing.T, url, secret string) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	_, err := pool.Exec(context.Background(), `INSERT INTO endpoints(id, url, secret) VALUES ($1, $2, $3)`, id, url, secret)
	require.NoError(t, err)
	return id
}

func insertEvent(t *testing.T) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	_, err := pool.Exec(context.Background(), `INSERT INTO events(id, type, payload) VALUES ($1, 'order.created', '{"a":1}')`, id)
	require.NoError(t, err)
	return id
}

func insertDelivery(t *testing.T, endpointID uuid.UUID, status string, nextRetryAt time.Time) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	_, err := pool.Exec(context.Background(), `
		INSERT INTO deliveries (id, endpoint_id, event_id, status, max_attempts, next_retry_at)
		VALUES ($1, $2, $3, $4, 5, $5)`,
		id, endpointID, insertEvent(t), status, nextRetryAt)
	require.NoError(t, err)
	return id
}
func insertDeliveryForEvent(t *testing.T, endpointID, eventID uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	_, err := pool.Exec(context.Background(), `
		INSERT INTO deliveries (id, endpoint_id, event_id, max_attempts)
		VALUES ($1, $2, $3, 5)`,
		id, endpointID, eventID)
	require.NoError(t, err)
	return id
}

func subscribe(t *testing.T, endpointID uuid.UUID, eventType string) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	_, err := pool.Exec(context.Background(),
		`INSERT INTO subscriptions(id, endpoint_id, event_type) VALUES ($1, $2, $3)`, id, endpointID, eventType)
	require.NoError(t, err)
	return id
}

func deactivateEndpoint(t *testing.T, endpointID uuid.UUID) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `UPDATE endpoints SET is_active = false WHERE id = $1`, endpointID)
	require.NoError(t, err)
}

func countRows(t *testing.T, table string) int {
	t.Helper()
	var count int
	err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM `+table).Scan(&count)
	require.NoError(t, err)
	return count
}

//go:build integration
package pg

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"
	"webhook-delivery/internal/domain"
	"webhook-delivery/internal/domain/dto"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
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

	container, err := postgres.Run(ctx, "postgres:17",
		postgres.WithDatabase("test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		postgres.BasicWaitStrategies())

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

func TestDeliveries_ClaimPending_OnlyDue(t *testing.T) {
	truncate(t)
	const url = "https://test.com"

	ctx := context.Background()
	repo := NewDeliveries(pool, slog.New(slog.DiscardHandler))

	ep := insertEndpoint(t, url, "whsec")
	due := insertDelivery(t, ep, "pending", time.Now().Add(-time.Minute))
	insertDelivery(t, ep, "pending", time.Now().Add(time.Hour))
	insertDelivery(t, ep, "delivered", time.Now().Add(-time.Minute))

	claimed, err := repo.ClaimPending(ctx, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.Equal(t, due, claimed[0].ID)
	require.Equal(t, url, claimed[0].URL)

	got, err := repo.GetByID(ctx, due)
	require.NoError(t, err)
	require.Equal(t, domain.StatusQueued, got.Status)
}

func TestDeliveries_ClaimPending_WithExpired(t *testing.T) {
	truncate(t)
	const url = "https://test.com"

	ctx := context.Background()
	repo := NewDeliveries(pool, slog.New(slog.DiscardHandler))

	ep := insertEndpoint(t, url, "whsec")
	insertDelivery(t, ep, "pending", time.Now().Add(time.Minute))
	insertDelivery(t, ep, "failed", time.Now().Add(-time.Hour))
	insertDelivery(t, ep, "delivered", time.Now().Add(-time.Minute))

	claimed, err := repo.ClaimPending(ctx, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 0)
}

func TestDeliveries_ClaimPending_WithLimit(t *testing.T) {
	truncate(t)
	const url = "https://test.com"

	ctx := context.Background()
	repo := NewDeliveries(pool, slog.New(slog.DiscardHandler))

	ep := insertEndpoint(t, url, "whsec")
	for range 8 {
		insertDelivery(t, ep, "pending", time.Now().Add(-time.Minute))
	}

	claimed, err := repo.ClaimPending(ctx, 5)
	require.NoError(t, err)
	require.Len(t, claimed, 5)

	for _, claimed := range claimed {
		require.Equal(t, url, claimed.URL)
		require.Equal(t, ep, claimed.EndpointID)

		got, err := repo.GetByID(ctx, claimed.ID)
		require.NoError(t, err)
		require.Equal(t, domain.StatusQueued, got.Status)
	}
}

func TestDeliveries_RecoverProcessing(t *testing.T) {
	truncate(t)
	const url = "https://test.com"

	ctx := context.Background()
	repo := NewDeliveries(pool, slog.New(slog.DiscardHandler))

	ep := insertEndpoint(t, url, "whsec")

	statuses := []domain.DeliveryStatus{domain.StatusPending, domain.StatusFailed, domain.StatusProcessing, domain.StatusDelivered, domain.StatusQueued}
	ids := make([]uuid.UUID, 0, len(statuses))

	now := time.Now()
	for _, status := range statuses {
		ids = append(ids, insertDelivery(t, ep, string(status), now.Add(-time.Minute)))
	}

	err := repo.RecoverProcessing(ctx)
	require.NoError(t, err)

	for index := 0; index < len(ids); index++ {
		got, err := repo.GetByID(ctx, ids[index])
		require.NoError(t, err)

		if statuses[index] == domain.StatusFailed || statuses[index] == domain.StatusDelivered {
			require.Equal(t, statuses[index], got.Status)
			continue
		}

		require.Equal(t, domain.StatusPending, got.Status)
	}
}

func TestDeliveries_UpdateStatus_Success(t *testing.T) {
	truncate(t)
	const url = "https://test.com"

	ctx := context.Background()
	repo := NewDeliveries(pool, slog.New(slog.DiscardHandler))

	ep := insertEndpoint(t, url, "whsec")
	del := insertDelivery(t, ep, "processing", time.Now().Add(-time.Minute))

	notAffectedTime := time.Now().Add(-time.Hour)
	notAffected := insertDelivery(t, ep, "failed", notAffectedTime)

	cmd := dto.UpdateDeliveryStatusCommand{
		ID:               del,
		Status:           domain.StatusPending,
		Attempts:         2,
		NextRetryAt:      time.Now().Add(time.Minute),
		LastError:        new("test error"),
		LastResponseCode: nil,
	}

	err := repo.UpdateStatus(ctx, cmd)
	require.NoError(t, err)

	got, err := repo.GetByID(ctx, notAffected)
	require.NoError(t, err)
	require.Equal(t, domain.StatusFailed, got.Status)
	require.Equal(t, ep, got.EndpointID)
	require.WithinDuration(t, notAffectedTime, got.NextRetryAt, time.Nanosecond)

	got, err = repo.GetByID(ctx, del)
	require.NoError(t, err)

	require.Equal(t, cmd.ID, got.ID)
	require.Equal(t, ep, got.EndpointID)
	require.Equal(t, cmd.Status, got.Status)
	require.Equal(t, cmd.Attempts, got.Attempts)
	require.WithinDuration(t, cmd.NextRetryAt, got.NextRetryAt, time.Nanosecond)
	require.Equal(t, cmd.LastError, got.LastError)
	require.Equal(t, cmd.LastResponseCode, got.LastResponseCode)
}

func TestDeliveries_UpdateStatus_NotFound(t *testing.T) {
	truncate(t)
	const url = "https://test.com"

	ctx := context.Background()
	repo := NewDeliveries(pool, slog.New(slog.DiscardHandler))

	ep := insertEndpoint(t, url, "whsec")
	delTime := time.Now().Add(-time.Hour)
	del := insertDelivery(t, ep, "failed", delTime)

	cmd := dto.UpdateDeliveryStatusCommand{
		ID: uuid.Must(uuid.NewV7()),
		Status: domain.StatusPending,
		NextRetryAt: time.Now().Add(time.Minute),
	}

	err := repo.UpdateStatus(ctx, cmd)
	require.Error(t, err)
	require.ErrorIs(t, err, domain.ErrDeliveryNotFound)

	got, err := repo.GetByID(ctx, del)
	require.NoError(t, err)
	require.Equal(t, domain.StatusFailed, got.Status)
	require.Equal(t, ep, got.EndpointID)
	require.WithinDuration(t, delTime, got.NextRetryAt, time.Nanosecond)
}

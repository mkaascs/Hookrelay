//go:build integration

package pg

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"webhook-delivery/internal/domain"
	"webhook-delivery/internal/domain/dto"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestEvents_PublishEventWithDeliveries_OnlyActiveSubscribed(t *testing.T) {
	truncate(t)

	ctx := context.Background()
	repo := NewEvents(pool, slog.New(slog.DiscardHandler), 7)

	subscribed := insertEndpoint(t, "https://subscribed.com", "whsec")
	subscribe(t, subscribed, "order.created")

	inactive := insertEndpoint(t, "https://inactive.com", "whsec")
	subscribe(t, inactive, "order.created")
	deactivateEndpoint(t, inactive)

	otherType := insertEndpoint(t, "https://other.com", "whsec")
	subscribe(t, otherType, "order.paid")

	cmd := dto.PublishEventCommand{Type: "order.created", Payload: json.RawMessage(`{"order_id":42}`)}

	event, count, err := repo.PublishEventWithDeliveries(ctx, cmd)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.Equal(t, cmd.Type, event.Type)
	require.NotEqual(t, uuid.Nil, event.ID)
	require.False(t, event.CreatedAt.IsZero())

	var (
		endpointID  uuid.UUID
		status      domain.DeliveryStatus
		attempts    int
		maxAttempts int
	)

	err = pool.QueryRow(ctx, `
		SELECT endpoint_id, status, attempts, max_attempts
		FROM deliveries
		WHERE event_id = $1`, event.ID).
		Scan(&endpointID, &status, &attempts, &maxAttempts)

	require.NoError(t, err)
	require.Equal(t, subscribed, endpointID)
	require.Equal(t, domain.StatusPending, status)
	require.Zero(t, attempts)
	require.Equal(t, 7, maxAttempts)
	require.Equal(t, 1, countRows(t, "deliveries"))
}

func TestEvents_PublishEventWithDeliveries_NoSubscribers(t *testing.T) {
	truncate(t)

	ctx := context.Background()
	repo := NewEvents(pool, slog.New(slog.DiscardHandler), 5)

	event, count, err := repo.PublishEventWithDeliveries(ctx, dto.PublishEventCommand{
		Type:    "order.created",
		Payload: json.RawMessage(`{}`),
	})

	require.NoError(t, err)
	require.Zero(t, count)
	require.Equal(t, 1, countRows(t, "events"))
	require.Zero(t, countRows(t, "deliveries"))

	_, err = repo.GetByID(ctx, event.ID)
	require.NoError(t, err)
}

func TestEvents_PublishEventWithDeliveries_RollbackOnDeliveriesError(t *testing.T) {
	truncate(t)

	ctx := context.Background()
	repo := NewEvents(pool, slog.New(slog.DiscardHandler), 5)

	ep := insertEndpoint(t, "https://test.com", "whsec")
	subscribe(t, ep, "order.created")

	// NOT VALID skips existing rows, but every new insert into deliveries fails
	_, err := pool.Exec(ctx, `ALTER TABLE deliveries ADD CONSTRAINT test_reject_all CHECK (false) NOT VALID`)
	require.NoError(t, err)

	t.Cleanup(func() {
		_, err := pool.Exec(context.Background(), `ALTER TABLE deliveries DROP CONSTRAINT test_reject_all`)
		require.NoError(t, err)
	})

	event, count, err := repo.PublishEventWithDeliveries(ctx, dto.PublishEventCommand{
		Type:    "order.created",
		Payload: json.RawMessage(`{}`),
	})

	require.Error(t, err)
	require.Nil(t, event)
	require.Zero(t, count)
	require.Zero(t, countRows(t, "events"), "event must be rolled back with deliveries")
	require.Zero(t, countRows(t, "deliveries"))
}

func TestEvents_PublishEventWithDeliveries_ManySubscribers(t *testing.T) {
	truncate(t)

	ctx := context.Background()
	repo := NewEvents(pool, slog.New(slog.DiscardHandler), 5)

	endpoints := make(map[uuid.UUID]struct{})
	for range 5 {
		ep := insertEndpoint(t, "https://test.com", "whsec")
		subscribe(t, ep, "order.created")
		endpoints[ep] = struct{}{}
	}

	event, count, err := repo.PublishEventWithDeliveries(ctx, dto.PublishEventCommand{
		Type:    "order.created",
		Payload: json.RawMessage(`{}`),
	})

	require.NoError(t, err)
	require.Equal(t, 5, count)

	rows, err := pool.Query(ctx, `SELECT endpoint_id FROM deliveries WHERE event_id = $1`, event.ID)
	require.NoError(t, err)
	defer rows.Close()

	got := make(map[uuid.UUID]struct{})
	for rows.Next() {
		var id uuid.UUID
		require.NoError(t, rows.Scan(&id))
		got[id] = struct{}{}
	}

	require.NoError(t, rows.Err())
	require.Equal(t, endpoints, got)
}

func TestEvents_GetByID_Success(t *testing.T) {
	truncate(t)

	ctx := context.Background()
	repo := NewEvents(pool, slog.New(slog.DiscardHandler), 5)

	published, _, err := repo.PublishEventWithDeliveries(ctx, dto.PublishEventCommand{
		Type:    "order.created",
		Payload: json.RawMessage(`{"b": 1,   "a": {"nested": true}}`),
	})
	require.NoError(t, err)

	got, err := repo.GetByID(ctx, published.ID)
	require.NoError(t, err)
	require.Equal(t, published.ID, got.ID)
	require.Equal(t, "order.created", got.Type)
	require.WithinDuration(t, published.CreatedAt, got.CreatedAt, 0)

	require.JSONEq(t, `{"a":{"nested":true},"b":1}`, string(got.Payload))
}

func TestEvents_GetByID_NotFound(t *testing.T) {
	truncate(t)

	ctx := context.Background()
	repo := NewEvents(pool, slog.New(slog.DiscardHandler), 5)

	insertEvent(t)

	got, err := repo.GetByID(ctx, uuid.Must(uuid.NewV7()))
	require.Nil(t, got)
	require.ErrorIs(t, err, domain.ErrEventNotFound)
}

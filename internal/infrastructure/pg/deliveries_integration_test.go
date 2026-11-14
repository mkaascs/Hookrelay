//go:build integration

package pg

import (
	"context"
	"log/slog"
	"testing"
	"time"
	"webhook-delivery/internal/domain"
	"webhook-delivery/internal/domain/dto"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

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
		ID:          uuid.Must(uuid.NewV7()),
		Status:      domain.StatusPending,
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

func TestDeliveries_ClaimPending_OldestFirst(t *testing.T) {
	truncate(t)

	ctx := context.Background()
	repo := NewDeliveries(pool, slog.New(slog.DiscardHandler))

	ep := insertEndpoint(t, "https://test.com", "whsec")
	now := time.Now()
	insertDelivery(t, ep, "pending", now.Add(-time.Minute))
	oldest := insertDelivery(t, ep, "pending", now.Add(-time.Hour))
	insertDelivery(t, ep, "pending", now.Add(-2*time.Minute))
	second := insertDelivery(t, ep, "pending", now.Add(-30*time.Minute))

	claimed, err := repo.ClaimPending(ctx, 2)
	require.NoError(t, err)
	require.Len(t, claimed, 2)

	ids := []uuid.UUID{claimed[0].ID, claimed[1].ID}
	require.ElementsMatch(t, []uuid.UUID{oldest, second}, ids)
}

func TestDeliveries_ClaimPending_ReturnsFullDelivery(t *testing.T) {
	truncate(t)

	ctx := context.Background()
	repo := NewDeliveries(pool, slog.New(slog.DiscardHandler))

	ep := insertEndpoint(t, "https://test.com", "whsec_top")
	eventID := insertEvent(t)
	id := insertDeliveryForEvent(t, ep, eventID)

	claimed, err := repo.ClaimPending(ctx, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	got := claimed[0]
	require.Equal(t, id, got.ID)
	require.Equal(t, ep, got.EndpointID)
	require.Equal(t, eventID, got.EventID)
	require.Equal(t, domain.StatusQueued, got.Status)
	require.Equal(t, 5, got.MaxAttempts)
	require.Equal(t, "https://test.com", got.URL)
	require.Equal(t, "whsec_top", got.Secret)
	require.JSONEq(t, `{"a":1}`, string(got.Payload))
}

func TestDeliveries_ClaimPending_TwiceDoesNotReclaim(t *testing.T) {
	truncate(t)

	ctx := context.Background()
	repo := NewDeliveries(pool, slog.New(slog.DiscardHandler))

	ep := insertEndpoint(t, "https://test.com", "whsec")
	insertDelivery(t, ep, "pending", time.Now().Add(-time.Minute))

	first, err := repo.ClaimPending(ctx, 10)
	require.NoError(t, err)
	require.Len(t, first, 1)

	second, err := repo.ClaimPending(ctx, 10)
	require.NoError(t, err)
	require.Empty(t, second)
}

func TestDeliveries_GetByID_NotFound(t *testing.T) {
	truncate(t)

	ctx := context.Background()
	repo := NewDeliveries(pool, slog.New(slog.DiscardHandler))

	ep := insertEndpoint(t, "https://test.com", "whsec")
	insertDelivery(t, ep, "pending", time.Now())

	got, err := repo.GetByID(ctx, uuid.Must(uuid.NewV7()))
	require.Nil(t, got)
	require.ErrorIs(t, err, domain.ErrDeliveryNotFound)
}

func TestDeliveries_GetFromEvent_Pagination(t *testing.T) {
	truncate(t)

	ctx := context.Background()
	repo := NewDeliveries(pool, slog.New(slog.DiscardHandler))

	eventID := insertEvent(t)
	ids := make([]uuid.UUID, 0, 5)
	for range 5 {
		ep := insertEndpoint(t, "https://test.com", "whsec")
		ids = append(ids, insertDeliveryForEvent(t, ep, eventID))
	}

	otherEvent := insertEvent(t)
	insertDeliveryForEvent(t, insertEndpoint(t, "https://other.com", "whsec"), otherEvent)

	page, total, err := repo.GetFromEvent(ctx, dto.GetDeliveriesFromEventCommand{EventID: eventID, Page: 2, Limit: 2})
	require.NoError(t, err)
	require.Equal(t, 5, total, "total must count only deliveries of this event")
	require.Len(t, page, 2)
	require.Equal(t, ids[2], page[0].ID)
	require.Equal(t, ids[3], page[1].ID)

	for _, delivery := range page {
		require.Equal(t, eventID, delivery.EventID)
	}

	last, _, err := repo.GetFromEvent(ctx, dto.GetDeliveriesFromEventCommand{EventID: eventID, Page: 3, Limit: 2})
	require.NoError(t, err)
	require.Len(t, last, 1)
	require.Equal(t, ids[4], last[0].ID)
}

func TestDeliveries_GetFromEvent_Empty(t *testing.T) {
	truncate(t)

	ctx := context.Background()
	repo := NewDeliveries(pool, slog.New(slog.DiscardHandler))

	page, total, err := repo.GetFromEvent(ctx, dto.GetDeliveriesFromEventCommand{
		EventID: insertEvent(t),
		Page:    1,
		Limit:   10,
	})
	require.NoError(t, err)
	require.Zero(t, total)
	require.Empty(t, page)
}

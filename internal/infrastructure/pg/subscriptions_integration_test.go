//go:build integration

package pg

import (
	"context"
	"log/slog"
	"testing"
	"webhook-delivery/internal/domain"
	"webhook-delivery/internal/domain/dto"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSubscriptions_Add_Success(t *testing.T) {
	truncate(t)

	ctx := context.Background()
	repo := NewSubscriptions(pool, slog.New(slog.DiscardHandler))

	ep := insertEndpoint(t, "https://test.com", "whsec")

	subs, err := repo.Add(ctx, dto.AddSubscriptionCommand{
		EndpointID: ep,
		EventTypes: []string{"order.created", "order.paid"},
	})
	require.NoError(t, err)
	require.Len(t, subs, 2)

	types := make([]string, 0, len(subs))
	for _, sub := range subs {
		require.NotEqual(t, uuid.Nil, sub.ID)
		require.Equal(t, ep, sub.EndpointID)
		require.False(t, sub.CreatedAt.IsZero())
		types = append(types, sub.EventType)
	}

	require.ElementsMatch(t, []string{"order.created", "order.paid"}, types)
	require.Equal(t, 2, countRows(t, "subscriptions"))
}

func TestSubscriptions_Add_ReturnsOnlyNew(t *testing.T) {
	truncate(t)

	ctx := context.Background()
	repo := NewSubscriptions(pool, slog.New(slog.DiscardHandler))

	ep := insertEndpoint(t, "https://test.com", "whsec")
	existing := subscribe(t, ep, "order.created")

	subs, err := repo.Add(ctx, dto.AddSubscriptionCommand{
		EndpointID: ep,
		EventTypes: []string{"order.created", "order.paid"},
	})
	require.NoError(t, err)
	require.Len(t, subs, 1)
	require.Equal(t, "order.paid", subs[0].EventType)
	require.NotEqual(t, existing, subs[0].ID)
	require.Equal(t, 2, countRows(t, "subscriptions"))
}

func TestSubscriptions_Add_AllExisting(t *testing.T) {
	truncate(t)

	ctx := context.Background()
	repo := NewSubscriptions(pool, slog.New(slog.DiscardHandler))

	ep := insertEndpoint(t, "https://test.com", "whsec")
	subscribe(t, ep, "order.created")

	subs, err := repo.Add(ctx, dto.AddSubscriptionCommand{
		EndpointID: ep,
		EventTypes: []string{"order.created"},
	})
	require.NoError(t, err)
	require.NotNil(t, subs)
	require.Empty(t, subs)
	require.Equal(t, 1, countRows(t, "subscriptions"))
}

func TestSubscriptions_Add_UnknownEndpoint(t *testing.T) {
	truncate(t)

	ctx := context.Background()
	repo := NewSubscriptions(pool, slog.New(slog.DiscardHandler))

	subs, err := repo.Add(ctx, dto.AddSubscriptionCommand{
		EndpointID: uuid.Must(uuid.NewV7()),
		EventTypes: []string{"order.created"},
	})
	require.ErrorIs(t, err, domain.ErrEndpointNotFound)
	require.Nil(t, subs)
	require.Zero(t, countRows(t, "subscriptions"))
}

func TestSubscriptions_GetAll(t *testing.T) {
	truncate(t)

	ctx := context.Background()
	repo := NewSubscriptions(pool, slog.New(slog.DiscardHandler))

	ep := insertEndpoint(t, "https://test.com", "whsec")
	first := subscribe(t, ep, "order.created")
	second := subscribe(t, ep, "order.paid")

	other := insertEndpoint(t, "https://other.com", "whsec")
	subscribe(t, other, "order.created")

	subs, err := repo.GetAll(ctx, ep)
	require.NoError(t, err)
	require.Len(t, subs, 2)

	ids := make([]uuid.UUID, 0, len(subs))
	for _, sub := range subs {
		require.Equal(t, ep, sub.EndpointID)
		ids = append(ids, sub.ID)
	}

	require.ElementsMatch(t, []uuid.UUID{first, second}, ids)
}

func TestSubscriptions_GetAll_Empty(t *testing.T) {
	truncate(t)

	ctx := context.Background()
	repo := NewSubscriptions(pool, slog.New(slog.DiscardHandler))

	ep := insertEndpoint(t, "https://test.com", "whsec")

	subs, err := repo.GetAll(ctx, ep)
	require.NoError(t, err)
	require.NotNil(t, subs)
	require.Empty(t, subs)
}

func TestSubscriptions_Delete_Success(t *testing.T) {
	truncate(t)

	ctx := context.Background()
	repo := NewSubscriptions(pool, slog.New(slog.DiscardHandler))

	ep := insertEndpoint(t, "https://test.com", "whsec")
	deleted := subscribe(t, ep, "order.created")
	kept := subscribe(t, ep, "order.paid")

	require.NoError(t, repo.Delete(ctx, deleted))

	subs, err := repo.GetAll(ctx, ep)
	require.NoError(t, err)
	require.Len(t, subs, 1)
	require.Equal(t, kept, subs[0].ID)
	require.Equal(t, 1, countRows(t, "endpoints"), "endpoint must not be deleted")
}

func TestSubscriptions_Delete_NotFound(t *testing.T) {
	truncate(t)

	ctx := context.Background()
	repo := NewSubscriptions(pool, slog.New(slog.DiscardHandler))

	ep := insertEndpoint(t, "https://test.com", "whsec")
	subscribe(t, ep, "order.created")

	err := repo.Delete(ctx, uuid.Must(uuid.NewV7()))
	require.ErrorIs(t, err, domain.ErrSubscriptionNotFound)
	require.Equal(t, 1, countRows(t, "subscriptions"))
}

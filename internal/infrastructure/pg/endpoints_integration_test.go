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

func TestEndpoints_Add_WithSubscriptions(t *testing.T) {
	truncate(t)

	ctx := context.Background()
	repo := NewEndpoints(pool, slog.New(slog.DiscardHandler))

	cmd := dto.AddEndpointCommand{
		URL:         "https://test.com",
		EventTypes:  []string{"order.created", "order.paid"},
		Description: new("billing"),
		Secret:      "whsec",
	}

	added, err := repo.Add(ctx, cmd)
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, added.ID)
	require.True(t, added.IsActive)
	require.False(t, added.CreatedAt.IsZero())

	got, err := repo.GetByID(ctx, added.ID)
	require.NoError(t, err)
	require.Equal(t, cmd.URL, got.URL)
	require.Equal(t, cmd.Secret, got.Secret)
	require.Equal(t, cmd.Description, got.Description)
	require.True(t, got.IsActive)
	require.ElementsMatch(t, cmd.EventTypes, got.EventTypes)
	require.WithinDuration(t, added.CreatedAt, got.CreatedAt, 0)
}

func TestEndpoints_Add_WithoutDescription(t *testing.T) {
	truncate(t)

	ctx := context.Background()
	repo := NewEndpoints(pool, slog.New(slog.DiscardHandler))

	added, err := repo.Add(ctx, dto.AddEndpointCommand{
		URL:        "https://test.com",
		EventTypes: []string{"order.created"},
		Secret:     "whsec",
	})
	require.NoError(t, err)

	got, err := repo.GetByID(ctx, added.ID)
	require.NoError(t, err)
	require.Equal(t, new(""), got.Description)
}

func TestEndpoints_Add_DuplicateEventTypes(t *testing.T) {
	truncate(t)

	ctx := context.Background()
	repo := NewEndpoints(pool, slog.New(slog.DiscardHandler))

	added, err := repo.Add(ctx, dto.AddEndpointCommand{
		URL:        "https://test.com",
		EventTypes: []string{"order.created", "order.created"},
		Secret:     "whsec",
	})
	require.NoError(t, err)

	got, err := repo.GetByID(ctx, added.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"order.created"}, got.EventTypes)
}

func TestEndpoints_GetByID_NotFound(t *testing.T) {
	truncate(t)

	ctx := context.Background()
	repo := NewEndpoints(pool, slog.New(slog.DiscardHandler))

	insertEndpoint(t, "https://test.com", "whsec")

	got, err := repo.GetByID(ctx, uuid.Must(uuid.NewV7()))
	require.Nil(t, got)
	require.ErrorIs(t, err, domain.ErrEndpointNotFound)
}

func TestEndpoints_GetAll_Pagination(t *testing.T) {
	truncate(t)

	ctx := context.Background()
	repo := NewEndpoints(pool, slog.New(slog.DiscardHandler))

	ids := make([]uuid.UUID, 0, 5)
	for range 5 {
		ids = append(ids, insertEndpoint(t, "https://test.com", "whsec"))
	}
	subscribe(t, ids[2], "order.created")
	subscribe(t, ids[2], "order.paid")

	page, total, err := repo.GetAll(ctx, dto.GetAllEndpointsCommand{Page: 2, Limit: 2})
	require.NoError(t, err)
	require.Equal(t, 5, total)
	require.Len(t, page, 2)
	require.Equal(t, ids[2], page[0].ID)
	require.Equal(t, ids[3], page[1].ID)
	require.ElementsMatch(t, []string{"order.created", "order.paid"}, page[0].EventTypes)
	require.Empty(t, page[1].EventTypes)

	last, total, err := repo.GetAll(ctx, dto.GetAllEndpointsCommand{Page: 3, Limit: 2})
	require.NoError(t, err)
	require.Equal(t, 5, total)
	require.Len(t, last, 1)
	require.Equal(t, ids[4], last[0].ID)
}

func TestEndpoints_GetAll_Empty(t *testing.T) {
	truncate(t)

	ctx := context.Background()
	repo := NewEndpoints(pool, slog.New(slog.DiscardHandler))

	got, total, err := repo.GetAll(ctx, dto.GetAllEndpointsCommand{Page: 1, Limit: 10})
	require.NoError(t, err)
	require.Zero(t, total)
	require.NotNil(t, got)
	require.Empty(t, got)
}

func TestEndpoints_Update_Partial(t *testing.T) {
	truncate(t)

	ctx := context.Background()
	repo := NewEndpoints(pool, slog.New(slog.DiscardHandler))

	added, err := repo.Add(ctx, dto.AddEndpointCommand{
		URL:         "https://old.com",
		EventTypes:  []string{"order.created"},
		Description: new("billing"),
		Secret:      "whsec",
	})
	require.NoError(t, err)

	other := insertEndpoint(t, "https://other.com", "whsec")

	err = repo.Update(ctx, dto.UpdateEndpointCommand{
		ID:       added.ID,
		URL:      new("https://new.com"),
		IsActive: new(false),
	})
	require.NoError(t, err)

	got, err := repo.GetByID(ctx, added.ID)
	require.NoError(t, err)
	require.Equal(t, "https://new.com", got.URL)
	require.False(t, got.IsActive)
	require.Equal(t, new("billing"), got.Description, "nil field must not be overwritten")
	require.Equal(t, "whsec", got.Secret)
	require.Equal(t, []string{"order.created"}, got.EventTypes)

	untouched, err := repo.GetByID(ctx, other)
	require.NoError(t, err)
	require.Equal(t, "https://other.com", untouched.URL)
	require.True(t, untouched.IsActive)
}

func TestEndpoints_Update_NotFound(t *testing.T) {
	truncate(t)

	ctx := context.Background()
	repo := NewEndpoints(pool, slog.New(slog.DiscardHandler))

	err := repo.Update(ctx, dto.UpdateEndpointCommand{
		ID:  uuid.Must(uuid.NewV7()),
		URL: new("https://new.com"),
	})
	require.ErrorIs(t, err, domain.ErrEndpointNotFound)
}

func TestEndpoints_Delete_Cascade(t *testing.T) {
	truncate(t)

	ctx := context.Background()
	repo := NewEndpoints(pool, slog.New(slog.DiscardHandler))

	ep := insertEndpoint(t, "https://test.com", "whsec")
	subscribe(t, ep, "order.created")
	insertDelivery(t, ep, "pending", time.Now())

	other := insertEndpoint(t, "https://other.com", "whsec")
	subscribe(t, other, "order.created")
	otherDelivery := insertDelivery(t, other, "pending", time.Now())

	require.NoError(t, repo.Delete(ctx, ep))

	_, err := repo.GetByID(ctx, ep)
	require.ErrorIs(t, err, domain.ErrEndpointNotFound)

	require.Equal(t, 1, countRows(t, "endpoints"))
	require.Equal(t, 1, countRows(t, "subscriptions"), "subscriptions must be deleted by cascade")
	require.Equal(t, 1, countRows(t, "deliveries"), "deliveries must be deleted by cascade")

	_, err = NewDeliveries(pool, slog.New(slog.DiscardHandler)).GetByID(ctx, otherDelivery)
	require.NoError(t, err)
}

func TestEndpoints_Delete_NotFound(t *testing.T) {
	truncate(t)

	ctx := context.Background()
	repo := NewEndpoints(pool, slog.New(slog.DiscardHandler))

	insertEndpoint(t, "https://test.com", "whsec")

	err := repo.Delete(ctx, uuid.Must(uuid.NewV7()))
	require.ErrorIs(t, err, domain.ErrEndpointNotFound)
	require.Equal(t, 1, countRows(t, "endpoints"))
}

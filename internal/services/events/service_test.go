package events

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"webhook-delivery/internal/domain"
	"webhook-delivery/internal/domain/dto"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

var eventID = uuid.MustParse("01900000-0000-7000-8000-0000000000e1")

func newTestService(t *testing.T) (*Service, *MockEventRepo, *MockNotifier) {
	ctrl := gomock.NewController(t)
	eventRepo := NewMockEventRepo(ctrl)
	notifier := NewMockNotifier(ctrl)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewService(eventRepo, notifier, log), eventRepo, notifier
}

func Test_Service_Publish(t *testing.T) {
	cmd := dto.PublishEventCommand{Type: "order.created"}
	event := &domain.Event{ID: eventID, Type: "order.created"}

	t.Run("success notifies dispatcher", func(t *testing.T) {
		svc, eventRepo, notifier := newTestService(t)
		gomock.InOrder(
			eventRepo.EXPECT().PublishEventWithDeliveries(gomock.Any(), cmd).Return(event, 3, nil),
			notifier.EXPECT().Notify(),
		)

		got, deliveriesCreated, err := svc.Publish(context.Background(), cmd)
		require.NoError(t, err)
		require.Equal(t, event, got)
		require.Equal(t, 3, deliveriesCreated)
	})

	t.Run("no subscribers does not notify dispatcher", func(t *testing.T) {
		svc, eventRepo, _ := newTestService(t)
		eventRepo.EXPECT().PublishEventWithDeliveries(gomock.Any(), cmd).Return(event, 0, nil)

		got, deliveriesCreated, err := svc.Publish(context.Background(), cmd)
		require.NoError(t, err)
		require.Equal(t, event, got)
		require.Zero(t, deliveriesCreated)
	})

	t.Run("repo error is wrapped and dispatcher is not notified", func(t *testing.T) {
		svc, eventRepo, _ := newTestService(t)
		repoErr := errors.New("db down")
		eventRepo.EXPECT().PublishEventWithDeliveries(gomock.Any(), cmd).Return(nil, 0, repoErr)

		got, deliveriesCreated, err := svc.Publish(context.Background(), cmd)
		require.Nil(t, got)
		require.Zero(t, deliveriesCreated)
		require.ErrorIs(t, err, repoErr)
	})
}

func Test_Service_Get(t *testing.T) {
	id := eventID
	event := &domain.Event{ID: id, Type: "order.created"}

	t.Run("success", func(t *testing.T) {
		svc, eventRepo, _ := newTestService(t)
		eventRepo.EXPECT().GetByID(gomock.Any(), id).Return(event, nil)

		got, err := svc.Get(context.Background(), id)
		require.NoError(t, err)
		require.Equal(t, event, got)
	})

	t.Run("error is wrapped", func(t *testing.T) {
		svc, eventRepo, _ := newTestService(t)
		eventRepo.EXPECT().GetByID(gomock.Any(), id).Return(nil, domain.ErrEventNotFound)

		got, err := svc.Get(context.Background(), id)
		require.Nil(t, got)
		require.ErrorIs(t, err, domain.ErrEventNotFound)
	})
}

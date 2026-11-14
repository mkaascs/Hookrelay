package events

import (
	"context"
	"fmt"
	"log/slog"
	"webhook-delivery/internal/domain"
	"webhook-delivery/internal/domain/dto"
	sloglib "webhook-delivery/internal/lib/logging/slog"

	"github.com/google/uuid"
)

type EventRepo interface {
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Event, error)
	PublishEventWithDeliveries(ctx context.Context, command dto.PublishEventCommand) (*domain.Event, int, error)
}

type Notifier interface {
	Notify()
}

type Service struct {
	log          *slog.Logger
	eventRepo    EventRepo
	notifier     Notifier
}

func NewService(eventRepo EventRepo, notifier Notifier, log *slog.Logger) *Service {
	return &Service{
		log:          log,
		eventRepo:    eventRepo,
		notifier:     notifier,
	}
}

func (s *Service) Publish(ctx context.Context, command dto.PublishEventCommand) (*domain.Event, int, error) {
	const fn = "services.events.Service.Publish"
	log := s.log.With(slog.String("fn", fn))

	event, count, err := s.eventRepo.PublishEventWithDeliveries(ctx, command)
	if err != nil {
		const msg = "failed to publish event"
		s.logError(log, msg, err)
		return nil, 0, fmt.Errorf("%s: %s: %w", fn, msg, err)
	}

	if count > 0 {
		s.notifier.Notify()
	}

	log.Info("event was published successfully", slog.String("event_id", event.ID.String()), slog.String("event_type", event.Type))

	return event, count, nil
}

func (s *Service) Get(ctx context.Context, eventID uuid.UUID) (*domain.Event, error) {
	const fn = "services.events.Service.Get"
	log := s.log.With(slog.String("fn", fn))

	event, err := s.eventRepo.GetByID(ctx, eventID)
	if err != nil {
		const msg = "failed to get event by id"
		s.logError(log, msg, err, slog.String("event_id", eventID.String()))
		return nil, fmt.Errorf("%s: %s: %w", fn, msg, err)
	}

	log.Info("event info was sent successfully", slog.String("event_id", event.ID.String()))

	return event, nil
}

func (s *Service) logError(log *slog.Logger, msg string, err error, args ...any) {
	if domain.IsDomainError(err) || domain.IsCtxError(err) {
		log.With(args...).Info(msg, sloglib.Error(err))
		return
	}

	log.With(args...).Error(msg, sloglib.Error(err))
}

package postgres

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"webhook-delivery/internal/domain"
	"webhook-delivery/internal/domain/dto"
	sloglib "webhook-delivery/internal/lib/logging/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Events struct {
	pool        *pgxpool.Pool
	log         *slog.Logger
	maxAttempts int
}

func NewEvents(pool *pgxpool.Pool, log *slog.Logger, maxAttempts int) *Events {
	return &Events{pool: pool, maxAttempts: maxAttempts, log: log}
}

func (e *Events) PublishEventWithDeliveries(ctx context.Context, command dto.PublishEventCommand) (*domain.Event, int, error) {
	const fn = "infrastructure.pg.Events.Add"
	log := e.log.With(slog.String("fn", fn))

	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("%s: %w", fn, err)
	}

	defer func(tx pgx.Tx) {
		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			log.Warn("failed to rollback transaction", sloglib.Error(err))
		}
	}(tx)

	event, err := e.addEventTx(ctx, tx, command)
	if err != nil {
		return nil, 0, fmt.Errorf("%s: %w", fn, err)
	}

	total, err := e.createDeliveriesForEventTx(ctx, tx, event.ID, event.Type)
	if err != nil {
		return nil, 0, fmt.Errorf("%s: %w", fn, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, 0, fmt.Errorf("%s: %w", fn, err)
	}

	return event, total, nil
}

func (e *Events) GetByID(ctx context.Context, id uuid.UUID) (*domain.Event, error) {
	const fn = "infrastructure.pg.Events.GetByID"

	event := &domain.Event{ID: id}

	err := e.pool.QueryRow(ctx, `
		SELECT type, payload, created_at
		FROM events
		WHERE id = $1`, id).
		Scan(&event.Type, &event.Payload, &event.CreatedAt)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrEventNotFound
		}

		return nil, fmt.Errorf("%s: %w", fn, err)
	}

	return event, nil
}

func (e *Events) addEventTx(ctx context.Context, tx pgx.Tx, command dto.PublishEventCommand) (*domain.Event, error) {
	const fn = "infrastructure.pg.Events.addEventTx"

	event := &domain.Event{
		ID:      uuid.Must(uuid.NewV7()),
		Payload: command.Payload,
		Type:    command.Type,
	}

	err := tx.QueryRow(ctx, `
		INSERT INTO events (id, type, payload)
		VALUES ($1, $2, $3)
		RETURNING created_at`,
		event.ID, command.Type, command.Payload).
		Scan(&event.CreatedAt)

	if err != nil {
		return nil, fmt.Errorf("%s: %w", fn, err)
	}

	return event, nil
}

func (e *Events) createDeliveriesForEventTx(ctx context.Context, tx pgx.Tx, eventID uuid.UUID, eventType string) (int, error) {
	const fn = "infrastructure.pg.Events.createDeliveriesForEventTx"

	rows, err := tx.Query(ctx, `
		SELECT endpoint_id
		FROM subscriptions
		JOIN endpoints ON endpoints.id = endpoint_id
		WHERE event_type = $1 AND endpoints.is_active`, eventType)

	if err != nil {
		return 0, fmt.Errorf("%s: %w", fn, err)
	}

	defer rows.Close()

	var deliveryIDs, endpointIDs []uuid.UUID
	for rows.Next() {
		var endpointID uuid.UUID
		if err := rows.Scan(&endpointID); err != nil {
			return 0, fmt.Errorf("%s: %w", fn, err)
		}

		endpointIDs = append(endpointIDs, endpointID)
		deliveryIDs = append(deliveryIDs, uuid.Must(uuid.NewV7()))
	}

	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("%s: %w", fn, err)
	}

	if len(endpointIDs) == 0 {
		return 0, nil
	}

	res, err := tx.Exec(ctx, `
		INSERT INTO deliveries (id, endpoint_id, event_id, max_attempts)
		SELECT d_id, e_id, $3, $4
		FROM unnest($1::uuid[], $2::uuid[]) AS t(d_id, e_id)
		ON CONFLICT (endpoint_id, event_id) DO NOTHING`,
		deliveryIDs, endpointIDs, eventID, e.maxAttempts)

	if err != nil {
		return 0, fmt.Errorf("%s: %w", fn, err)
	}

	return int(res.RowsAffected()), nil
}

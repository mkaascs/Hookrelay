package telemetry

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

type Telemetry struct {
	provider *sdktrace.TracerProvider
	tracer   trace.Tracer
}

func New(ctx context.Context, endpoint string) (*Telemetry, error) {
	if endpoint == "" || endpoint == "-" {
		return &Telemetry{}, nil
	}

	u, err := url.Parse(endpoint)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, errors.New("tracing endpoint must be an absolute HTTP or HTTPS URL")
	}

	exporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint))
	if err != nil {
		return nil, fmt.Errorf("create trace exporter: %w", err)
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", "hookrelay"))),
		sdktrace.WithBatcher(exporter),
	)

	return &Telemetry{
		provider: provider,
		tracer:   provider.Tracer("webhook-delivery"),
	}, nil
}

func (t *Telemetry) StartDeliveryAttempt(ctx context.Context, deliveryID uuid.UUID, attempt int) (context.Context, func(code int, err error)) {
	if t.provider == nil {
		return ctx, func(int, error) {}
	}

	ctx, span := t.tracer.Start(ctx, "delivery.attempt",
		trace.WithAttributes(
			attribute.String("delivery.id", deliveryID.String()),
			attribute.Int("delivery.attempt", attempt),
		),
	)
	return ctx, func(code int, err error) {
		if err != nil || code < 200 || code >= 300 {
			span.SetStatus(codes.Error, "delivery attempt failed")
		}
		span.End()
	}
}

func (t *Telemetry) StartWebhookSend(ctx context.Context) (context.Context, func(code int, err error)) {
	if t.provider == nil {
		return ctx, func(int, error) {}
	}

	ctx, span := t.tracer.Start(ctx, "webhook.send", trace.WithSpanKind(trace.SpanKindClient))
	return ctx, func(code int, err error) {
		defer span.End()

		if code > 0 {
			span.SetAttributes(attribute.Int("http.response.status_code", code))
		}

		switch {
		case err != nil:
			span.SetStatus(codes.Error, "webhook request failed")
		case code < 200 || code >= 300:
			span.SetStatus(codes.Error, "unsuccessful HTTP response")
		}
	}
}

func (t *Telemetry) Shutdown(ctx context.Context) error {
	if t.provider == nil {
		return nil
	}
	return t.provider.Shutdown(ctx)
}

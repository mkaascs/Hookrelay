package telemetry

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

func (t *Telemetry) PostgresTracer() pgx.QueryTracer {
	if t.provider == nil {
		return nil
	}
	return postgresTracer{tracer: t.tracer}
}

type postgresTracer struct {
	tracer trace.Tracer
}

func (t postgresTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if !trace.SpanFromContext(ctx).IsRecording() {
		return ctx
	}

	name := "postgresql"
	fields := strings.Fields(data.SQL)
	if len(fields) > 0 {
		name += " " + strings.ToUpper(fields[0])
	}
	ctx, _ = t.tracer.Start(ctx, name,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attribute.String("db.system.name", "postgresql")),
	)
	return ctx
}

func (t postgresTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	if data.Err != nil {
		span.SetStatus(codes.Error, "query failed")
	}
	span.End()
}

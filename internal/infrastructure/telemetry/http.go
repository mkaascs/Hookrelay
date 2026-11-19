package telemetry

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/propagation"
)

func (t *Telemetry) HTTPMiddleware(next http.Handler) http.Handler {
	if t.provider == nil {
		return next
	}

	return otelhttp.NewMiddleware("hookrelay.http",
		otelhttp.WithTracerProvider(t.provider),
		otelhttp.WithPropagators(propagation.TraceContext{}),
	)(next)
}

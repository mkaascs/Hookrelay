package middlewares

import (
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	apiv1 "webhook-delivery/gen/rest/v1"

	"github.com/go-chi/chi/v5/middleware"
)

const apiKeyHeader = "X-Api-Key"

func NewAPIAuth(log *slog.Logger) func(http.Handler) http.Handler {
	expected := []byte(os.Getenv("API_KEY"))

	return func(next http.Handler) http.Handler {
		log := log.With(slog.String("component", "middleware/api_auth"))

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log.With(slog.String("request_id", middleware.GetReqID(r.Context())))

			if len(expected) == 0 {
				log.Warn("env API_KEY is not provided: auth is skipped")
				next.ServeHTTP(w, r)
			}

			provided := r.Header.Get(apiKeyHeader)
			if provided == "" {
				log.Info("unauthorized attempt to access API")
				writeAuthError(w, http.StatusUnauthorized, "unauthorized", "missing api key")
				return
			}

			if subtle.ConstantTimeCompare([]byte(provided), expected) != 1 {
				log.Info("forbidden to access API")
				writeAuthError(w, http.StatusForbidden, "forbidden", "invalid api key")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func writeAuthError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(apiv1.Error{
		Code: &code,
		Message: message,
	})
}

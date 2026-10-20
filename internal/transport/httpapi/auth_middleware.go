package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/purangoswami2001/hookrelay/internal/application/users"
	"github.com/purangoswami2001/hookrelay/internal/domain"
)

type contextKey string

const userContextKey contextKey = "hookrelay_auth_user"

// UserFromContext retrieves the authenticated user from the HTTP request context.
func UserFromContext(ctx context.Context) (*domain.User, bool) {
	user, ok := ctx.Value(userContextKey).(*domain.User)
	return user, ok && user != nil
}

// RequireAuth creates a middleware that enforces API key authentication.
func RequireAuth(userService *users.Service, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apiKey := extractAPIKey(r)
			if apiKey == "" {
				writeError(w, http.StatusUnauthorized, "unauthorized: API key required (provide via Authorization: Bearer <key> or X-API-Key header)")
				return
			}

			user, err := userService.AuthenticateByAPIKey(r.Context(), apiKey)
			if err != nil {
				logger.Warn("authentication failed", "path", r.URL.Path, "error", err)
				writeError(w, http.StatusUnauthorized, "unauthorized: invalid API key")
				return
			}

			ctx := context.WithValue(r.Context(), userContextKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func extractAPIKey(r *http.Request) string {
	// 1. Try Authorization: Bearer <key>
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "bearer") {
			return strings.TrimSpace(parts[1])
		}
		// Also support raw key in Authorization header
		if len(parts) == 1 {
			return parts[0]
		}
	}

	// 2. Try X-API-Key header
	apiKeyHeader := strings.TrimSpace(r.Header.Get("X-API-Key"))
	if apiKeyHeader != "" {
		return apiKeyHeader
	}

	return ""
}

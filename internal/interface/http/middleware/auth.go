package middleware

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/SeventeenthEarth/kkachi/internal/config"
)

// AuthMiddleware creates a middleware that checks for Bearer token authentication.
func NewAuthMiddleware(cfg config.AuthConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !cfg.AuthEnabled {
				next.ServeHTTP(w, r)
				return
			}

			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				slog.Warn("auth_failed", "reason", "missing_header", "path", r.URL.Path, "remote_addr", r.RemoteAddr)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			parts := strings.Split(authHeader, " ")
			if len(parts) != 2 || parts[0] != "Bearer" {
				slog.Warn("auth_failed", "reason", "malformed_header", "path", r.URL.Path, "remote_addr", r.RemoteAddr)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			token := parts[1]
			if token != cfg.AuthToken {
				slog.Warn("auth_failed", "reason", "invalid_token", "path", r.URL.Path, "remote_addr", r.RemoteAddr)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

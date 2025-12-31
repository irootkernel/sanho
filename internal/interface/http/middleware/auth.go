package middleware

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/SeventeenthEarth/kkachi/internal/config"
	"github.com/SeventeenthEarth/kkachi/internal/pty"
)

type authErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

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
				writeAuthError(w, "missing_authorization_header")
				return
			}

			parts := strings.Split(authHeader, " ")
			if len(parts) != 2 || parts[0] != "Bearer" {
				slog.Warn("auth_failed", "reason", "malformed_header", "path", r.URL.Path, "remote_addr", r.RemoteAddr)
				writeAuthError(w, "malformed_authorization_header")
				return
			}

			token := parts[1]
			if token != cfg.AuthToken {
				slog.Warn("auth_failed", "reason", "invalid_token", "path", r.URL.Path, "remote_addr", r.RemoteAddr)
				writeAuthError(w, "invalid_token")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func writeAuthError(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	if err := json.NewEncoder(w).Encode(authErrorResponse{
		Error:   pty.CodeUnauthorized,
		Message: message,
	}); err != nil {
		slog.Error("auth_error_response_write_failed", "error", err)
	}
}

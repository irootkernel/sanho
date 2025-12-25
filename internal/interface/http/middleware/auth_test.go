package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/SeventeenthEarth/kkachi/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestAuthMiddleware(t *testing.T) {
	tests := []struct {
		name           string
		authConfig     config.AuthConfig
		authHeader     string
		expectedStatus int
	}{
		{
			name: "Auth disabled",
			authConfig: config.AuthConfig{
				AuthEnabled: false,
			},
			authHeader:     "",
			expectedStatus: http.StatusOK,
		},
		{
			name: "Auth enabled, valid token",
			authConfig: config.AuthConfig{
				AuthEnabled: true,
				AuthToken:   "valid-token",
			},
			authHeader:     "Bearer valid-token",
			expectedStatus: http.StatusOK,
		},
		{
			name: "Auth enabled, missing header",
			authConfig: config.AuthConfig{
				AuthEnabled: true,
				AuthToken:   "valid-token",
			},
			authHeader:     "",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name: "Auth enabled, invalid token",
			authConfig: config.AuthConfig{
				AuthEnabled: true,
				AuthToken:   "valid-token",
			},
			authHeader:     "Bearer invalid-token",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name: "Auth enabled, malformed header",
			authConfig: config.AuthConfig{
				AuthEnabled: true,
				AuthToken:   "valid-token",
			},
			authHeader:     "Basic user:pass",
			expectedStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			middleware := NewAuthMiddleware(tt.authConfig)
			handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest("GET", "/", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
		})
	}
}

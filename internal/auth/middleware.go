// internal/auth/middleware.go
package auth

import (
	"context"
	"net/http"
	"strings"
)

// contextKey is unexported to avoid collisions with other packages.
type contextKey string

const clientIDKey contextKey = "clientID"
const tokenKey contextKey = "token"

// TokenFromRequest extracts the Bearer token from the Authorization header.
// Returns empty string if not present.
func TokenFromRequest(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if strings.HasPrefix(header, "Bearer ") {
		return strings.TrimPrefix(header, "Bearer ")
	}
	// also allow ?token= query param (used by WebSocket upgrade)
	return r.URL.Query().Get("token")
}

// ClientIDFromContext retrieves the authenticated clientID injected by AuthMiddleware.
// Returns 0 if not set.
func ClientIDFromContext(ctx context.Context) int {
	v, _ := ctx.Value(clientIDKey).(int)
	return v
}

// TokenFromContext retrieves the raw token string injected by AuthMiddleware.
func TokenFromContext(ctx context.Context) string {
	v, _ := ctx.Value(tokenKey).(string)
	return v
}

// AuthMiddleware wraps an http.HandlerFunc, validates the Bearer token, and injects
// clientID into the request context. Responds 401 if missing/invalid.
func AuthMiddleware(ss *SessionStore, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// handle CORS preflight — let it pass without auth check
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.WriteHeader(http.StatusNoContent)
			return
		}

		token := TokenFromRequest(r)
		if token == "" {
			errJSON(w, http.StatusUnauthorized, "missing authorization token")
			return
		}

		clientID, err := ss.ValidateSession(r.Context(), token)
		if err != nil {
			errJSON(w, http.StatusUnauthorized, "invalid or expired session")
			return
		}

		ctx := context.WithValue(r.Context(), clientIDKey, clientID)
		ctx = context.WithValue(ctx, tokenKey, token)
		next(w, r.WithContext(ctx))
	}
}

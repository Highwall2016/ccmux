package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/ccmux/backend/internal/auth"
)

type contextKey string

const contextUserID = contextKey("userID")

// Auth validates Bearer JWT tokens and injects userID into the request context.
func Auth(jwtSecret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if !strings.HasPrefix(header, "Bearer ") {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			claims, err := auth.ParseToken(strings.TrimPrefix(header, "Bearer "), jwtSecret)
			if err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), contextUserID, claims.UserID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// UserIDFromCtx retrieves the authenticated user ID from the context.
func UserIDFromCtx(ctx context.Context) string {
	id, _ := ctx.Value(contextUserID).(string)
	return id
}

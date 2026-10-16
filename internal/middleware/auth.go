package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/abneribeiro/internal/authz"
	"github.com/abneribeiro/internal/httpx"
	"github.com/abneribeiro/internal/jwt"
	"github.com/google/uuid"
)

type contextKey string

const UserIDKey contextKey = "userID"
const UserEmailKey contextKey = "userEmail"
const UserRoleKey contextKey = "userRole"

const bearerPrefix = "Bearer "

func AuthMiddleware(jwtManager *jwt.Manager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				httpx.WriteError(w, http.StatusUnauthorized, httpx.CodeUnauthorized, "authorization header required")
				return
			}

			// RFC 6750 makes the scheme case-insensitive, so "bearer x" is valid.
			if len(authHeader) < len(bearerPrefix) || !strings.EqualFold(authHeader[:len(bearerPrefix)], bearerPrefix) {
				httpx.WriteError(w, http.StatusUnauthorized, httpx.CodeUnauthorized, "invalid authorization header format")
				return
			}

			tokenString := strings.TrimSpace(authHeader[len(bearerPrefix):])
			if tokenString == "" {
				httpx.WriteError(w, http.StatusUnauthorized, httpx.CodeUnauthorized, "invalid authorization header format")
				return
			}

			claims, err := jwtManager.ValidateToken(tokenString)
			if err != nil {
				// The reason stays server-side: telling an unauthenticated
				// caller why the token failed is free reconnaissance.
				slog.WarnContext(r.Context(), "token validation failed", "error", err, "path", r.URL.Path)
				httpx.WriteError(w, http.StatusUnauthorized, httpx.CodeUnauthorized, "invalid or expired token")
				return
			}

			ctx := context.WithValue(r.Context(), UserIDKey, claims.UserID)
			ctx = context.WithValue(ctx, UserEmailKey, claims.Email)
			ctx = context.WithValue(ctx, UserRoleKey, claims.Role)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func GetUserIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	userID, ok := ctx.Value(UserIDKey).(uuid.UUID)
	return userID, ok
}

func GetUserEmailFromContext(ctx context.Context) (string, bool) {
	email, ok := ctx.Value(UserEmailKey).(string)
	return email, ok
}

// GetUserRoleFromContext returns the role carried in the access token. A
// token issued before roles existed has no role claim; such a caller is
// treated as a member, never as an admin.
func GetUserRoleFromContext(ctx context.Context) (string, bool) {
	role, ok := ctx.Value(UserRoleKey).(string)
	if !ok || role == "" {
		return authz.RoleMember, false
	}

	return role, true
}

// ScopeFromContext builds the identity that repositories filter by.
func ScopeFromContext(ctx context.Context) (authz.Scope, bool) {
	userID, ok := GetUserIDFromContext(ctx)
	if !ok {
		return authz.Scope{}, false
	}

	role, _ := GetUserRoleFromContext(ctx)

	return authz.NewScope(userID, role), true
}

// RequireAdmin rejects any caller whose token does not carry the admin role.
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		role, _ := GetUserRoleFromContext(r.Context())
		if role != authz.RoleAdmin {
			httpx.WriteError(w, http.StatusForbidden, httpx.CodeForbidden, "admin role required")
			return
		}

		next.ServeHTTP(w, r)
	})
}

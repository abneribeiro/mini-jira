package jwt

import (
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const testSecret = "test-secret-at-least-32-characters-long"

// sign builds a token from arbitrary claims, for the cases GenerateToken
// cannot produce.
func sign(t *testing.T, method jwt.SigningMethod, key any, claims Claims) string {
	t.Helper()

	token, err := jwt.NewWithClaims(method, claims).SignedString(key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	return token
}

func TestGenerateAndValidateRoundTrip(t *testing.T) {
	m := NewManager(testSecret)
	id := uuid.New()

	token, err := m.GenerateToken(id, "alice@example.com", "admin")
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	claims, err := m.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}

	if claims.UserID != id || claims.Email != "alice@example.com" || claims.Role != "admin" {
		t.Errorf("claims = %+v, want the values the token was issued with", claims)
	}
	if claims.Subject != id.String() {
		t.Errorf("sub = %q, want %q", claims.Subject, id)
	}

	ttl := claims.ExpiresAt.Sub(claims.IssuedAt.Time)
	if ttl != AccessTokenTTL {
		t.Errorf("token lifetime = %v, want %v", ttl, AccessTokenTTL)
	}
}

func TestExpiredTokenIsDistinguished(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	token := sign(t, jwt.SigningMethodHS256, []byte(testSecret), Claims{
		UserID: uuid.New(),
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(past),
			IssuedAt:  jwt.NewNumericDate(past.Add(-AccessTokenTTL)),
		},
	})

	if _, err := NewManager(testSecret).ValidateToken(token); !errors.Is(err, ErrExpiredToken) {
		t.Errorf("err = %v, want ErrExpiredToken", err)
	}
}

func TestInvalidTokensAreRejected(t *testing.T) {
	future := jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}
	claims := Claims{UserID: uuid.New(), Role: "admin", RegisteredClaims: future}

	other, err := NewManager("another-secret-that-is-32-chars-long!").GenerateToken(uuid.New(), "x@y.z", "member")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name  string
		token string
	}{
		{"signed with another secret", other},
		// Same secret, different HMAC: only HS256 is accepted.
		{"HS512", sign(t, jwt.SigningMethodHS512, []byte(testSecret), claims)},
		// The classic forgery: an unsigned token claiming admin.
		{"alg none", sign(t, jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, claims)},
		{"garbage", "not.a.jwt"},
		{"empty", ""},
	}

	m := NewManager(testSecret)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := m.ValidateToken(tt.token); !errors.Is(err, ErrInvalidToken) {
				t.Errorf("err = %v, want ErrInvalidToken", err)
			}
		})
	}
}

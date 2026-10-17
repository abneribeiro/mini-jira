package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/abneribeiro/internal/metrics"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// refreshTokenBytes is the entropy of the opaque token handed to the client.
const refreshTokenBytes = 32

type RefreshRepository interface {
	Store(ctx context.Context, id, userID, familyID uuid.UUID, hash []byte, expiresAt time.Time) error
	FindByHash(ctx context.Context, hash []byte) (RefreshToken, error)
	Revoke(ctx context.Context, id uuid.UUID) (bool, error)
	RevokeFamily(ctx context.Context, familyID uuid.UUID) error
	RevokeAllForUser(ctx context.Context, userID uuid.UUID) error
	DeleteExpired(ctx context.Context, before time.Time) (int64, error)
}

type refreshRepository struct {
	db *pgxpool.Pool
}

func NewRefreshRepository(db *pgxpool.Pool) RefreshRepository {
	return &refreshRepository{db: db}
}

// newRefreshToken returns the value to hand the client and the hash to store.
// The plaintext is never persisted, so a database leak yields nothing usable.
func newRefreshToken() (token string, hash []byte, err error) {
	raw := make([]byte, refreshTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("generate refresh token: %w", err)
	}

	token = base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(token))

	return token, sum[:], nil
}

func hashRefreshToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))

	return sum[:]
}

func (r *refreshRepository) Store(ctx context.Context, id, userID, familyID uuid.UUID, hash []byte, expiresAt time.Time) error {
	defer metrics.ObserveQuery("insert", "refresh_tokens")()

	const query = `
		INSERT INTO refresh_tokens (id, user_id, family_id, token_hash, expires_at)
		VALUES ($1, $2, $3, $4, $5)
	`

	if _, err := r.db.Exec(ctx, query, id, userID, familyID, hash, expiresAt); err != nil {
		return fmt.Errorf("repository.Store: failed to store refresh token: %w", err)
	}

	return nil
}

func (r *refreshRepository) FindByHash(ctx context.Context, hash []byte) (RefreshToken, error) {
	defer metrics.ObserveQuery("select", "refresh_tokens")()

	const query = `
		SELECT id, user_id, family_id, expires_at, revoked_at
		FROM refresh_tokens
		WHERE token_hash = $1
	`

	var t RefreshToken
	err := r.db.QueryRow(ctx, query, hash).Scan(&t.ID, &t.UserID, &t.FamilyID, &t.ExpiresAt, &t.RevokedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return RefreshToken{}, fmt.Errorf("repository.FindByHash: %w", ErrInvalidRefresh)
		}
		return RefreshToken{}, fmt.Errorf("repository.FindByHash: failed to look up refresh token: %w", err)
	}

	return t, nil
}

// Revoke reports whether this call is the one that revoked the token. The
// revoked_at IS NULL guard makes the UPDATE the arbiter between concurrent
// rotations: only one of them affects the row, and the other learns that the
// token was already spent.
func (r *refreshRepository) Revoke(ctx context.Context, id uuid.UUID) (bool, error) {
	defer metrics.ObserveQuery("update", "refresh_tokens")()

	const query = `
		UPDATE refresh_tokens
		SET revoked_at = NOW()
		WHERE id = $1 AND revoked_at IS NULL
	`

	tag, err := r.db.Exec(ctx, query, id)
	if err != nil {
		return false, fmt.Errorf("repository.Revoke: failed to revoke refresh token: %w", err)
	}

	return tag.RowsAffected() == 1, nil
}

// RevokeFamily kills an entire rotation chain, used when an already-rotated
// token is replayed.
func (r *refreshRepository) RevokeFamily(ctx context.Context, familyID uuid.UUID) error {
	defer metrics.ObserveQuery("update", "refresh_tokens")()

	const query = `
		UPDATE refresh_tokens
		SET revoked_at = NOW()
		WHERE family_id = $1 AND revoked_at IS NULL
	`

	if _, err := r.db.Exec(ctx, query, familyID); err != nil {
		return fmt.Errorf("repository.RevokeFamily: failed to revoke family: %w", err)
	}

	return nil
}

// RevokeAllForUser ends every session of a user. The user service calls it
// after a password change, so a stolen refresh token stops working.
func (r *refreshRepository) RevokeAllForUser(ctx context.Context, userID uuid.UUID) error {
	defer metrics.ObserveQuery("update", "refresh_tokens")()

	const query = `
		UPDATE refresh_tokens
		SET revoked_at = NOW()
		WHERE user_id = $1 AND revoked_at IS NULL
	`

	if _, err := r.db.Exec(ctx, query, userID); err != nil {
		return fmt.Errorf("repository.RevokeAllForUser: failed to revoke tokens: %w", err)
	}

	return nil
}

// DeleteExpired keeps the table from growing without bound.
func (r *refreshRepository) DeleteExpired(ctx context.Context, before time.Time) (int64, error) {
	defer metrics.ObserveQuery("delete", "refresh_tokens")()

	const query = `DELETE FROM refresh_tokens WHERE expires_at < $1`

	tag, err := r.db.Exec(ctx, query, before)
	if err != nil {
		return 0, fmt.Errorf("repository.DeleteExpired: failed to prune refresh tokens: %w", err)
	}

	return tag.RowsAffected(), nil
}

package auth

import (
	"errors"
	"testing"
	"time"

	"github.com/abneribeiro/internal/testdb"
	"github.com/google/uuid"
)

// storeToken persists a fresh token and returns its id and plaintext.
func storeToken(t *testing.T, repo RefreshRepository, userID, familyID uuid.UUID, expiresAt time.Time) (uuid.UUID, string) {
	t.Helper()

	token, hash, err := newRefreshToken()
	if err != nil {
		t.Fatalf("newRefreshToken: %v", err)
	}

	id := uuid.New()
	if err := repo.Store(testdb.Context(t), id, userID, familyID, hash, expiresAt); err != nil {
		t.Fatalf("Store: %v", err)
	}

	return id, token
}

func find(t *testing.T, repo RefreshRepository, token string) RefreshToken {
	t.Helper()

	stored, err := repo.FindByHash(testdb.Context(t), hashRefreshToken(token))
	if err != nil {
		t.Fatalf("FindByHash: %v", err)
	}

	return stored
}

func TestRepositoryStoreAndFind(t *testing.T) {
	db := testdb.Pool(t)
	repo := NewRefreshRepository(db)
	userID := testdb.SeedUser(t, db)
	family := uuid.New()
	expires := time.Now().Add(time.Hour).Truncate(time.Microsecond)

	id, token := storeToken(t, repo, userID, family, expires)

	stored := find(t, repo, token)
	if stored.ID != id || stored.UserID != userID || stored.FamilyID != family {
		t.Errorf("stored = %+v, want id %v user %v family %v", stored, id, userID, family)
	}
	if !stored.ExpiresAt.Equal(expires) {
		t.Errorf("expires_at = %v, want %v", stored.ExpiresAt, expires)
	}
	if stored.revoked() {
		t.Error("a fresh token is already revoked")
	}

	// Only the hash is stored, so the plaintext must not match anything.
	var plaintextHits int
	if err := db.QueryRow(testdb.Context(t),
		`SELECT count(*) FROM refresh_tokens WHERE token_hash = $1`, []byte(token)).Scan(&plaintextHits); err != nil {
		t.Fatal(err)
	}
	if plaintextHits != 0 {
		t.Error("refresh token stored in plaintext")
	}
}

func TestRepositoryFindUnknownToken(t *testing.T) {
	repo := NewRefreshRepository(testdb.Pool(t))

	_, err := repo.FindByHash(testdb.Context(t), hashRefreshToken("never-issued"))
	if !errors.Is(err, ErrInvalidRefresh) {
		t.Errorf("err = %v, want ErrInvalidRefresh", err)
	}
}

func TestRepositoryRevokeReportsOnlyTheFirstCall(t *testing.T) {
	db := testdb.Pool(t)
	repo := NewRefreshRepository(db)
	ctx := testdb.Context(t)
	userID := testdb.SeedUser(t, db)

	id, token := storeToken(t, repo, userID, uuid.New(), time.Now().Add(time.Hour))

	revoked, err := repo.Revoke(ctx, id)
	if err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if !revoked {
		t.Error("first Revoke reported the token as already revoked")
	}
	first := find(t, repo, token)
	if !first.revoked() {
		t.Fatal("token not revoked")
	}

	// A second revoke must not move revoked_at: it records when the chain
	// was first cut, which reuse investigations depend on.
	// A second revoke reports false: that is how a concurrent rotation learns
	// it lost the race.
	revoked, err = repo.Revoke(ctx, id)
	if err != nil {
		t.Fatalf("second Revoke: %v", err)
	}
	if revoked {
		t.Error("second Revoke claimed to revoke an already revoked token")
	}
	if second := find(t, repo, token); !second.RevokedAt.Equal(*first.RevokedAt) {
		t.Errorf("revoked_at moved from %v to %v", first.RevokedAt, second.RevokedAt)
	}
}

func TestRepositoryRevokeFamilyOnlyTouchesThatFamily(t *testing.T) {
	db := testdb.Pool(t)
	repo := NewRefreshRepository(db)
	userID := testdb.SeedUser(t, db)
	family, other := uuid.New(), uuid.New()
	expires := time.Now().Add(time.Hour)

	_, a := storeToken(t, repo, userID, family, expires)
	_, b := storeToken(t, repo, userID, family, expires)
	_, c := storeToken(t, repo, userID, other, expires)

	if err := repo.RevokeFamily(testdb.Context(t), family); err != nil {
		t.Fatalf("RevokeFamily: %v", err)
	}

	if !find(t, repo, a).revoked() || !find(t, repo, b).revoked() {
		t.Error("a token of the revoked family is still live")
	}
	if find(t, repo, c).revoked() {
		t.Error("a token of another family was revoked")
	}
}

func TestRepositoryRevokeAllForUser(t *testing.T) {
	db := testdb.Pool(t)
	repo := NewRefreshRepository(db)
	alice, bob := testdb.SeedUser(t, db), testdb.SeedUser(t, db)
	expires := time.Now().Add(time.Hour)

	_, a1 := storeToken(t, repo, alice, uuid.New(), expires)
	_, a2 := storeToken(t, repo, alice, uuid.New(), expires)
	_, b1 := storeToken(t, repo, bob, uuid.New(), expires)

	if err := repo.RevokeAllForUser(testdb.Context(t), alice); err != nil {
		t.Fatalf("RevokeAllForUser: %v", err)
	}

	if !find(t, repo, a1).revoked() || !find(t, repo, a2).revoked() {
		t.Error("one of alice's sessions survived")
	}
	if find(t, repo, b1).revoked() {
		t.Error("bob's session was revoked by alice's password change")
	}
}

func TestRepositoryDeleteExpired(t *testing.T) {
	db := testdb.Pool(t)
	repo := NewRefreshRepository(db)
	userID := testdb.SeedUser(t, db)
	now := time.Now()

	_, expired := storeToken(t, repo, userID, uuid.New(), now.Add(-time.Hour))
	_, live := storeToken(t, repo, userID, uuid.New(), now.Add(time.Hour))

	deleted, err := repo.DeleteExpired(testdb.Context(t), now)
	if err != nil {
		t.Fatalf("DeleteExpired: %v", err)
	}
	// Other test runs may leave expired rows behind, so only a floor holds.
	if deleted < 1 {
		t.Errorf("deleted = %d, want at least the one expired token", deleted)
	}

	if _, err := repo.FindByHash(testdb.Context(t), hashRefreshToken(expired)); !errors.Is(err, ErrInvalidRefresh) {
		t.Errorf("expired token still present: %v", err)
	}
	find(t, repo, live)
}

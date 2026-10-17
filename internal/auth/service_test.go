package auth

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/abneribeiro/internal/jwt"
	"github.com/abneribeiro/internal/user"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

var errDatabaseDown = errors.New("connection refused")

type fakeUserRepo struct {
	user        user.User
	err         error
	getByIDErr  error
	gotEmail    string
	emailLookup int
}

func (f *fakeUserRepo) GetByEmail(_ context.Context, email string) (user.User, error) {
	f.gotEmail = email
	f.emailLookup++
	return f.user, f.err
}

func (f *fakeUserRepo) Create(context.Context, uuid.UUID, user.CreateInput) (user.User, error) {
	return user.User{}, nil
}
func (f *fakeUserRepo) GetByID(context.Context, uuid.UUID) (user.User, error) {
	return f.user, f.getByIDErr
}
func (f *fakeUserRepo) List(context.Context, user.ListFilter) ([]user.User, error) { return nil, nil }
func (f *fakeUserRepo) UpdateRole(context.Context, uuid.UUID, string) (user.User, error) {
	return user.User{}, nil
}
func (f *fakeUserRepo) Update(context.Context, uuid.UUID, user.UpdateInput) (user.User, error) {
	return user.User{}, nil
}
func (f *fakeUserRepo) Delete(context.Context, uuid.UUID) error { return nil }

// fakeRefreshRepo is an in-memory stand-in for the refresh_tokens table.
type fakeRefreshRepo struct {
	tokens map[string]*RefreshToken // keyed by hex of the hash
	err    error

	// rotatedConcurrently makes Revoke behave as if another request had
	// revoked the token between FindByHash and Revoke.
	rotatedConcurrently bool
}

func newFakeRefreshRepo() *fakeRefreshRepo {
	return &fakeRefreshRepo{tokens: make(map[string]*RefreshToken)}
}

func key(hash []byte) string { return fmt.Sprintf("%x", hash) }

func (f *fakeRefreshRepo) Store(_ context.Context, id, userID, familyID uuid.UUID, hash []byte, expiresAt time.Time) error {
	if f.err != nil {
		return f.err
	}
	f.tokens[key(hash)] = &RefreshToken{ID: id, UserID: userID, FamilyID: familyID, ExpiresAt: expiresAt}
	return nil
}

func (f *fakeRefreshRepo) FindByHash(_ context.Context, hash []byte) (RefreshToken, error) {
	t, ok := f.tokens[key(hash)]
	if !ok {
		return RefreshToken{}, ErrInvalidRefresh
	}
	return *t, nil
}

func (f *fakeRefreshRepo) Revoke(_ context.Context, id uuid.UUID) (bool, error) {
	now := time.Now()
	for _, t := range f.tokens {
		if t.ID == id {
			if f.rotatedConcurrently {
				t.RevokedAt = &now
			}
			if t.RevokedAt != nil {
				return false, nil
			}
			t.RevokedAt = &now
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeRefreshRepo) RevokeFamily(_ context.Context, familyID uuid.UUID) error {
	now := time.Now()
	for _, t := range f.tokens {
		if t.FamilyID == familyID {
			t.RevokedAt = &now
		}
	}
	return nil
}

func (f *fakeRefreshRepo) RevokeAllForUser(_ context.Context, userID uuid.UUID) error {
	now := time.Now()
	for _, t := range f.tokens {
		if t.UserID == userID {
			t.RevokedAt = &now
		}
	}
	return nil
}

func (f *fakeRefreshRepo) DeleteExpired(_ context.Context, before time.Time) (int64, error) {
	var n int64
	for k, t := range f.tokens {
		if t.ExpiresAt.Before(before) {
			delete(f.tokens, k)
			n++
		}
	}
	return n, nil
}

// liveTokens counts tokens that have not been revoked.
func (f *fakeRefreshRepo) liveTokens() int {
	n := 0
	for _, t := range f.tokens {
		if t.RevokedAt == nil {
			n++
		}
	}
	return n
}

func testManager() *jwt.Manager {
	return jwt.NewManager("test-secret-at-least-32-characters-long")
}

func registeredUser(t *testing.T, password string) user.User {
	t.Helper()

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	return user.User{ID: uuid.New(), Email: "alice@example.com", Password: string(hash)}
}

func TestLoginSuccess(t *testing.T) {
	u := registeredUser(t, "password123")
	repo := &fakeUserRepo{user: u}
	manager := testManager()

	resp, err := NewService(repo, newFakeRefreshRepo(), manager).Login(context.Background(), LoginInput{
		Email: "alice@example.com", Password: "password123",
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	if resp.UserID != u.ID {
		t.Errorf("UserID = %v, want %v", resp.UserID, u.ID)
	}

	claims, err := manager.ValidateToken(resp.Token)
	if err != nil {
		t.Fatalf("issued token does not validate: %v", err)
	}
	if claims.UserID != u.ID {
		t.Errorf("token UserID = %v, want %v", claims.UserID, u.ID)
	}
}

func TestLoginNormalizesEmailBeforeLookup(t *testing.T) {
	repo := &fakeUserRepo{user: registeredUser(t, "password123")}

	_, err := NewService(repo, newFakeRefreshRepo(), testManager()).Login(context.Background(), LoginInput{
		Email: "  ALICE@Example.COM ", Password: "password123",
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	if repo.gotEmail != "alice@example.com" {
		t.Errorf("looked up %q, want the normalized address", repo.gotEmail)
	}
}

func TestLoginWrongPassword(t *testing.T) {
	repo := &fakeUserRepo{user: registeredUser(t, "password123")}

	_, err := NewService(repo, newFakeRefreshRepo(), testManager()).Login(context.Background(), LoginInput{
		Email: "alice@example.com", Password: "wrongpassword",
	})

	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
}

func TestLoginUnknownUser(t *testing.T) {
	repo := &fakeUserRepo{err: user.ErrUserNotFound}

	_, err := NewService(repo, newFakeRefreshRepo(), testManager()).Login(context.Background(), LoginInput{
		Email: "nobody@example.com", Password: "password123",
	})

	if !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("err = %v, want ErrUserNotFound", err)
	}
}

// A dead database used to be reported as ErrUserNotFound, which the handler
// turned into 401. Every login during an outage looked like bad credentials
// and nothing was logged.
func TestLoginInfrastructureErrorIsNotACredentialFailure(t *testing.T) {
	repo := &fakeUserRepo{err: errDatabaseDown}

	_, err := NewService(repo, newFakeRefreshRepo(), testManager()).Login(context.Background(), LoginInput{
		Email: "alice@example.com", Password: "password123",
	})

	if err == nil {
		t.Fatal("got nil error, want the infrastructure failure")
	}
	if errors.Is(err, ErrUserNotFound) || errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("infrastructure failure reported as a credential error: %v", err)
	}
	if !errors.Is(err, errDatabaseDown) {
		t.Errorf("underlying cause was dropped, err = %v", err)
	}
}

func TestLoginIssuesBothTokens(t *testing.T) {
	repo := &fakeUserRepo{user: registeredUser(t, "password123")}
	refresh := newFakeRefreshRepo()

	resp, err := NewService(repo, refresh, testManager()).Login(context.Background(), LoginInput{
		Email: "alice@example.com", Password: "password123",
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	if resp.Token == "" {
		t.Error("no access token issued")
	}
	if resp.RefreshToken == "" {
		t.Error("no refresh token issued")
	}
	if resp.ExpiresIn <= 0 {
		t.Errorf("expires_in = %d, want the access token TTL in seconds", resp.ExpiresIn)
	}
	if refresh.liveTokens() != 1 {
		t.Errorf("stored %d live refresh tokens, want 1", refresh.liveTokens())
	}

	// The plaintext must never be persisted; only its hash is a key.
	for k := range refresh.tokens {
		if k == resp.RefreshToken {
			t.Error("refresh token stored in plaintext")
		}
	}
}

func TestRefreshRotatesTheToken(t *testing.T) {
	u := registeredUser(t, "password123")
	repo := &fakeUserRepo{user: u}
	refresh := newFakeRefreshRepo()
	svc := NewService(repo, refresh, testManager())
	ctx := context.Background()

	login, err := svc.Login(ctx, LoginInput{Email: "alice@example.com", Password: "password123"})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	rotated, err := svc.Refresh(ctx, RefreshInput{RefreshToken: login.RefreshToken})
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	if rotated.RefreshToken == login.RefreshToken {
		t.Error("refresh token was not rotated")
	}
	if rotated.UserID != u.ID {
		t.Errorf("UserID = %v, want %v", rotated.UserID, u.ID)
	}
	if refresh.liveTokens() != 1 {
		t.Errorf("%d live tokens after rotation, want only the new one", refresh.liveTokens())
	}
}

// Replaying an already-rotated token means it leaked: the whole chain dies,
// not just that one request.
func TestRefreshReuseRevokesTheFamily(t *testing.T) {
	repo := &fakeUserRepo{user: registeredUser(t, "password123")}
	refresh := newFakeRefreshRepo()
	svc := NewService(repo, refresh, testManager())
	ctx := context.Background()

	login, err := svc.Login(ctx, LoginInput{Email: "alice@example.com", Password: "password123"})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	rotated, err := svc.Refresh(ctx, RefreshInput{RefreshToken: login.RefreshToken})
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	// Replay the old one.
	if _, err := svc.Refresh(ctx, RefreshInput{RefreshToken: login.RefreshToken}); !errors.Is(err, ErrInvalidRefresh) {
		t.Fatalf("replay err = %v, want ErrInvalidRefresh", err)
	}

	if refresh.liveTokens() != 0 {
		t.Errorf("%d tokens still live after reuse detection, want 0", refresh.liveTokens())
	}

	// The token the legitimate client holds must be dead too.
	if _, err := svc.Refresh(ctx, RefreshInput{RefreshToken: rotated.RefreshToken}); !errors.Is(err, ErrInvalidRefresh) {
		t.Errorf("current token still works after family revocation: %v", err)
	}
}

// Two requests presenting the same token at once must not both rotate it:
// the one that loses the revoke is treated as reuse, so a thief racing the
// legitimate client cannot slip past detection.
func TestRefreshRaceIsTreatedAsReuse(t *testing.T) {
	repo := &fakeUserRepo{user: registeredUser(t, "password123")}
	refresh := newFakeRefreshRepo()
	svc := NewService(repo, refresh, testManager())
	ctx := context.Background()

	login, err := svc.Login(ctx, LoginInput{Email: "alice@example.com", Password: "password123"})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	refresh.rotatedConcurrently = true

	if _, err := svc.Refresh(ctx, RefreshInput{RefreshToken: login.RefreshToken}); !errors.Is(err, ErrInvalidRefresh) {
		t.Fatalf("err = %v, want ErrInvalidRefresh", err)
	}
	if refresh.liveTokens() != 0 {
		t.Errorf("%d tokens still live after losing the rotation race, want 0", refresh.liveTokens())
	}
	if len(refresh.tokens) != 1 {
		t.Errorf("%d tokens stored, want no new token issued", len(refresh.tokens))
	}
}

func TestRefreshRejectsUnknownAndExpired(t *testing.T) {
	repo := &fakeUserRepo{user: registeredUser(t, "password123")}
	refresh := newFakeRefreshRepo()
	svc := NewService(repo, refresh, testManager())
	ctx := context.Background()

	t.Run("unknown token", func(t *testing.T) {
		if _, err := svc.Refresh(ctx, RefreshInput{RefreshToken: "nope"}); !errors.Is(err, ErrInvalidRefresh) {
			t.Errorf("err = %v, want ErrInvalidRefresh", err)
		}
	})

	t.Run("empty token", func(t *testing.T) {
		if _, err := svc.Refresh(ctx, RefreshInput{}); !errors.Is(err, ErrInvalidRefresh) {
			t.Errorf("err = %v, want ErrInvalidRefresh", err)
		}
	})

	t.Run("expired token", func(t *testing.T) {
		login, err := svc.Login(ctx, LoginInput{Email: "alice@example.com", Password: "password123"})
		if err != nil {
			t.Fatal(err)
		}

		for _, stored := range refresh.tokens {
			stored.ExpiresAt = time.Now().Add(-time.Hour)
		}

		if _, err := svc.Refresh(ctx, RefreshInput{RefreshToken: login.RefreshToken}); !errors.Is(err, ErrInvalidRefresh) {
			t.Errorf("err = %v, want ErrInvalidRefresh", err)
		}
	})
}

func TestLogoutRevokesTheSession(t *testing.T) {
	repo := &fakeUserRepo{user: registeredUser(t, "password123")}
	refresh := newFakeRefreshRepo()
	svc := NewService(repo, refresh, testManager())
	ctx := context.Background()

	login, err := svc.Login(ctx, LoginInput{Email: "alice@example.com", Password: "password123"})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	if err := svc.Logout(ctx, RefreshInput{RefreshToken: login.RefreshToken}); err != nil {
		t.Fatalf("Logout: %v", err)
	}

	if refresh.liveTokens() != 0 {
		t.Errorf("%d live tokens after logout, want 0", refresh.liveTokens())
	}

	if _, err := svc.Refresh(ctx, RefreshInput{RefreshToken: login.RefreshToken}); !errors.Is(err, ErrInvalidRefresh) {
		t.Errorf("token still usable after logout: %v", err)
	}
}

// Logging out with a token the server has never seen still leaves the caller
// logged out, so it is not reported as an error.
func TestLogoutWithUnknownTokenSucceeds(t *testing.T) {
	svc := NewService(&fakeUserRepo{}, newFakeRefreshRepo(), testManager())

	if err := svc.Logout(context.Background(), RefreshInput{RefreshToken: "never-issued"}); err != nil {
		t.Errorf("Logout: %v", err)
	}
}

func TestAccessTokenCarriesTheRole(t *testing.T) {
	u := registeredUser(t, "password123")
	u.Role = "admin"
	repo := &fakeUserRepo{user: u}
	manager := testManager()

	resp, err := NewService(repo, newFakeRefreshRepo(), manager).Login(context.Background(), LoginInput{
		Email: "alice@example.com", Password: "password123",
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	claims, err := manager.ValidateToken(resp.Token)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if claims.Role != "admin" {
		t.Errorf("role claim = %q, want admin", claims.Role)
	}
	if resp.Role != "admin" {
		t.Errorf("response role = %q, want admin", resp.Role)
	}
}

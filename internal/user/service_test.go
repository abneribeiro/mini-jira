package user

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// fakeRepo records what the service hands the repository and returns
// whatever the test configures.
type fakeRepo struct {
	createdID    uuid.UUID
	createdInput CreateInput
	createErr    error

	updatedID    uuid.UUID
	updatedInput UpdateInput
	updateErr    error

	getErr    error
	deletedID uuid.UUID
	deleteErr error

	user User
}

func (f *fakeRepo) Create(_ context.Context, id uuid.UUID, input CreateInput) (User, error) {
	f.createdID, f.createdInput = id, input
	return f.user, f.createErr
}

func (f *fakeRepo) Update(_ context.Context, id uuid.UUID, input UpdateInput) (User, error) {
	f.updatedID, f.updatedInput = id, input
	return f.user, f.updateErr
}

func (f *fakeRepo) GetByID(_ context.Context, _ uuid.UUID) (User, error) {
	return f.user, f.getErr
}

func (f *fakeRepo) GetByEmail(_ context.Context, _ string) (User, error) {
	return f.user, f.getErr
}

func (f *fakeRepo) List(_ context.Context, _ ListFilter) ([]User, error) {
	return []User{f.user}, nil
}

func (f *fakeRepo) UpdateRole(_ context.Context, id uuid.UUID, role string) (User, error) {
	f.updatedID = id
	f.user.Role = role
	return f.user, f.updateErr
}

func (f *fakeRepo) Delete(_ context.Context, id uuid.UUID) error {
	f.deletedID = id
	return f.deleteErr
}

func validCreate() CreateInput {
	return CreateInput{Name: "Valid Name", Email: "valid@example.com", Password: "password123"}
}

func TestCreateValidation(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*CreateInput)
		wantErr  bool
		contains string
	}{
		{"valid", func(*CreateInput) {}, false, ""},
		{"empty name", func(c *CreateInput) { c.Name = "" }, true, "name is required"},
		{"blank name", func(c *CreateInput) { c.Name = "   " }, true, "name is required"},
		{"name too long", func(c *CreateInput) { c.Name = strings.Repeat("a", 51) }, true, "at most 50"},
		// Regression: every validation failure used to report "name is
		// required" because validateUser's real error was discarded.
		{"malformed email", func(c *CreateInput) { c.Email = "not-an-email" }, true, "plain address"},
		{"email with display name", func(c *CreateInput) { c.Email = "X <a@b.co>" }, true, "plain address"},
		{"empty email", func(c *CreateInput) { c.Email = "" }, true, "email is required"},
		{"email too long", func(c *CreateInput) { c.Email = strings.Repeat("a", 200) + "@x.co" }, true, "at most 200"},
		{"empty password", func(c *CreateInput) { c.Password = "" }, true, "password is required"},
		{"password too short", func(c *CreateInput) { c.Password = "short" }, true, "at least 8"},
		{"password too long", func(c *CreateInput) { c.Password = strings.Repeat("a", 73) }, true, "at most 72"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := validCreate()
			tt.mutate(&input)

			_, err := NewService(&fakeRepo{}).Create(context.Background(), input)

			if !tt.wantErr {
				if err != nil {
					t.Fatalf("got error %v, want nil", err)
				}
				return
			}

			if err == nil {
				t.Fatal("got nil error, want a validation failure")
			}
			if !errors.Is(err, ErrInvalidUser) {
				t.Errorf("errors.Is(err, ErrInvalidUser) = false, err = %v", err)
			}
			if !strings.Contains(err.Error(), tt.contains) {
				t.Errorf("error = %q, want it to mention %q", err, tt.contains)
			}
		})
	}
}

func TestCreateNormalizesEmailAndHashesPassword(t *testing.T) {
	repo := &fakeRepo{}
	input := validCreate()
	input.Email = "  Alice@EXAMPLE.com  "

	if _, err := NewService(repo).Create(context.Background(), input); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if got := repo.createdInput.Email; got != "alice@example.com" {
		t.Errorf("email stored as %q, want it trimmed and lowercased", got)
	}

	if repo.createdInput.Password == input.Password {
		t.Fatal("password reached the repository in plaintext")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(repo.createdInput.Password), []byte("password123")); err != nil {
		t.Errorf("stored value is not a bcrypt hash of the password: %v", err)
	}

	if repo.createdID == uuid.Nil {
		t.Error("service did not generate an ID")
	}
}

func TestCreatePropagatesDuplicateEmail(t *testing.T) {
	repo := &fakeRepo{createErr: ErrEmailAlreadyExists}

	_, err := NewService(repo).Create(context.Background(), validCreate())

	if !errors.Is(err, ErrEmailAlreadyExists) {
		t.Fatalf("errors.Is(err, ErrEmailAlreadyExists) = false, err = %v", err)
	}
}

// Update used to run no validation at all, so every Create rule was
// bypassable through PUT /users/{id}.
func TestUpdateValidation(t *testing.T) {
	ptr := func(s string) *string { return &s }

	tests := []struct {
		name     string
		input    UpdateInput
		contains string
	}{
		{"empty name", UpdateInput{Name: ptr("")}, "name is required"},
		{"name too long", UpdateInput{Name: ptr(strings.Repeat("a", 51))}, "at most 50"},
		{"malformed email", UpdateInput{Email: ptr("garbage")}, "plain address"},
		{"password too short", UpdateInput{Password: ptr("abc")}, "at least 8"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewService(&fakeRepo{}).Update(context.Background(), uuid.New(), tt.input)

			if err == nil {
				t.Fatal("got nil error, want a validation failure")
			}
			if !errors.Is(err, ErrInvalidUser) {
				t.Errorf("errors.Is(err, ErrInvalidUser) = false, err = %v", err)
			}
			if !strings.Contains(err.Error(), tt.contains) {
				t.Errorf("error = %q, want it to mention %q", err, tt.contains)
			}
		})
	}
}

func TestUpdateNormalizesAndHashes(t *testing.T) {
	repo := &fakeRepo{}
	email := "  Bob@EXAMPLE.com "
	password := "newpassword1"

	_, err := NewService(repo).Update(context.Background(), uuid.New(), UpdateInput{
		Email:    &email,
		Password: &password,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if got := *repo.updatedInput.Email; got != "bob@example.com" {
		t.Errorf("email stored as %q, want it normalized", got)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(*repo.updatedInput.Password), []byte(password)); err != nil {
		t.Errorf("password was not hashed before reaching the repository: %v", err)
	}
}

func TestUpdatePropagatesNotFound(t *testing.T) {
	repo := &fakeRepo{updateErr: ErrUserNotFound}
	name := "Whoever"

	_, err := NewService(repo).Update(context.Background(), uuid.New(), UpdateInput{Name: &name})

	// The old service wrapped this as "failed to save user: ...", which broke
	// the handler's not-found detection and produced a 400 instead of a 404.
	if !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("errors.Is(err, ErrUserNotFound) = false, err = %v", err)
	}
}

func TestNilIDRejected(t *testing.T) {
	svc := NewService(&fakeRepo{})
	ctx := context.Background()

	if _, err := svc.GetByID(ctx, uuid.Nil); !errors.Is(err, ErrInvalidUser) {
		t.Errorf("GetByID(uuid.Nil) = %v, want ErrInvalidUser", err)
	}
	if _, err := svc.Update(ctx, uuid.Nil, UpdateInput{}); !errors.Is(err, ErrInvalidUser) {
		t.Errorf("Update(uuid.Nil) = %v, want ErrInvalidUser", err)
	}
	if err := svc.Delete(ctx, uuid.Nil); !errors.Is(err, ErrInvalidUser) {
		t.Errorf("Delete(uuid.Nil) = %v, want ErrInvalidUser", err)
	}
}

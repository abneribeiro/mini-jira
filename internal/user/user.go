package user

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/abneribeiro/internal/authz"
	"github.com/google/uuid"
)

var (
	ErrInvalidUser        = errors.New("invalid user data")
	ErrInvalidRole        = errors.New("invalid role")
	ErrUserNotFound       = errors.New("user not found")
	ErrEmailAlreadyExists = errors.New("email already registered")
	ErrNoFieldsToUpdate   = errors.New("no fields to update")
)

const (
	maxNameLen     = 50
	maxEmailLen    = 200
	minPasswordLen = 8
	maxPasswordBytes = 72
	DefaultListLimit = 50
	MaxListLimit     = 200
)

type User struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	Password  string    `json:"-"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type CreateInput struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

// UpdateInput deliberately has no Role field: a role change goes through
// PATCH /users/{id}/role, which is admin-only. Accepting it here would let
// any caller promote themselves with an ordinary profile update.
type UpdateInput struct {
	Name     *string `json:"name"`
	Email    *string `json:"email"`
	Password *string `json:"password"`
}

// ListFilter paginates GET /users.
type ListFilter struct {
	Limit  int
	Offset int
}

// NormalizeEmail trims and lowercases so that A@x.com and a@x.com collide on
// the UNIQUE index instead of becoming two accounts.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func validateName(name string) error {
	switch {
	case strings.TrimSpace(name) == "":
		return fmt.Errorf("%w: name is required", ErrInvalidUser)
	case len([]rune(name)) > maxNameLen:
		return fmt.Errorf("%w: name must be at most %d characters", ErrInvalidUser, maxNameLen)
	}

	return nil
}

func validateEmail(email string) error {
	switch {
	case strings.TrimSpace(email) == "":
		return fmt.Errorf("%w: email is required", ErrInvalidUser)
	case len([]rune(email)) > maxEmailLen:
		return fmt.Errorf("%w: email must be at most %d characters", ErrInvalidUser, maxEmailLen)
	}

	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Name != "" || addr.Address != email {
		return fmt.Errorf("%w: expected a plain address like name@example.com", ErrInvalidUser)
	}

	return nil
}

func validatePassword(password string) error {
	switch {
	case strings.TrimSpace(password) == "":
		return fmt.Errorf("%w: password is required", ErrInvalidUser)
	case len([]rune(strings.TrimSpace(password))) < minPasswordLen:
		return fmt.Errorf("%w: password must be at least %d characters", ErrInvalidUser, minPasswordLen)
	case len(password) > maxPasswordBytes:
		return fmt.Errorf("%w: password must be at most %d bytes", ErrInvalidUser, maxPasswordBytes)
	}

	return nil
}

func validateUser(name, email string) error {
	if err := validateName(name); err != nil {
		return err
	}

	return validateEmail(email)
}

func validateRole(role string) error {
	if !authz.ValidRole(role) {
		return fmt.Errorf("%w: role must be %q or %q", ErrInvalidRole, authz.RoleMember, authz.RoleAdmin)
	}

	return nil
}

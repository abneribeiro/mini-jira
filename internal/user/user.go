package user

import (
	"fmt"
	"net/mail"
	"time"
)

type User struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Password  string    `json:"-"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type CreateInput struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type UpdateInput struct {
	Name     *string `json:"name"`
	Email    *string `json:"email"`
	Password *string `json:"password"`
}

func validateUser(name, email string) error {
	switch {
	case name == "" || email == "":
		return fmt.Errorf("%w: name and email are required", ErrInvalidUser)
	case len([]rune(name)) > 50:
		return fmt.Errorf("%w: name must be at most 50 characters", ErrInvalidUser)
	case len([]rune(email)) > 200:
		return fmt.Errorf("%w: email must be at most 200 characters", ErrInvalidUser)

	}

	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Name != "" || addr.Address != email {
		return fmt.Errorf("%w: expected a plain address like name@example.com", ErrInvalidUser)
	}

	return nil
}

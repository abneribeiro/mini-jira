package database

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// Postgres SQLSTATE codes the repositories translate into domain errors.
const (
	codeUniqueViolation     = "23505"
	codeForeignKeyViolation = "23503"
)

// UniqueViolation reports whether err is a 23505 on the given constraint.
// An empty constraint matches any unique violation.
func UniqueViolation(err error, constraint string) bool {
	return isPgError(err, codeUniqueViolation, constraint)
}

// ForeignKeyViolation reports whether err is a 23503 on the given constraint.
// An empty constraint matches any foreign key violation.
func ForeignKeyViolation(err error, constraint string) bool {
	return isPgError(err, codeForeignKeyViolation, constraint)
}

func isPgError(err error, code, constraint string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != code {
		return false
	}

	return constraint == "" || pgErr.ConstraintName == constraint
}

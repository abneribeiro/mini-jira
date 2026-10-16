// Package authz carries the caller's identity and privilege level from the
// auth middleware down to the repositories, so ownership can be enforced in
// SQL rather than re-checked in each layer.
package authz

import "github.com/google/uuid"

// Roles stored in users.role.
const (
	RoleMember = "member"
	RoleAdmin  = "admin"
)

// Scope is who is asking. An admin is not restricted to rows they own.
type Scope struct {
	UserID  uuid.UUID
	IsAdmin bool
}

func NewScope(userID uuid.UUID, role string) Scope {
	return Scope{UserID: userID, IsAdmin: role == RoleAdmin}
}

// CanAccess reports whether this caller may act on a row owned by ownerID.
// Repositories pass IsAdmin into the query instead of calling this, so the
// check happens in the same statement as the write.
func (s Scope) CanAccess(ownerID uuid.UUID) bool {
	return s.IsAdmin || s.UserID == ownerID
}

// ValidRole reports whether role is one the system recognises.
func ValidRole(role string) bool {
	return role == RoleMember || role == RoleAdmin
}

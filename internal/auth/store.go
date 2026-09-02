package auth

import (
	"context"
	"errors"
)

var (
	// ErrAdminExists is returned when an admin account already exists and a
	// first-admin creation is attempted again.
	ErrAdminExists = errors.New("auth: admin account already exists")
	// ErrAdminNotFound is returned when no account matches the username.
	ErrAdminNotFound = errors.New("auth: admin account not found")
)

// Account is a stored admin credential. PasswordHash is always a bcrypt
// digest; plaintext passwords never reach the store.
type Account struct {
	Username     string
	PasswordHash string
}

// Store persists admin accounts. The gateway is the only writer (init) and
// reader (login); the MySQL implementation backs both.
type Store interface {
	Initialized(ctx context.Context) (bool, error)
	// CreateFirstAdmin stores the first account or fails with ErrAdminExists
	// when any account already exists — enforced atomically.
	CreateFirstAdmin(ctx context.Context, username, passwordHash string) error
	// AccountByUsername returns the account or ErrAdminNotFound.
	AccountByUsername(ctx context.Context, username string) (Account, error)
}

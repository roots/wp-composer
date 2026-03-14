package auth

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type User struct {
	ID           int64
	Email        string
	Name         string
	PasswordHash string
	IsAdmin      bool
	CreatedAt    string
	UpdatedAt    string
}

func CreateUser(ctx context.Context, db *sql.DB, email, name, passwordHash string, isAdmin bool) (*User, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	admin := 0
	if isAdmin {
		admin = 1
	}

	result, err := db.ExecContext(ctx,
		`INSERT INTO users (email, name, password_hash, is_admin, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		email, name, passwordHash, admin, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("inserting user: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("getting user id: %w", err)
	}

	return &User{
		ID:           id,
		Email:        email,
		Name:         name,
		PasswordHash: passwordHash,
		IsAdmin:      isAdmin,
		CreatedAt:    now,
		UpdatedAt:    now,
	}, nil
}

func GetUserByEmail(ctx context.Context, db *sql.DB, email string) (*User, error) {
	u := &User{}
	var isAdmin int
	err := db.QueryRowContext(ctx,
		`SELECT id, email, name, password_hash, is_admin, created_at, updated_at
		 FROM users WHERE email = ?`, email,
	).Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &isAdmin, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("querying user by email: %w", err)
	}
	u.IsAdmin = isAdmin == 1
	return u, nil
}

func PromoteToAdmin(ctx context.Context, db *sql.DB, email string) error {
	result, err := db.ExecContext(ctx,
		`UPDATE users SET is_admin = 1, updated_at = ? WHERE email = ?`,
		time.Now().UTC().Format(time.RFC3339), email,
	)
	if err != nil {
		return fmt.Errorf("promoting user: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("no user found with email %s", email)
	}
	return nil
}

func UpdatePassword(ctx context.Context, db *sql.DB, email, newHash string) error {
	result, err := db.ExecContext(ctx,
		`UPDATE users SET password_hash = ?, updated_at = ? WHERE email = ?`,
		newHash, time.Now().UTC().Format(time.RFC3339), email,
	)
	if err != nil {
		return fmt.Errorf("updating password: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("no user found with email %s", email)
	}
	return nil
}

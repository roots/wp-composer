package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"
)

func CreateSession(ctx context.Context, db *sql.DB, userID int64, lifetimeMinutes int) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating session id: %w", err)
	}
	id := hex.EncodeToString(b)

	now := time.Now().UTC()
	expiresAt := now.Add(time.Duration(lifetimeMinutes) * time.Minute)

	_, err := db.ExecContext(ctx,
		`INSERT INTO sessions (id, user_id, expires_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?)`,
		id, userID, expiresAt.Format(time.RFC3339), now.Format(time.RFC3339), now.Format(time.RFC3339),
	)
	if err != nil {
		return "", fmt.Errorf("inserting session: %w", err)
	}

	return id, nil
}

func ValidateSession(ctx context.Context, db *sql.DB, sessionID string) (*User, error) {
	var u User
	var isAdmin int
	err := db.QueryRowContext(ctx,
		`SELECT u.id, u.email, u.name, u.is_admin, u.created_at, u.updated_at
		 FROM sessions s
		 JOIN users u ON u.id = s.user_id
		 WHERE s.id = ? AND s.expires_at > ?`,
		sessionID, time.Now().UTC().Format(time.RFC3339),
	).Scan(&u.ID, &u.Email, &u.Name, &isAdmin, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("validating session: %w", err)
	}
	u.IsAdmin = isAdmin == 1
	return &u, nil
}

func DeleteSession(ctx context.Context, db *sql.DB, sessionID string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, sessionID)
	if err != nil {
		return fmt.Errorf("deleting session: %w", err)
	}
	return nil
}

func CleanupExpiredSessions(ctx context.Context, db *sql.DB) (int64, error) {
	result, err := db.ExecContext(ctx,
		`DELETE FROM sessions WHERE expires_at < ?`,
		time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return 0, fmt.Errorf("cleaning up sessions: %w", err)
	}
	return result.RowsAffected()
}

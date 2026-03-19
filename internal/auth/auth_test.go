package auth

import (
	"context"
	"database/sql"
	"testing"

	"github.com/roots/wp-packages/internal/db"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}

	_, _ = database.Exec(`
		CREATE TABLE users (
			id INTEGER PRIMARY KEY,
			email TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL,
			password_hash TEXT NOT NULL,
			is_admin INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);
		CREATE TABLE sessions (
			id TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			expires_at TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);
	`)
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func TestCreateUser(t *testing.T) {
	database := setupTestDB(t)
	ctx := context.Background()

	hash, _ := HashPassword("test123")
	user, err := CreateUser(ctx, database, "admin@example.com", "Admin", hash, true)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if user.Email != "admin@example.com" {
		t.Errorf("email = %s", user.Email)
	}
	if !user.IsAdmin {
		t.Error("should be admin")
	}
}

func TestCreateUser_DuplicateEmail(t *testing.T) {
	database := setupTestDB(t)
	ctx := context.Background()

	hash, _ := HashPassword("test123")
	_, _ = CreateUser(ctx, database, "dup@example.com", "First", hash, false)
	_, err := CreateUser(ctx, database, "dup@example.com", "Second", hash, false)
	if err == nil {
		t.Error("duplicate email should fail")
	}
}

func TestGetUserByEmail(t *testing.T) {
	database := setupTestDB(t)
	ctx := context.Background()

	hash, _ := HashPassword("test123")
	_, _ = CreateUser(ctx, database, "find@example.com", "Find Me", hash, true)

	user, err := GetUserByEmail(ctx, database, "find@example.com")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if user.Name != "Find Me" {
		t.Errorf("name = %s", user.Name)
	}

	_, err = GetUserByEmail(ctx, database, "missing@example.com")
	if err == nil {
		t.Error("missing user should return error")
	}
}

func TestPromoteToAdmin(t *testing.T) {
	database := setupTestDB(t)
	ctx := context.Background()

	hash, _ := HashPassword("test123")
	_, _ = CreateUser(ctx, database, "promote@example.com", "User", hash, false)

	err := PromoteToAdmin(ctx, database, "promote@example.com")
	if err != nil {
		t.Fatalf("promote: %v", err)
	}

	user, _ := GetUserByEmail(ctx, database, "promote@example.com")
	if !user.IsAdmin {
		t.Error("should be admin after promote")
	}
}

func TestPromoteToAdmin_NotFound(t *testing.T) {
	database := setupTestDB(t)
	ctx := context.Background()

	err := PromoteToAdmin(ctx, database, "nobody@example.com")
	if err == nil {
		t.Error("promoting non-existent user should fail")
	}
}

func TestUpdatePassword(t *testing.T) {
	database := setupTestDB(t)
	ctx := context.Background()

	oldHash, _ := HashPassword("old-password")
	_, _ = CreateUser(ctx, database, "pw@example.com", "User", oldHash, false)

	newHash, _ := HashPassword("new-password")
	err := UpdatePassword(ctx, database, "pw@example.com", newHash)
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	user, _ := GetUserByEmail(ctx, database, "pw@example.com")
	if err := CheckPassword(user.PasswordHash, "new-password"); err != nil {
		t.Error("new password should validate")
	}
	if err := CheckPassword(user.PasswordHash, "old-password"); err == nil {
		t.Error("old password should not validate")
	}
}

func TestUpdatePassword_NotFound(t *testing.T) {
	database := setupTestDB(t)
	ctx := context.Background()

	hash, _ := HashPassword("test")
	err := UpdatePassword(ctx, database, "nobody@example.com", hash)
	if err == nil {
		t.Error("updating non-existent user should fail")
	}
}

func TestPasswordHashing(t *testing.T) {
	hash, err := HashPassword("my-secret")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	if err := CheckPassword(hash, "my-secret"); err != nil {
		t.Error("correct password should pass")
	}
	if err := CheckPassword(hash, "wrong"); err == nil {
		t.Error("wrong password should fail")
	}
}

func TestSession_CreateValidateDelete(t *testing.T) {
	database := setupTestDB(t)
	ctx := context.Background()

	hash, _ := HashPassword("test")
	user, _ := CreateUser(ctx, database, "session@example.com", "User", hash, true)

	// Create session
	sessionID, err := CreateSession(ctx, database, user.ID, 60)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if sessionID == "" {
		t.Fatal("session ID should not be empty")
	}

	// Validate session
	validUser, err := ValidateSession(ctx, database, sessionID)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if validUser.Email != "session@example.com" {
		t.Errorf("session user email = %s", validUser.Email)
	}

	// Delete session
	err = DeleteSession(ctx, database, sessionID)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Validate should now fail
	_, err = ValidateSession(ctx, database, sessionID)
	if err == nil {
		t.Error("deleted session should not validate")
	}
}

func TestSession_InvalidID(t *testing.T) {
	database := setupTestDB(t)
	ctx := context.Background()

	_, err := ValidateSession(ctx, database, "nonexistent-session-id")
	if err == nil {
		t.Error("invalid session ID should fail")
	}
}

func TestCleanupExpiredSessions(t *testing.T) {
	database := setupTestDB(t)
	ctx := context.Background()

	hash, _ := HashPassword("test")
	user, _ := CreateUser(ctx, database, "cleanup@example.com", "User", hash, false)

	// Create a valid session
	_, _ = CreateSession(ctx, database, user.ID, 60)

	// Create an expired session (insert directly with past expiry)
	_, _ = database.Exec(`INSERT INTO sessions (id, user_id, expires_at, created_at, updated_at)
		VALUES ('expired-session', ?, datetime('now', '-1 hour'), datetime('now', '-2 hours'), datetime('now', '-2 hours'))`, user.ID)

	deleted, err := CleanupExpiredSessions(ctx, database)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1", deleted)
	}

	// Valid session should still exist
	var count int
	_ = database.QueryRow("SELECT COUNT(*) FROM sessions").Scan(&count)
	if count != 1 {
		t.Errorf("remaining sessions = %d, want 1", count)
	}
}

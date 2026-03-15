package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func resetLockState() {
	if pipelineLockFile != nil {
		_ = syscall.Flock(int(pipelineLockFile.Fd()), syscall.LOCK_UN)
		_ = pipelineLockFile.Close()
		pipelineLockFile = nil
	}
	_ = os.Unsetenv("PIPELINE_LOCK_FD")
}

func TestAcquireLock_BlocksSecondCaller(t *testing.T) {
	t.Cleanup(resetLockState)

	lockPath := filepath.Join(t.TempDir(), "pipeline.lock")

	if err := acquireLock(lockPath); err != nil {
		t.Fatalf("first lock acquisition failed: %v", err)
	}

	// Hold the lock in pipelineLockFile; save and swap it out so the second
	// call creates its own fd.
	held := pipelineLockFile
	pipelineLockFile = nil
	t.Cleanup(func() {
		_ = syscall.Flock(int(held.Fd()), syscall.LOCK_UN)
		_ = held.Close()
	})

	err := acquireLock(lockPath)
	if err == nil {
		t.Fatal("expected second lock acquisition to fail, but it succeeded")
	}
}

func TestAcquireLock_InheritedFD_Valid(t *testing.T) {
	t.Cleanup(resetLockState)

	lockPath := filepath.Join(t.TempDir(), "pipeline.lock")

	// Simulate parent: open and lock the file.
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	})
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("flock failed: %v", err)
	}

	t.Setenv("PIPELINE_LOCK_FD", fmt.Sprintf("%d", f.Fd()))
	if err := acquireLock(lockPath); err != nil {
		t.Fatalf("inherited fd should be accepted: %v", err)
	}
}

func TestAcquireLock_InheritedFD_WrongFile(t *testing.T) {
	t.Cleanup(resetLockState)

	dir := t.TempDir()
	lockPath := filepath.Join(dir, "pipeline.lock")

	// Create the lock file so stat succeeds.
	if err := os.WriteFile(lockPath, nil, 0644); err != nil {
		t.Fatal(err)
	}

	// Open a different file and lock it.
	other, err := os.CreateTemp(dir, "other")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = other.Close() })
	_ = syscall.Flock(int(other.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)

	t.Setenv("PIPELINE_LOCK_FD", fmt.Sprintf("%d", other.Fd()))
	err = acquireLock(lockPath)
	if err == nil {
		t.Fatal("expected rejection for fd pointing to wrong file")
	}
}

func TestAcquireLock_InheritedFD_NotLocked(t *testing.T) {
	t.Cleanup(resetLockState)

	lockPath := filepath.Join(t.TempDir(), "pipeline.lock")

	// Open the correct file but don't lock it.
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })

	// Hold the lock from a *different* fd to simulate a real pipeline running.
	holder, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = syscall.Flock(int(holder.Fd()), syscall.LOCK_UN)
		_ = holder.Close()
	})
	if err := syscall.Flock(int(holder.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("flock failed: %v", err)
	}

	// f points to the right file but doesn't hold the lock — should be rejected.
	t.Setenv("PIPELINE_LOCK_FD", fmt.Sprintf("%d", f.Fd()))
	err = acquireLock(lockPath)
	if err == nil {
		t.Fatal("expected rejection for fd that doesn't hold the lock")
	}
}

package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatchFileReportsChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "model.frg")
	if err := os.WriteFile(path, []byte("#lang forge\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	changes, stop, err := watchFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	select {
	case <-changes:
		t.Fatal("watchFile reported a change before the file was modified")
	case <-time.After(2 * watchDebounce):
	}

	if err := os.WriteFile(path, []byte("#lang forge\nsig A {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	select {
	case <-changes:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the file change")
	}
}

func TestWatchFileReportsAtomicReplacement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "model.frg")
	if err := os.WriteFile(path, []byte("#lang forge\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	changes, stop, err := watchFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	replacement := filepath.Join(dir, "model.frg.tmp")
	if err := os.WriteFile(replacement, []byte("#lang forge\nsig A {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}

	select {
	case <-changes:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the replacement")
	}
}

func TestWatchFileIgnoresUnchangedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "model.frg")
	if err := os.WriteFile(path, []byte("#lang forge\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	changes, stop, err := watchFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	select {
	case <-changes:
		t.Fatal("watchFile reported a change without a write")
	case <-time.After(3 * watchDebounce):
	}
}

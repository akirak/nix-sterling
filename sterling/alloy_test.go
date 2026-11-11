package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStagedAlloyPathFollowsModuleName(t *testing.T) {
	tempDir := t.TempDir()
	model := filepath.Join(tempDir, "utilities", "types", "ordering.als")
	content := "module util/ordering[exactly elem]\n"

	staged, err := stagedAlloyPath(tempDir, model, content)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(tempDir, "util", "ordering.als")
	if staged != want {
		t.Fatalf("staged path = %q, want %q", staged, want)
	}
}

func TestStagedAlloyPathHandlesIntegerModule(t *testing.T) {
	tempDir := t.TempDir()
	model := filepath.Join(tempDir, "somewhere", "integer.als")
	content := "module util/integer\n"

	staged, err := stagedAlloyPath(tempDir, model, content)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(tempDir, "util", "integer.als")
	if staged != want {
		t.Fatalf("staged path = %q, want %q", staged, want)
	}
}

func TestStageAlloyModelSkipsConflictingIntegerModule(t *testing.T) {
	sourceDir := t.TempDir()
	model := filepath.Join(sourceDir, "ordering.als")
	if err := os.WriteFile(model, []byte("module util/ordering[exactly elem]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "integer.als"), []byte("module util/integer\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "natural.als"), []byte("module util/natural\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	staged, cleanup, err := stageAlloyModel(model)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	if _, err := os.Stat(staged); err != nil {
		t.Fatalf("staged model is missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(staged), "integer.als")); !os.IsNotExist(err) {
		t.Fatalf("conflicting integer.als should not be staged (err = %v)", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(staged), "natural.als")); err != nil {
		t.Fatalf("local module was not staged: %v", err)
	}
}

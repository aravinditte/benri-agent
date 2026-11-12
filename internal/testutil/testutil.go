// Package testutil provides shared fixtures for Go tests: a seeded temp
// SQLite database and per-role store handles on it. Test-only helper.
package testutil

import (
	"path/filepath"
	"testing"

	"github.com/aravinditte/benri-agent/internal/japanese"
	"github.com/aravinditte/benri-agent/internal/seed"
	"github.com/aravinditte/benri-agent/internal/store"
)

// RootDir resolves the repo root from internal/testutil (../../).
func RootDir() string {
	abs, _ := filepath.Abs(filepath.Join("..", ".."))
	return abs
}

// SeededDB creates a temp SQLite DB, seeds it, and returns the path plus a
// full-role handle.
func SeededDB(t *testing.T) (string, store.Repository) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.sqlite")
	full, err := store.OpenSQLite(path, store.RoleFull)
	if err != nil {
		t.Fatal(err)
	}
	tk := japanese.ForDict("ipa")
	if err := seed.Seed(full, filepath.Join(RootDir(), "fixtures"), tk); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { full.Close() })
	return path, full
}

// Handle opens another role handle on the same DB file.
func Handle(t *testing.T, path string, role store.Role) store.Repository {
	t.Helper()
	h, err := store.OpenSQLite(path, role)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.Close() })
	return h
}

// Tokenizer returns the IPA tokenizer (benchmark winner).
func Tokenizer() japanese.Tokenizer { return japanese.ForDict("ipa") }

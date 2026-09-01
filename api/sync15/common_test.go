package sync15

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetCachedTreePathUsesExplicitDirectoryWithoutEnvironment(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")

	cacheDir := filepath.Join(t.TempDir(), "application", "rmapi")
	got, err := getCachedTreePath(cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(cacheDir, "tree.cache")
	if got != want {
		t.Fatalf("getCachedTreePath() = %q, want %q", got, want)
	}

	info, err := os.Stat(cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	if gotPerm := info.Mode().Perm(); gotPerm != 0700 {
		t.Fatalf("cache directory permissions = %o, want 700", gotPerm)
	}
}

func TestGetCachedTreePathPreservesDefault(t *testing.T) {
	userCacheDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", userCacheDir)

	got, err := getCachedTreePath("")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(userCacheDir, "rmapi", "tree.cache")
	if got != want {
		t.Fatalf("getCachedTreePath() = %q, want %q", got, want)
	}
}

func TestSaveTreeUsesExplicitDirectoryWithoutEnvironment(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")

	cacheDir := filepath.Join(t.TempDir(), "rmapi")
	if err := saveTree(&HashTree{}, cacheDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "tree.cache")); err != nil {
		t.Fatal(err)
	}
}

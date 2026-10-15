package provider

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/opsbl/virtualsms/internal/config"
)

func TestDiscoverPluginsRejectsInvalidSharedObject(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "example.so")
	if err := os.WriteFile(path, []byte("placeholder"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	_, err := discoverPlugins(dir, map[string]config.InstanceConfig{"demo": {Provider: "example"}})
	if err == nil {
		t.Fatal("expected invalid plugin to fail discovery")
	}
}

func TestListPluginsShowsInvalidSharedObject(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.so")
	if err := os.WriteFile(path, []byte("invalid"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	found, err := ListPlugins(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].Path != path || found[0].Err == nil {
		t.Fatalf("unexpected plugins: %+v", found)
	}
}

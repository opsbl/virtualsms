package provider

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/opsbl/virtualsms/internal/config"
)

func TestDiscoverPlugins(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "example.so")
	if err := os.WriteFile(path, []byte("placeholder"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	available, err := discoverPlugins(dir, map[string]config.ProviderConfig{"demo": {Plugin: "example"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(available) != 1 || available["example"] != path {
		t.Fatalf("discovered plugins: %v", available)
	}
}

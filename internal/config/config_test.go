package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDuplicateDevicePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := `
[providers.demo]
type = "log"
[[devices]]
path = "/tmp/ttySMS0"
provider = "demo"
[[devices]]
path = "/tmp/ttySMS0"
provider = "demo"
`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate path error, got %v", err)
	}
}

func TestPluginDirectoryAndTypedOptions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := `
[plugins]
dir = "/usr/lib/virtualsms/plugins"
[providers.demo]
plugin = "example"
[providers.demo.options]
attempts = 2
enabled = true
[[devices]]
path = "/tmp/ttySMS0"
provider = "demo"
`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Plugins.Dir != "/usr/lib/virtualsms/plugins" || cfg.Providers["demo"].Options["attempts"] != int64(2) || cfg.Providers["demo"].Options["enabled"] != true {
		t.Fatalf("unexpected plugin config: %+v", cfg)
	}
}

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
[instances.demo]
provider = "log"
[[devices]]
path = "/tmp/ttySMS0"
instance = "demo"
[[devices]]
path = "/tmp/ttySMS0"
instance = "demo"
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
[instances.demo]
provider = "example"
[instances.demo.options]
attempts = 2
enabled = true
[[devices]]
path = "/tmp/ttySMS0"
instance = "demo"
`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Plugins.Dir != "/usr/lib/virtualsms/plugins" || cfg.Instances["demo"].Options["attempts"] != int64(2) || cfg.Instances["demo"].Options["enabled"] != true {
		t.Fatalf("unexpected plugin config: %+v", cfg)
	}
}

package config

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Metrics   MetricsConfig             `toml:"metrics"`
	Batch     BatchConfig               `toml:"batch"`
	Logging   LoggingConfig             `toml:"logging"`
	Plugins   PluginConfig              `toml:"plugins"`
	Providers map[string]ProviderConfig `toml:"providers"`
	Devices   []DeviceConfig            `toml:"devices"`
}

type BatchConfig struct {
	Wait    string `toml:"wait"`
	AckMode string `toml:"ack_mode"`
}
type LoggingConfig struct {
	Level string `toml:"level"`
}

type MetricsConfig struct {
	Listen string `toml:"listen"`
}

type PluginConfig struct {
	Dir string `toml:"dir"`
}

type ProviderConfig struct {
	Type    string         `toml:"type"`
	Plugin  string         `toml:"plugin"`
	Options map[string]any `toml:"options"`
	URL     string         `toml:"url"`
	Token   string         `toml:"token"`
	Timeout string         `toml:"timeout"`
}

type DeviceConfig struct {
	Path     string `toml:"path"`
	Provider string `toml:"provider"`
	Group    string `toml:"group"`
	Mode     uint32 `toml:"mode"`
}

func Load(path string) (Config, error) {
	var c Config
	meta, err := toml.DecodeFile(path, &c)
	if err != nil {
		return c, err
	}
	if keys := meta.Undecoded(); len(keys) > 0 {
		return c, fmt.Errorf("unknown TOML key: %s", keys[0])
	}
	if c.Metrics.Listen == "" {
		c.Metrics.Listen = "127.0.0.1:9099"
	}
	if c.Batch.Wait == "" {
		c.Batch.Wait = "3s"
	}
	if wait, err := time.ParseDuration(c.Batch.Wait); err != nil || wait <= 0 {
		return c, fmt.Errorf("batch.wait must be a positive duration")
	}
	if c.Batch.AckMode == "" {
		c.Batch.AckMode = "queued"
	}
	if c.Batch.AckMode != "queued" && c.Batch.AckMode != "sent" {
		return c, fmt.Errorf("batch.ack_mode must be queued or sent")
	}
	if c.Logging.Level == "" {
		c.Logging.Level = "info"
	}
	switch c.Logging.Level {
	case "debug", "info", "warn", "error":
	default:
		return c, fmt.Errorf("logging.level must be debug, info, warn, or error")
	}
	if len(c.Devices) == 0 {
		return c, fmt.Errorf("at least one [[devices]] entry is required")
	}
	seen := make(map[string]bool)
	for _, d := range c.Devices {
		if !filepath.IsAbs(d.Path) || filepath.Clean(d.Path) != d.Path || d.Path == "/" {
			return c, fmt.Errorf("device path %q must be a clean absolute path", d.Path)
		}
		if seen[d.Path] {
			return c, fmt.Errorf("duplicate device path %q", d.Path)
		}
		seen[d.Path] = true
		if _, ok := c.Providers[d.Provider]; !ok {
			return c, fmt.Errorf("device %q refers to unknown provider %q", d.Path, d.Provider)
		}
		if d.Mode > 0777 {
			return c, fmt.Errorf("device %q has invalid mode", d.Path)
		}
		if d.Group != "" {
			if _, err := user.LookupGroup(d.Group); err != nil {
				return c, fmt.Errorf("device %q: group %q: %w", d.Path, d.Group, err)
			}
		}
	}
	for name, p := range c.Providers {
		if p.Plugin != "" {
			if p.Type != "" {
				return c, fmt.Errorf("provider %q cannot set both type and plugin", name)
			}
			if c.Plugins.Dir == "" {
				return c, fmt.Errorf("provider %q requires plugins.dir", name)
			}
			continue
		}
		switch p.Type {
		case "log":
		case "http":
			if !strings.HasPrefix(p.URL, "https://") && !strings.HasPrefix(p.URL, "http://") {
				return c, fmt.Errorf("provider %q needs an HTTP(S) url", name)
			}
		default:
			return c, fmt.Errorf("provider %q has unsupported type %q", name, p.Type)
		}
		if p.Timeout != "" {
			timeout, err := time.ParseDuration(p.Timeout)
			if err != nil || timeout <= 0 {
				return c, fmt.Errorf("provider %q: invalid timeout %q", name, p.Timeout)
			}
		}
	}
	if c.Plugins.Dir != "" && (!filepath.IsAbs(c.Plugins.Dir) || filepath.Clean(c.Plugins.Dir) != c.Plugins.Dir) {
		return c, fmt.Errorf("plugins.dir must be a clean absolute path")
	}
	return c, nil
}

func Check(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("config is a directory")
	}
	_, err = Load(path)
	return err
}

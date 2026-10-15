package provider

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"plugin"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/opsbl/virtualsms/internal/config"
	"github.com/opsbl/virtualsms/pkg/pluginapi"
)

type pluginAdapter struct{ provider pluginapi.Provider }

func (p pluginAdapter) SendBatch(ctx context.Context, b Batch) []error {
	return p.provider.SendBatch(ctx, pluginapi.Batch{To: b.To, Text: b.Text})
}

func discoverPlugins(dir string, configs map[string]config.ProviderConfig) (map[string]string, error) {
	needed := false
	for _, cfg := range configs {
		if cfg.Plugin != "" {
			needed = true
			break
		}
	}
	if !needed {
		return nil, nil
	}
	if dir == "" {
		return nil, fmt.Errorf("plugins.dir is required")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read plugin directory %q: %w", dir, err)
	}
	available := make(map[string]string)
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !strings.HasSuffix(entry.Name(), ".so") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".so")
		if name != "" {
			available[name] = filepath.Join(dir, entry.Name())
		}
	}
	return available, nil
}

func loadPlugin(path, name string, options map[string]any) (Provider, error) {
	var encoded bytes.Buffer
	if err := toml.NewEncoder(&encoded).Encode(options); err != nil {
		return nil, fmt.Errorf("provider %q: encode plugin options: %w", name, err)
	}
	p, err := plugin.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open plugin %q: %w", path, err)
	}
	versionSymbol, err := p.Lookup("APIVersion")
	if err != nil {
		return nil, fmt.Errorf("plugin %q: missing APIVersion: %w", path, err)
	}
	version, ok := versionSymbol.(*int)
	if !ok || *version != pluginapi.Version {
		return nil, fmt.Errorf("plugin %q: incompatible API version", path)
	}
	symbol, err := p.Lookup("New")
	if err != nil {
		return nil, fmt.Errorf("plugin %q: missing New factory: %w", path, err)
	}
	factory, ok := symbol.(func(pluginapi.Config) (pluginapi.Provider, error))
	if !ok {
		return nil, fmt.Errorf("plugin %q: New must be func(pluginapi.Config) (pluginapi.Provider, error)", path)
	}
	instance, err := factory(pluginapi.Config{Name: name, OptionsTOML: encoded.Bytes(), Logger: slog.Default().With("provider", name, "plugin", strings.TrimSuffix(filepath.Base(path), ".so"))})
	if err != nil {
		return nil, fmt.Errorf("plugin %q: %w", path, err)
	}
	if instance == nil {
		return nil, fmt.Errorf("plugin %q: New returned nil", path)
	}
	return pluginAdapter{provider: instance}, nil
}

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

type pluginDefinition struct {
	path    string
	factory pluginapi.Factory
}

// discoverPlugins reads provider IDs from plugin exports; filenames are not identifiers.
func discoverPlugins(dir string, instances map[string]config.InstanceConfig) (map[string]pluginDefinition, error) {
	needed := false
	for _, instance := range instances {
		if instance.Provider != "log" && instance.Provider != "http" {
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
	available := make(map[string]pluginDefinition)
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !strings.HasSuffix(entry.Name(), ".so") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
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
		idSymbol, err := p.Lookup("ProviderID")
		if err != nil {
			return nil, fmt.Errorf("plugin %q: missing ProviderID: %w", path, err)
		}
		id, ok := idSymbol.(*string)
		if !ok || *id == "" || *id == "log" || *id == "http" {
			return nil, fmt.Errorf("plugin %q: invalid or reserved ProviderID", path)
		}
		if previous, exists := available[*id]; exists {
			return nil, fmt.Errorf("duplicate ProviderID %q in %q and %q", *id, previous.path, path)
		}
		symbol, err := p.Lookup("New")
		if err != nil {
			return nil, fmt.Errorf("plugin %q: missing New factory: %w", path, err)
		}
		factory, ok := symbol.(func(pluginapi.Config) (pluginapi.Provider, error))
		if !ok {
			return nil, fmt.Errorf("plugin %q: New must be func(pluginapi.Config) (pluginapi.Provider, error)", path)
		}
		available[*id] = pluginDefinition{path: path, factory: factory}
	}
	return available, nil
}

func newPlugin(def pluginDefinition, id, name string, options map[string]any) (Provider, error) {
	var encoded bytes.Buffer
	if err := toml.NewEncoder(&encoded).Encode(options); err != nil {
		return nil, fmt.Errorf("instance %q: encode options: %w", name, err)
	}
	instance, err := def.factory(pluginapi.Config{Name: name, OptionsTOML: encoded.Bytes(), Logger: slog.Default().With("instance", name, "provider", id)})
	if err != nil {
		return nil, fmt.Errorf("instance %q (%s): %w", name, id, err)
	}
	if instance == nil {
		return nil, fmt.Errorf("instance %q (%s): New returned nil", name, id)
	}
	return pluginAdapter{provider: instance}, nil
}

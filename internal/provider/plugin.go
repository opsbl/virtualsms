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

// PluginInfo describes one shared object found in the plugin directory.
type PluginInfo struct {
	ID         string
	Path       string
	Version    string
	APIVersion int
	Err        error
}

type inspectedPlugin struct {
	info    PluginInfo
	factory pluginapi.Factory
}

// ListPlugins reports every .so, including ones that cannot be loaded.
func ListPlugins(dir string) ([]PluginInfo, error) {
	plugins, err := scanPlugins(dir)
	if err != nil {
		return nil, err
	}
	result := make([]PluginInfo, len(plugins))
	for i, p := range plugins {
		result[i] = p.info
	}
	return result, nil
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
	plugins, err := scanPlugins(dir)
	if err != nil {
		return nil, err
	}
	available := make(map[string]pluginDefinition)
	for _, p := range plugins {
		if p.info.Err != nil {
			return nil, fmt.Errorf("plugin %q: %w", p.info.Path, p.info.Err)
		}
		available[p.info.ID] = pluginDefinition{path: p.info.Path, factory: p.factory}
	}
	return available, nil
}

func scanPlugins(dir string) ([]inspectedPlugin, error) {
	absoluteDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve plugin directory %q: %w", dir, err)
	}
	entries, err := os.ReadDir(absoluteDir)
	if err != nil {
		return nil, fmt.Errorf("read plugin directory %q: %w", absoluteDir, err)
	}
	var result []inspectedPlugin
	ids := make(map[string]string)
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !strings.HasSuffix(entry.Name(), ".so") {
			continue
		}
		path := filepath.Join(absoluteDir, entry.Name())
		p := inspectPlugin(path)
		if p.info.ID != "" {
			if previous, exists := ids[p.info.ID]; exists {
				p.info.Err = fmt.Errorf("duplicate ProviderID %q (also in %q)", p.info.ID, previous)
			} else {
				ids[p.info.ID] = path
			}
		}
		result = append(result, p)
	}
	return result, nil
}

func inspectPlugin(path string) inspectedPlugin {
	p := inspectedPlugin{info: PluginInfo{Path: path}}
	loaded, err := plugin.Open(path)
	if err != nil {
		p.info.Err = fmt.Errorf("open: %w", err)
		return p
	}
	if symbol, err := loaded.Lookup("ProviderID"); err == nil {
		if id, ok := symbol.(*string); ok {
			p.info.ID = *id
		}
	}
	if symbol, err := loaded.Lookup("ProviderVersion"); err == nil {
		if version, ok := symbol.(*string); ok {
			p.info.Version = *version
		}
	}
	versionSymbol, err := loaded.Lookup("APIVersion")
	if err != nil {
		p.info.Err = fmt.Errorf("missing APIVersion: %w", err)
		return p
	}
	version, ok := versionSymbol.(*int)
	if !ok {
		p.info.Err = fmt.Errorf("APIVersion must be an int")
		return p
	}
	p.info.APIVersion = *version
	if *version != pluginapi.Version {
		p.info.Err = fmt.Errorf("incompatible API version %d (expected %d)", *version, pluginapi.Version)
		return p
	}
	if p.info.ID == "" || p.info.ID == "log" || p.info.ID == "http" {
		p.info.Err = fmt.Errorf("invalid or reserved ProviderID")
		return p
	}
	symbol, err := loaded.Lookup("New")
	if err != nil {
		p.info.Err = fmt.Errorf("missing New factory: %w", err)
		return p
	}
	factory, ok := symbol.(func(pluginapi.Config) (pluginapi.Provider, error))
	if !ok {
		p.info.Err = fmt.Errorf("New must be func(pluginapi.Config) (pluginapi.Provider, error)")
		return p
	}
	p.factory = factory
	return p
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

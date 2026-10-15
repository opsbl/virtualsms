package main

import (
	"log/slog"
	"net/http"

	"github.com/opsbl/virtualsms/pkg/pluginapi"
)

var APIVersion = pluginapi.Version

// New decodes AbleMobile options and creates one provider instance.
func New(cfg pluginapi.Config) (pluginapi.Provider, error) {
	options, err := decodeOptions(cfg.OptionsTOML)
	if err != nil {
		return nil, err
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default().With("provider", cfg.Name, "plugin", "ablemobile")
	}
	logger.Info("AbleMobile provider configured", "endpoint", options.URL)
	return &ableMobileProvider{options: options, client: &http.Client{Timeout: options.timeout}, logger: logger}, nil
}

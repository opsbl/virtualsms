// Package pluginapi defines the interface shared by VirtualSMS and Go provider plugins.
package pluginapi

import (
	"context"
	"log/slog"
)

const Version = 1

type Batch struct {
	To   []string
	Text string
}

type Provider interface {
	// Results must have the same length and order as Batch.To.
	SendBatch(context.Context, Batch) []error
}

type Config struct {
	Name        string
	OptionsTOML []byte
	Logger      *slog.Logger
}

// Factory is the signature of the exported New symbol in a provider plugin.
type Factory func(Config) (Provider, error)

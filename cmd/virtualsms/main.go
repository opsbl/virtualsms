package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/opsbl/virtualsms/internal/atclient"
	"github.com/opsbl/virtualsms/internal/config"
	"github.com/opsbl/virtualsms/internal/provider"
	"github.com/opsbl/virtualsms/internal/service"
	"github.com/spf13/cobra"
)

func main() {
	if err := command().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func command() *cobra.Command {
	var path string
	root := &cobra.Command{Use: "virtualsms", Short: "Virtual serial SMS modems for Zabbix and other AT clients", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().StringVarP(&path, "config", "c", "/etc/virtualsms/config.toml", "TOML configuration path")
	root.AddCommand(&cobra.Command{Use: "check", Short: "Validate configuration and load plugins", RunE: func(cmd *cobra.Command, _ []string) error {
		c, err := config.Load(path)
		if err != nil {
			return err
		}
		if _, err := provider.NewWithPlugins(c.Instances, c.Plugins.Dir); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "configuration OK")
		return nil
	}})
	root.AddCommand(&cobra.Command{Use: "serve", Short: "Start virtual modems and metrics endpoint", RunE: func(cmd *cobra.Command, _ []string) error {
		c, err := config.Load(path)
		if err != nil {
			return err
		}
		return service.Run(cmd.Context(), c)
	}})
	var device, to, message string
	var direct bool
	var timeout time.Duration
	test := &cobra.Command{Use: "test", Short: "Send a test SMS through a virtual serial device", RunE: func(cmd *cobra.Command, _ []string) error {
		if !provider.ValidPhone(to) || message == "" {
			return fmt.Errorf("--to must be a phone number and --message must not be empty")
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
		defer cancel()
		if !direct {
			if err := atclient.Send(ctx, device, to, message); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "SMS accepted by virtual modem")
			return nil
		}
		c, err := config.Load(path)
		if err != nil {
			return err
		}
		instances, err := provider.NewWithPlugins(c.Instances, c.Plugins.Dir)
		if err != nil {
			return err
		}
		for _, d := range c.Devices {
			if d.Path == device {
				if err := instances[d.Instance].SendBatch(ctx, provider.Batch{To: []string{to}, Text: message})[0]; err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), "SMS accepted by provider")
				return nil
			}
		}
		return fmt.Errorf("device %q not configured", device)
	}}
	test.Flags().StringVarP(&device, "device", "d", "", "configured device path")
	test.Flags().StringVarP(&to, "to", "t", "", "recipient number")
	test.Flags().StringVarP(&message, "message", "m", "", "SMS text")
	test.Flags().BoolVarP(&direct, "direct", "D", false, "call the configured provider without a running modem")
	test.Flags().DurationVarP(&timeout, "timeout", "T", 45*time.Second, "test timeout")
	test.MarkFlagRequired("device")
	test.MarkFlagRequired("to")
	test.MarkFlagRequired("message")
	root.AddCommand(test)
	return root
}

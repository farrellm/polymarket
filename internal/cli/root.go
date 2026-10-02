// Package cli builds the polymarket command line.
package cli

import (
	"context"

	"github.com/charmbracelet/fang"
	"github.com/spf13/cobra"
)

// version is overridden at build time with -ldflags.
var version = "dev"

// Execute runs the command line.
func Execute(ctx context.Context) error {
	return fang.Execute(ctx, newCommand(), fang.WithVersion(version))
}

// newCommand builds the root command.
func newCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "polymarket",
		Short: "browse Polymarket market data and export it to CSV",
		Long: "polymarket is a read-only terminal browser for Polymarket.\n\n" +
			"It drills down from tags to events to markets, shows one market in\n" +
			"detail, and exports what it shows to CSV. It needs no credentials.",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// There is no browser yet, so there is nothing to open.
			return cmd.Help()
		},
	}
}

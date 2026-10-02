// Package cli builds the polymarket command line.
package cli

import (
	"context"
	"os"
	"syscall"
	"time"

	"github.com/charmbracelet/fang"
	"github.com/spf13/cobra"

	"github.com/farrellm/polymarket/internal/api"
)

// version is overridden at build time with -ldflags.
var version = "dev"

// options are the flags every command shares.
type options struct {
	timeout time.Duration

	// Base URL overrides, for pointing the commands at a test server.
	gammaURL string
	clobURL  string
	dataURL  string
}

// client builds the API client the flags describe.
func (o *options) client() *api.Client {
	return api.New(api.Options{
		GammaURL:  o.gammaURL,
		CLOBURL:   o.clobURL,
		DataURL:   o.dataURL,
		Timeout:   o.timeout,
		UserAgent: "polymarket-tui/" + version,
	})
}

// Execute runs the command line.
func Execute(ctx context.Context) error {
	var o options
	return fang.Execute(ctx, newCommand(&o),
		fang.WithVersion(version),
		// An interrupt cancels the context rather than killing the process,
		// so an export under way gets to remove its temporary file.
		fang.WithNotifySignal(os.Interrupt, syscall.SIGTERM),
	)
}

// newCommand builds the root command, writing parsed flags into o.
func newCommand(o *options) *cobra.Command {
	cmd := &cobra.Command{
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

	pf := cmd.PersistentFlags()
	pf.DurationVar(&o.timeout, "timeout", 15*time.Second, "give up on a request after this long")
	pf.StringVar(&o.gammaURL, "gamma-url", "", "base URL of the Gamma service")
	pf.StringVar(&o.clobURL, "clob-url", "", "base URL of the CLOB service")
	pf.StringVar(&o.dataURL, "data-url", "", "base URL of the Data API")
	for _, name := range []string{"gamma-url", "clob-url", "data-url"} {
		if err := pf.MarkHidden(name); err != nil {
			panic(err)
		}
	}

	cmd.AddCommand(newExportCommand(o))
	return cmd
}

// registerFlagCompletion offers a fixed set of values for a flag. It panics on
// a misspelled flag name, which is a programming error, not a runtime one.
func registerFlagCompletion(cmd *cobra.Command, flag string, choices []string) {
	err := cmd.RegisterFlagCompletionFunc(flag,
		cobra.FixedCompletions(choices, cobra.ShellCompDirectiveNoFileComp))
	if err != nil {
		panic(err)
	}
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

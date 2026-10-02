// Package cli builds the polymarket command line.
package cli

import (
	"context"
	"errors"
	"os"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/fang"
	"github.com/spf13/cobra"

	"github.com/farrellm/polymarket/internal/api"
	"github.com/farrellm/polymarket/internal/ui"
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
	var lf listFlags
	cmd := &cobra.Command{
		Use:   "polymarket",
		Short: "browse Polymarket market data and export it to CSV",
		Long: "polymarket is a read-only terminal browser for Polymarket.\n\n" +
			"It drills down from tags to events to markets, shows one market in\n" +
			"detail, and exports what it shows to CSV. It needs no credentials.\n\n" +
			"With --tag it opens inside that tag, and the other flags set what its\n" +
			"lists start out sorted and filtered by; esc still leads up to the tags.",
		Example: "polymarket\n" +
			"polymarket --tag politics --min-volume 10000 --order endDate",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runBrowser(cmd, o, &lf)
		},
	}
	bindFilterFlags(cmd, &lf)

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

// runBrowser opens the browser, on the Tags level or inside the tag the
// flags name, and runs it until the user leaves.
func runBrowser(cmd *cobra.Command, o *options, lf *listFlags) error {
	f, err := lf.filter()
	if err != nil {
		return err
	}
	in, out := cmd.InOrStdin(), cmd.OutOrStdout()
	if !isTerminalStream(in) || !isTerminalStream(out) {
		// Drawing into a pipe helps nobody, and neither does waiting for
		// keys from one.
		return errors.New("the browser needs a terminal; polymarket export writes CSV without one")
	}

	client := o.client()
	opts := ui.Options{Filter: f.Filter}
	if f.tagSlug != "" {
		// A misspelt tag is an error here, before the screen is taken over,
		// rather than a list with nothing in it.
		if opts.Tag, err = findTag(cmd.Context(), client, f); err != nil {
			return err
		}
	}

	// The context is cancelled by a signal from outside; ctrl+c at the
	// keyboard is a key like any other, which the browser quits on.
	p := tea.NewProgram(ui.New(client, opts),
		tea.WithContext(cmd.Context()), tea.WithInput(in), tea.WithOutput(out))
	_, err = p.Run()
	return err
}

// isTerminalStream reports whether one of a command's streams is a terminal.
// A stream a test has swapped for a buffer is not.
func isTerminalStream(stream any) bool {
	f, ok := stream.(*os.File)
	return ok && isTerminal(f)
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

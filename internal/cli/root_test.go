package cli

import (
	"bytes"
	"strings"
	"testing"
)

// The browser must not start drawing into a pipe: a script that forgot the
// export subcommand gets told so instead.
func TestRootNeedsATerminal(t *testing.T) {
	cmd := newCommand(&options{})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(nil)
	err := cmd.Execute()
	if err == nil {
		t.Fatal("the browser started without a terminal, want an error")
	}
	for _, want := range []string{"needs a terminal", "polymarket export"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestRootHelp(t *testing.T) {
	cmd := newCommand(&options{})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); !strings.Contains(got, "read-only terminal browser") {
		t.Errorf("output %q does not look like the help text", got)
	}
}

func TestRootTakesNoArguments(t *testing.T) {
	cmd := newCommand(&options{})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"stray"})
	if err := cmd.Execute(); err == nil {
		t.Error("a positional argument was accepted, want an error")
	}
}

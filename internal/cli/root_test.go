package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootPrintsHelp(t *testing.T) {
	cmd := newCommand(&options{})
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(nil)
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

package ui

import (
	"os/exec"
	"runtime"
)

// openInBrowser hands a URL to whatever the system opens one with. It does
// not wait for that to finish: a browser may stay up for hours.
func openInBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	// Reaped when it exits, whenever that is.
	go func() { _ = cmd.Wait() }()
	return nil
}

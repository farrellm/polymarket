// Command polymarket browses Polymarket market data in the terminal and
// exports it to CSV.
package main

import (
	"context"
	"os"

	"github.com/farrellm/polymarket/internal/cli"
)

func main() {
	if err := cli.Execute(context.Background()); err != nil {
		// fang has already reported the error.
		os.Exit(1)
	}
}

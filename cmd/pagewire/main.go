// Command pagewire is a single-binary SDR receiver, decoder and
// delivery gateway for pager and emergency-service radio protocols.
package main

import (
	"os"

	"github.com/gopager/pagewire/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:], os.Stdout, os.Stderr))
}

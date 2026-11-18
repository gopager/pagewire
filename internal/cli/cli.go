// Package cli defines the command line interface of pagewire.
package cli

import (
	"fmt"
	"io"

	"github.com/alecthomas/kong"

	"github.com/gopager/pagewire/internal/buildinfo"
)

// CLI is the root command. Global flags are shared by all sub-commands.
type CLI struct {
	Config string `help:"Path to the YAML configuration file." short:"c" default:"pagewire.yaml" env:"PAGEWIRE_CONFIG" type:"path"`

	Run       RunCmd       `cmd:"" help:"Run the gateway."`
	Record    RecordCmd    `cmd:"" help:"Record raw IQ and channel audio to files."`
	Calibrate CalibrateCmd `cmd:"" help:"Measure ppm and gain for a source."`
	Decode    DecodeCmd    `cmd:"" help:"Decode a recorded IQ or audio file offline."`
	Version   VersionCmd   `cmd:"" help:"Print version information."`
	Manpage   ManpageCmd   `cmd:"" hidden:"" help:"Write the manual page in roff format to stdout."`
	Devices   DevicesCmd   `cmd:"" help:"List attached SDR devices."`
	Cfg       ConfigCmd    `cmd:"" name:"config" help:"Validate or print configuration."`
	Queue     QueueCmd     `cmd:"" help:"Inspect or repair the delivery queue."`
}

// Context carries dependencies into command Run methods.
type Context struct {
	Stdout io.Writer
	Stderr io.Writer
}

// Main parses args and runs the selected command, returning the exit code.
func Main(args []string, stdout, stderr io.Writer) int {
	var cli CLI
	parser, err := kong.New(&cli,
		kong.Name("pagewire"),
		kong.Description("Receives pager and emergency-service radio protocols with an SDR and delivers decoded messages to one or more servers."),
		kong.UsageOnError(),
		kong.Writers(stdout, stderr),
		kong.Vars{"version": buildinfo.Version},
	)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	kctx, err := parser.Parse(args)
	if err != nil {
		parser.FatalIfErrorf(err)
		return 2
	}
	if err := kctx.Run(&Context{Stdout: stdout, Stderr: stderr}, &cli, kctx); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

// VersionCmd prints the build version.
type VersionCmd struct{}

// Run implements the command.
func (VersionCmd) Run(ctx *Context) error {
	_, err := fmt.Fprintf(ctx.Stdout, "pagewire %s (%s)\n", buildinfo.Version, buildinfo.Commit())
	return err
}

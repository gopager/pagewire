package cli

import (
	"fmt"

	"github.com/gopager/pagewire/internal/config"
)

// ConfigCmd groups configuration helpers.
type ConfigCmd struct {
	Validate ConfigValidateCmd `cmd:"" help:"Load the configuration and report problems."`
	Example  ConfigExampleCmd  `cmd:"" help:"Print a documented example configuration."`
}

// ConfigValidateCmd validates the configuration file.
type ConfigValidateCmd struct{}

// Run implements the command.
func (ConfigValidateCmd) Run(ctx *Context, root *CLI) error {
	cfg, err := config.Load(root.Config)
	if err != nil {
		return err
	}
	fmt.Fprintf(ctx.Stdout, "%s: OK (instance %s, %d source(s), %d channel(s), %d target(s))\n", root.Config, cfg.Instance, len(cfg.Sources), len(cfg.Channels), len(cfg.Targets))
	return nil
}

// ConfigExampleCmd prints the example configuration.
type ConfigExampleCmd struct{}

// Run implements the command.
func (ConfigExampleCmd) Run(ctx *Context) error {
	_, err := fmt.Fprint(ctx.Stdout, config.Example)
	return err
}

package cli

import (
	"fmt"

	"github.com/alecthomas/kong"
	mangokong "github.com/alecthomas/mango-kong"
	"github.com/muesli/roff"

	"github.com/gopager/pagewire/internal/buildinfo"
)

// ManpageCmd writes the manual page (roff) to stdout; the release workflow
// ships it as pagewire.1.
type ManpageCmd struct{}

// Run implements the command.
func (ManpageCmd) Run(ctx *Context, kctx *kong.Context) error {
	man := mangokong.NewManPage(1, kctx.Model)
	man = man.WithSection("Files", "/etc/pagewire/<instance>.yaml\n  Configuration, see \"pagewire config example\".\n\n/var/lib/pagewire/\n  Persistent delivery queues.")
	man = man.WithSection("See Also", "Documentation in the docs/ directory of the source distribution; Prometheus metrics on /metrics of the configured listen address.")
	man = man.WithSection("Version", buildinfo.Version)
	_, err := fmt.Fprint(ctx.Stdout, man.Build(roff.NewDocument()))
	return err
}

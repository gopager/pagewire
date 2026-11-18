package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/gopager/pagewire/internal/config"
	"github.com/gopager/pagewire/internal/queue"
)

// QueueCmd inspects and repairs the persistent delivery queue. The gateway
// must not be running while these commands use the same queue file.
type QueueCmd struct {
	Inspect QueueInspectCmd `cmd:"" help:"Show queue depths and the oldest entries per target."`
	Replay  QueueReplayCmd  `cmd:"" help:"Move dead-letter entries back into the queue."`
	Purge   QueuePurgeCmd   `cmd:"" help:"Delete pending or dead-letter entries of a target."`
}

func openQueue(root *CLI) (*config.Config, *queue.Queue, error) {
	cfg, err := config.Load(root.Config)
	if err != nil {
		return nil, nil, err
	}
	q, err := queue.Open(cfg.Queue.Path, cfg.Queue.MaxPerTarget)
	if err != nil {
		return nil, nil, fmt.Errorf("%w (is the gateway running?)", err)
	}
	return cfg, q, nil
}

// QueueInspectCmd lists depths and entries.
type QueueInspectCmd struct {
	Limit int  `help:"Entries to show per target." default:"5"`
	Dead  bool `help:"Show dead-letter entries instead of pending ones."`
}

// Run implements the command.
func (c QueueInspectCmd) Run(ctx *Context, root *CLI) error {
	cfg, q, err := openQueue(root)
	if err != nil {
		return err
	}
	defer q.Close()
	tw := tabwriter.NewWriter(ctx.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "TARGET\tPENDING\tDEAD\tDROPPED")
	for _, t := range cfg.Targets {
		p, d := q.Depth(t.Name)
		fmt.Fprintf(tw, "%s\t%d\t%d\t%d\n", t.Name, p, d, q.Dropped(t.Name))
	}
	tw.Flush()
	for _, t := range cfg.Targets {
		entries, err := q.List(t.Name, c.Dead, c.Limit)
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			continue
		}
		fmt.Fprintf(ctx.Stdout, "\n%s:\n", t.Name)
		for _, e := range entries {
			fmt.Fprintf(ctx.Stdout, "  #%d attempts=%d replays=%d %s %s addr=%s %s: %q", e.Seq, e.Attempts, e.Replays, e.Message.ReceivedAt.Format("2006-01-02 15:04:05"), e.Message.Protocol, e.Message.Address, e.Message.Kind, e.Message.Text)
			if e.LastError != "" {
				fmt.Fprintf(ctx.Stdout, "  [%s: %s]", e.Class, e.LastError)
			}
			fmt.Fprintln(ctx.Stdout)
		}
	}
	return nil
}

// QueueReplayCmd re-queues dead letters.
type QueueReplayCmd struct {
	Target string `arg:"" help:"Target name."`
}

// Run implements the command.
func (c QueueReplayCmd) Run(ctx *Context, root *CLI) error {
	_, q, err := openQueue(root)
	if err != nil {
		return err
	}
	defer q.Close()
	n, err := q.Replay(c.Target, "", 0)
	if err != nil {
		return err
	}
	fmt.Fprintf(ctx.Stdout, "%d entries re-queued for %s\n", n, c.Target)
	return nil
}

// QueuePurgeCmd deletes entries.
type QueuePurgeCmd struct {
	Target string `arg:"" help:"Target name."`
	Dead   bool   `help:"Purge dead letters instead of pending entries."`
	Yes    bool   `help:"Confirm the deletion." required:""`
}

// Run implements the command.
func (c QueuePurgeCmd) Run(ctx *Context, root *CLI) error {
	_, q, err := openQueue(root)
	if err != nil {
		return err
	}
	defer q.Close()
	n, err := q.Purge(c.Target, c.Dead)
	if err != nil {
		return err
	}
	fmt.Fprintf(ctx.Stdout, "%d entries deleted for %s\n", n, c.Target)
	return nil
}

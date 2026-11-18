package cli

import (
	"errors"
	"fmt"
	"text/tabwriter"

	"github.com/gopager/pagewire/internal/config"
	"github.com/gopager/pagewire/internal/sdr"
	"github.com/gopager/pagewire/internal/sdr/rtlsdr"
)

// DevicesCmd lists SDR devices visible to the compiled-in drivers.
type DevicesCmd struct {
	Probe bool `help:"Open each device to read tuner type and gain steps."`
}

// Run implements the command.
func (c DevicesCmd) Run(ctx *Context) error {
	infos, err := rtlsdr.Enumerate()
	if errors.Is(err, sdr.ErrUnsupported) {
		fmt.Fprintln(ctx.Stderr, "rtlsdr driver is not available in this build")
		return nil
	}
	if err != nil {
		return err
	}
	if len(infos) == 0 {
		fmt.Fprintln(ctx.Stdout, "no RTL-SDR devices found")
		return nil
	}
	serials := map[string]int{}
	for _, info := range infos {
		serials[info.Serial]++
	}
	tw := tabwriter.NewWriter(ctx.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "SELECTOR (USB PORT)\tSELECTOR (SERIAL)\tINDEX\tNAME\tMANUFACTURER\tPRODUCT\tTUNER\tGAINS (dB)")
	for _, info := range infos {
		tuner, gains := "", ""
		if c.Probe {
			tuner, gains = probe(info.Index)
		}
		fmt.Fprintf(tw, "usb:%s\tserial:%s\t%d\t%s\t%s\t%s\t%s\t%s\n", config.USBPath(info.Bus, info.Ports), info.Serial, info.Index, info.Name, info.Manufacturer, info.Product, tuner, gains)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	for serial, n := range serials {
		if n > 1 {
			fmt.Fprintf(ctx.Stdout, "\nnote: %d devices share serial %q; select them by usb: port path or program unique serials\n", n, serial)
		}
	}
	return nil
}

func probe(index int) (tuner, gains string) {
	dev, err := rtlsdr.Open(index)
	if err != nil {
		return "error: " + err.Error(), ""
	}
	defer dev.Close()
	for i, g := range dev.Gains() {
		if i > 0 {
			gains += " "
		}
		gains += fmt.Sprintf("%.1f", float64(g)/10)
	}
	return dev.Info().Tuner, gains
}

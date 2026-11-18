package config

import (
	"fmt"
	"strconv"
	"strings"
)

// SelectorKind says how an rtlsdr device selector identifies a dongle.
type SelectorKind int

// Selector kinds.
const (
	// SelectAny matches the only attached device.
	SelectAny SelectorKind = iota
	// SelectSerial matches the USB serial string (stable if the EEPROM
	// serial was made unique).
	SelectSerial
	// SelectUSBPath matches bus number and port chain, i.e. the physical
	// socket the dongle is plugged into.
	SelectUSBPath
	// SelectIndex matches the enumeration index; unstable across boots.
	SelectIndex
)

// DeviceSelector is a parsed rtlsdr device selector.
type DeviceSelector struct {
	Kind   SelectorKind
	Serial string
	Bus    int
	Ports  []int
	Index  int
}

// ParseDeviceSelector parses "", "serial:<s>", "usb:<bus>-<p.p…>" or
// "index:<n>".
func ParseDeviceSelector(s string) (DeviceSelector, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return DeviceSelector{Kind: SelectAny}, nil
	}
	kind, val, ok := strings.Cut(s, ":")
	if !ok {
		return DeviceSelector{}, fmt.Errorf("device %q must be serial:<serial>, usb:<bus>-<ports> or index:<n>", s)
	}
	switch kind {
	case "serial":
		if val == "" {
			return DeviceSelector{}, fmt.Errorf("device %q: empty serial", s)
		}
		return DeviceSelector{Kind: SelectSerial, Serial: val}, nil
	case "usb":
		bus, ports, ok := strings.Cut(val, "-")
		if !ok {
			return DeviceSelector{}, fmt.Errorf("device %q: expected usb:<bus>-<port[.port…]>", s)
		}
		sel := DeviceSelector{Kind: SelectUSBPath}
		var err error
		if sel.Bus, err = strconv.Atoi(bus); err != nil {
			return DeviceSelector{}, fmt.Errorf("device %q: bad bus number", s)
		}
		for _, p := range strings.Split(ports, ".") {
			n, err := strconv.Atoi(p)
			if err != nil {
				return DeviceSelector{}, fmt.Errorf("device %q: bad port number %q", s, p)
			}
			sel.Ports = append(sel.Ports, n)
		}
		return sel, nil
	case "index":
		n, err := strconv.Atoi(val)
		if err != nil || n < 0 {
			return DeviceSelector{}, fmt.Errorf("device %q: bad index", s)
		}
		return DeviceSelector{Kind: SelectIndex, Index: n}, nil
	default:
		return DeviceSelector{}, fmt.Errorf("device %q: unknown selector kind %q", s, kind)
	}
}

// USBPath formats a bus number and port chain as used in usb: selectors,
// e.g. "1-1.3".
func USBPath(bus int, ports []int) string {
	var b strings.Builder
	b.WriteString(strconv.Itoa(bus))
	b.WriteByte('-')
	for i, p := range ports {
		if i > 0 {
			b.WriteByte('.')
		}
		b.WriteString(strconv.Itoa(p))
	}
	return b.String()
}

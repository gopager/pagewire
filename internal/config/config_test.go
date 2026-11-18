package config

import (
	"os"
	"strings"
	"testing"
)

func TestExampleParses(t *testing.T) {
	cfg, err := Parse([]byte(Example))
	if err != nil {
		t.Fatalf("example config must be valid: %v", err)
	}
	if got := cfg.Channels[0].Frequency; got != 150000000 {
		t.Errorf("frequency = %d, want 150000000", got)
	}
	if !cfg.Sources[0].RTLSDR.Gain.Auto {
		t.Errorf("gain should be auto")
	}
	if cfg.Channels[0].Source != "vhf" {
		t.Errorf("channel source = %q", cfg.Channels[0].Source)
	}
	if cfg.Channels[1].Decoders[0].FLEX == nil || !cfg.Channels[1].Decoders[0].FLEX.ExpandGroupsEnabled() {
		t.Errorf("flex defaults not applied: %+v", cfg.Channels[1].Decoders[0].FLEX)
	}
	if cfg.Targets[0].Pagermon.Timeout.Seconds() != 10 {
		t.Errorf("timeout = %v", cfg.Targets[0].Pagermon.Timeout)
	}
}

func TestParseFrequency(t *testing.T) {
	cases := map[string]Frequency{
		"150.000M":    150000000,
		"150000000":   150000000,
		"460.000 MHz": 460000000,
		"1.2G":        1200000000,
		"144k":        144000,
	}
	for in, want := range cases {
		got, err := ParseFrequency(in)
		if err != nil || got != want {
			t.Errorf("ParseFrequency(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "abc", "-5M", "0"} {
		if _, err := ParseFrequency(bad); err == nil {
			t.Errorf("ParseFrequency(%q) should fail", bad)
		}
	}
}

func TestEnvOverrideAndExpansion(t *testing.T) {
	t.Setenv("PAGEWIRE__INSTANCE", "env-instance")
	t.Setenv("PM_KEY", "secret-from-env")
	yaml := strings.Replace(Example, "api_key_file: /run/secrets/pagermon_api_key", "api_key: ${PM_KEY}", 1)
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Instance != "env-instance" {
		t.Errorf("instance = %q, want value from environment", cfg.Instance)
	}
	if cfg.Targets[0].Pagermon.APIKey != "secret-from-env" {
		t.Errorf("api_key = %q", cfg.Targets[0].Pagermon.APIKey)
	}
}

func TestEnvOverrideOfListElement(t *testing.T) {
	t.Setenv("PAGEWIRE__SOURCES__0__RTLSDR__PPM", "12")
	cfg, err := Parse([]byte(Example))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Sources[0].RTLSDR.PPM != 12 {
		t.Errorf("ppm = %v, want value from environment", cfg.Sources[0].RTLSDR.PPM)
	}
}

func TestUnknownKeysAreRejected(t *testing.T) {
	for name, yaml := range map[string]string{
		"top level": Example + "\nfilters: { kinds: [alpha] }\n",
		"nested":    strings.Replace(Example, "  pprof: false", "  pprof: false\n  lsten: x", 1),
		"in a list": strings.Replace(Example, "      ppm: 0 ", "      ppn: 0 ", 1),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Parse([]byte(yaml))
			if err == nil || !strings.Contains(err.Error(), "invalid keys") {
				t.Fatalf("expected an invalid-keys error, got %v", err)
			}
		})
	}
	t.Run("environment", func(t *testing.T) {
		t.Setenv("PAGEWIRE__INSTANCEE", "x")
		_, err := Parse([]byte(Example))
		if err == nil || !strings.Contains(err.Error(), "invalid keys") {
			t.Fatalf("expected an invalid-keys error, got %v", err)
		}
	})
	t.Run("environment index out of range", func(t *testing.T) {
		t.Setenv("PAGEWIRE__SOURCES__5__RTLSDR__PPM", "1")
		if _, err := Parse([]byte(Example)); err == nil {
			t.Fatal("expected an error for a list index without element")
		}
	})
}

func TestValidationErrors(t *testing.T) {
	bad := `
sources:
  - { name: s, type: nope }
  - { name: s, type: rtlsdr, rtlsdr: { device: "bogus" } }
channels:
  - { name: a, source: s, frequency: 150.000M, decoders: [ { protocol: pocsag1200, pocsag: { charset: XX } } ] }
  - { name: a, source: missing, decoders: [] }
targets:
  - { name: t, type: pagermon, pagermon: { url: "ftp://x" } }
observability: { log: { level: loud } }
`
	_, err := Parse([]byte(bad))
	if err == nil {
		t.Fatal("expected validation errors")
	}
	for _, want := range []string{"sources[0].type", "sources[1].name", "rtlsdr.device", "does not name", "charset", "not unique", "at least one decoder", "url", "api_key", "log.level"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q:\n%v", want, err)
		}
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load("/nonexistent/pagewire.yaml"); err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveSecret(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "key")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("  k3y \n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveSecret("", f.Name())
	if err != nil || got != "k3y" {
		t.Errorf("ResolveSecret = %q, %v", got, err)
	}
	if got, _ := ResolveSecret("inline", f.Name()); got != "inline" {
		t.Errorf("inline value must win, got %q", got)
	}
}

func TestSingleSourceDefaults(t *testing.T) {
	yaml := `
sources: [ { type: rtlsdr } ]
channels: [ { name: a, frequency: 150.000M, decoders: [ { protocol: pocsag1200 } ] } ]
targets: [ { name: t, type: pagermon, pagermon: { url: "http://x", api_key: k } } ]
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Sources[0].Name != "default" || cfg.Channels[0].Source != "default" {
		t.Errorf("single source should be named default and bound: %+v / %+v", cfg.Sources[0].Name, cfg.Channels[0].Source)
	}
	if cfg.Queue.Path != "pagewire-default.db" {
		t.Errorf("queue path = %q", cfg.Queue.Path)
	}
	if cfg.Targets[0].Pagermon.Source != "{channel}" {
		t.Errorf("pagermon source default = %q", cfg.Targets[0].Pagermon.Source)
	}
}

func TestParseDeviceSelector(t *testing.T) {
	sel, err := ParseDeviceSelector("usb:1-1.3")
	if err != nil || sel.Kind != SelectUSBPath || sel.Bus != 1 || len(sel.Ports) != 2 || sel.Ports[1] != 3 {
		t.Errorf("usb selector = %+v, %v", sel, err)
	}
	if USBPath(sel.Bus, sel.Ports) != "1-1.3" {
		t.Errorf("USBPath round trip failed")
	}
	if sel, _ := ParseDeviceSelector("serial:00000002"); sel.Kind != SelectSerial || sel.Serial != "00000002" {
		t.Errorf("serial selector = %+v", sel)
	}
	if sel, _ := ParseDeviceSelector(""); sel.Kind != SelectAny {
		t.Errorf("empty selector should be any")
	}
	for _, bad := range []string{"0", "usb:x", "usb:1-a", "index:-1", "foo:bar"} {
		if _, err := ParseDeviceSelector(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

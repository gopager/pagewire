package config

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/rawbytes"
	"github.com/knadh/koanf/v2"
)

// EnvPrefix is the prefix of environment variables that override scalar
// configuration values. Path elements are separated by "__" and numeric
// elements address list entries, e.g. PAGEWIRE__SOURCES__0__RTLSDR__PPM=12.
const EnvPrefix = "PAGEWIRE__"

// Default returns the configuration with all defaults applied and no
// channels or targets.
func Default() Config {
	return Config{
		Instance: "default",
		Queue: QueueConfig{
			Path:         "",
			MaxPerTarget: 100000,
			MaxAttempts:  10,
			MaxReplays:   3,
			MaxBackoff:   5 * time.Minute,
			DrainTimeout: 30 * time.Second,
		},
		Observability: ObservabilityConfig{
			Listen:  ":9464",
			Metrics: true,
			Log:     LogConfig{Level: "info", Format: "text"},
		},
	}
}

// Load reads the YAML file at path, expands ${VAR} references, applies
// environment overrides, fills defaults and validates the result.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	return Parse(raw)
}

var envRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-([^}]*))?\}`)

// expandEnv replaces ${VAR} and ${VAR:-default} in the YAML text.
func expandEnv(b []byte) []byte {
	return envRef.ReplaceAllFunc(b, func(m []byte) []byte {
		sub := envRef.FindSubmatch(m)
		if v, ok := os.LookupEnv(string(sub[1])); ok {
			return []byte(v)
		}
		return sub[2]
	})
}

// Parse behaves like Load for configuration already held in memory.
func Parse(raw []byte) (*Config, error) {
	k := koanf.New(".")
	if err := k.Load(rawbytes.Provider(expandEnv(raw)), yaml.Parser()); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	tree := k.Raw()
	if err := applyEnv(tree, os.Environ()); err != nil {
		return nil, fmt.Errorf("apply environment: %w", err)
	}
	cfg := Default()
	if err := decode(tree, &cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	applyDefaults(&cfg)
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// applyEnv overrides values of the parsed configuration tree with the
// EnvPrefix variables of environ (KEY=VALUE entries as from os.Environ). The
// tree is edited in place so that an override of one field of a list element
// keeps the rest of the list, which a map merge would replace.
func applyEnv(tree map[string]any, environ []string) error {
	for _, kv := range environ {
		name, value, ok := strings.Cut(kv, "=")
		if !ok || !strings.HasPrefix(name, EnvPrefix) {
			continue
		}
		path := strings.Split(strings.ToLower(strings.TrimPrefix(name, EnvPrefix)), "__")
		if err := setPath(tree, path, value); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

// setPath stores value at path below node, a tree of maps and lists as the
// YAML parser produces it. Missing map levels are created; list indexes must
// address an existing element. It returns an error when the path runs into a
// scalar or a list index is invalid.
func setPath(node any, path []string, value string) error {
	for i, key := range path {
		last := i == len(path)-1
		switch n := node.(type) {
		case map[string]any:
			if last {
				n[key] = value
				return nil
			}
			child, ok := n[key]
			if !ok || child == nil {
				child = map[string]any{}
				n[key] = child
			}
			node = child
		case []any:
			idx, err := strconv.Atoi(key)
			if err != nil || idx < 0 || idx >= len(n) {
				return fmt.Errorf("%q does not address one of the %d list elements", key, len(n))
			}
			if last {
				n[idx] = value
				return nil
			}
			node = n[idx]
		default:
			return fmt.Errorf("%q is a value, not a section", strings.Join(path[:i], "."))
		}
	}
	return nil
}

// decode fills cfg from the configuration tree. Unknown keys are errors, so
// typos and options that no longer exist fail instead of being ignored.
func decode(tree map[string]any, cfg *Config) error {
	d, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		TagName:          "koanf",
		Result:           cfg,
		WeaklyTypedInput: true,
		ErrorUnused:      true,
		DecodeHook: mapstructure.ComposeDecodeHookFunc(
			mapstructure.StringToTimeDurationHookFunc(),
			mapstructure.TextUnmarshallerHookFunc()),
	})
	if err != nil {
		return err
	}
	return d.Decode(tree)
}

// DefaultSource returns a source with all defaults applied.
func DefaultSource() SourceConfig {
	return SourceConfig{
		Type:   SourceRTLSDR,
		RTLSDR: RTLSDRConfig{Gain: Gain{Auto: true}},
		RTLTCP: RTLTCPConfig{Address: "127.0.0.1:1234", Gain: Gain{Auto: true}},
		Audio:  AudioConfig{Backend: AudioStdin, Format: "s16le", Rate: 22050},
	}
}

// applyDefaults fills per-item defaults that cannot be expressed in Default.
func applyDefaults(cfg *Config) {
	if cfg.Queue.Path == "" {
		cfg.Queue.Path = "pagewire-" + cfg.Instance + ".db"
	}
	for i := range cfg.Sources {
		src := &cfg.Sources[i]
		def := DefaultSource()
		if src.Type == "" {
			src.Type = def.Type
		}
		if src.Name == "" && len(cfg.Sources) == 1 {
			src.Name = "default"
		}
		if src.RTLTCP.Address == "" {
			src.RTLTCP.Address = def.RTLTCP.Address
		}
		if src.Audio.Backend == "" {
			src.Audio.Backend = def.Audio.Backend
		}
		if src.Audio.Format == "" {
			src.Audio.Format = def.Audio.Format
		}
		if src.Audio.Rate == 0 {
			src.Audio.Rate = def.Audio.Rate
		}
	}
	for i := range cfg.Channels {
		if cfg.Channels[i].Source == "" && len(cfg.Sources) == 1 {
			cfg.Channels[i].Source = cfg.Sources[0].Name
		}
	}
	for i := range cfg.Channels {
		ch := &cfg.Channels[i]
		for j := range ch.Decoders {
			d := &ch.Decoders[j]
			d.Protocol = strings.ToLower(d.Protocol)
			if strings.HasPrefix(d.Protocol, "pocsag") {
				if d.POCSAG == nil {
					d.POCSAG = &POCSAGOptions{}
				}
				p := d.POCSAG
				if p.Mode == "" {
					p.Mode = "alpha"
				}
				if p.Charset == "" {
					p.Charset = "US"
				}
				if p.BCHLevel == nil {
					lvl := 2
					p.BCHLevel = &lvl
				}
				if p.Polarity == "" {
					p.Polarity = "auto"
				}
			}
			if d.Protocol == "flex" && d.FLEX == nil {
				d.FLEX = &FLEXOptions{}
			}
		}
	}
	for i := range cfg.Targets {
		t := &cfg.Targets[i]
		t.Type = strings.ToLower(t.Type)
		if t.Type == "pagermon" && t.Pagermon != nil {
			p := t.Pagermon
			if p.Source == "" {
				p.Source = "{channel}"
			}
			if p.Timeout == 0 {
				p.Timeout = 10 * time.Second
			}
		}
		if t.Type == "pagermon-ingest" && t.PagermonIngest != nil {
			p := t.PagermonIngest
			if p.Label == "" {
				p.Label = "pagermon-ingest"
			}
			if p.Timeout == 0 {
				p.Timeout = 10 * time.Second
			}
		}
	}
}

// Validate checks the configuration for internal consistency.
func (c *Config) Validate() error {
	var errs []error
	fail := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	if c.Instance == "" {
		fail("instance must not be empty")
	}
	if len(c.Sources) == 0 {
		fail("at least one source is required")
	}
	srcNames := map[string]*SourceConfig{}
	devices := map[string]string{}
	for i := range c.Sources {
		src := &c.Sources[i]
		where := fmt.Sprintf("sources[%d]", i)
		if src.Name == "" {
			fail("%s.name is required when several sources are configured", where)
		} else if _, dup := srcNames[src.Name]; dup {
			fail("%s.name %q is not unique", where, src.Name)
		}
		srcNames[src.Name] = src
		switch src.Type {
		case SourceRTLSDR:
			if sel, err := ParseDeviceSelector(src.RTLSDR.Device); err != nil {
				fail("%s.rtlsdr.device: %v", where, err)
			} else if sel.Kind != SelectAny {
				if prev, dup := devices[src.RTLSDR.Device]; dup {
					fail("%s.rtlsdr.device %q is already used by source %q", where, src.RTLSDR.Device, prev)
				}
				devices[src.RTLSDR.Device] = src.Name
			} else if countType(c.Sources, SourceRTLSDR) > 1 {
				fail("%s.rtlsdr.device must be set when several rtlsdr sources are configured", where)
			}
		case SourceRTLTCP:
			if src.RTLTCP.Address == "" {
				fail("%s.rtltcp.address is required", where)
			}
		case SourceAudio:
			switch src.Audio.Backend {
			case AudioPulse, AudioStdin, AudioFile, AudioTCP, AudioUDP:
			default:
				fail("%s.audio.backend %q is not supported", where, src.Audio.Backend)
			}
			if src.Audio.Format != "s16le" && src.Audio.Format != "f32le" {
				fail("%s.audio.format %q must be s16le or f32le", where, src.Audio.Format)
			}
			if src.Audio.Rate == 0 {
				fail("%s.audio.rate must be set", where)
			}
		default:
			fail("%s.type %q must be one of rtlsdr, rtltcp, audio", where, src.Type)
		}
	}

	if len(c.Channels) == 0 {
		fail("at least one channel is required")
	}
	names := map[string]bool{}
	for i, ch := range c.Channels {
		where := fmt.Sprintf("channels[%d]", i)
		if ch.Name == "" {
			fail("%s.name is required", where)
		} else if names[ch.Name] {
			fail("%s.name %q is not unique", where, ch.Name)
		}
		names[ch.Name] = true
		src, ok := srcNames[ch.Source]
		switch {
		case ch.Source == "":
			fail("%s.source is required when several sources are configured", where)
		case !ok:
			fail("%s.source %q does not name a configured source", where, ch.Source)
		case src.Type == SourceAudio:
			if n := countChannelsOn(c.Channels, ch.Source); n != 1 {
				fail("%s: audio source %q must have exactly one channel, has %d", where, ch.Source, n)
			}
		case ch.Frequency == 0:
			fail("%s.frequency is required", where)
		}
		if len(ch.Decoders) == 0 {
			fail("%s.decoders must list at least one decoder", where)
		}
		for j, d := range ch.Decoders {
			dw := fmt.Sprintf("%s.decoders[%d]", where, j)
			if !KnownProtocol(d.Protocol) {
				fail("%s.protocol %q is unknown", dw, d.Protocol)
			}
			if d.POCSAG != nil {
				if err := d.POCSAG.validate(); err != nil {
					fail("%s.pocsag: %v", dw, err)
				}
			}
			if d.Selcall != nil && d.Selcall.SequenceTimeout < 0 {
				fail("%s.selcall.sequence_timeout must not be negative", dw)
			}
		}
	}

	if len(c.Targets) == 0 {
		fail("at least one target is required")
	}
	tnames := map[string]bool{}
	for i, t := range c.Targets {
		where := fmt.Sprintf("targets[%d]", i)
		if t.Name == "" {
			fail("%s.name is required", where)
		} else if tnames[t.Name] {
			fail("%s.name %q is not unique", where, t.Name)
		}
		tnames[t.Name] = true
		switch t.Type {
		case "pagermon":
			if t.Pagermon == nil {
				fail("%s.pagermon block is required for type pagermon", where)
			} else if err := t.Pagermon.validate(); err != nil {
				fail("%s.pagermon: %v", where, err)
			}
		case "pagermon-ingest":
			if t.PagermonIngest == nil {
				fail("%s.pagermon_ingest block is required for type pagermon-ingest", where)
			} else if err := t.PagermonIngest.validate(); err != nil {
				fail("%s.pagermon_ingest: %v", where, err)
			}
		default:
			fail("%s.type %q is unknown (supported: pagermon, pagermon-ingest)", where, t.Type)
		}
	}

	if c.Queue.MaxPerTarget <= 0 {
		fail("queue.max_per_target must be positive")
	}
	if c.Queue.MaxAttempts <= 0 {
		fail("queue.max_attempts must be positive")
	}
	if c.Queue.MaxReplays < 0 {
		fail("queue.max_replays must not be negative")
	}
	switch c.Observability.Log.Level {
	case "debug", "info", "warn", "error":
	default:
		fail("observability.log.level %q must be debug, info, warn or error", c.Observability.Log.Level)
	}
	switch c.Observability.Log.Format {
	case "text", "json":
	default:
		fail("observability.log.format %q must be text or json", c.Observability.Log.Format)
	}
	return errors.Join(errs...)
}

func countType(sources []SourceConfig, t SourceType) int {
	n := 0
	for _, s := range sources {
		if s.Type == t {
			n++
		}
	}
	return n
}

func countChannelsOn(channels []ChannelConfig, source string) int {
	n := 0
	for _, ch := range channels {
		if ch.Source == source {
			n++
		}
	}
	return n
}

func (p *POCSAGOptions) validate() error {
	switch p.Mode {
	case "auto", "standard", "numeric", "alpha", "skyper":
	default:
		return fmt.Errorf("mode %q must be auto, standard, numeric, alpha or skyper", p.Mode)
	}
	switch strings.ToUpper(p.Charset) {
	case "US", "FR", "DE", "DK", "SE", "SI":
	default:
		return fmt.Errorf("charset %q must be US, FR, DE, DK, SE or SI", p.Charset)
	}
	if p.BCHLevel != nil && (*p.BCHLevel < 0 || *p.BCHLevel > 2) {
		return fmt.Errorf("bch_level %d must be 0, 1 or 2", *p.BCHLevel)
	}
	if !validPolarity(p.Polarity) {
		return fmt.Errorf("polarity %q must be auto, normal or inverted", p.Polarity)
	}
	return nil
}

func validPolarity(s string) bool {
	return s == "auto" || s == "normal" || s == "inverted"
}

func (p *PagermonConfig) validate() error {
	return validateEndpoint(p.URL, p.APIKey, p.APIKeyFile, p.Timeout)
}

func (p *PagermonIngestConfig) validate() error {
	return validateEndpoint(p.URL, p.APIKey, p.APIKeyFile, p.Timeout)
}

// validateEndpoint checks the fields every HTTP target shares.
func validateEndpoint(url, apiKey, apiKeyFile string, timeout time.Duration) error {
	var errs []error
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		errs = append(errs, fmt.Errorf("url %q must start with http:// or https://", url))
	}
	if apiKey == "" && apiKeyFile == "" {
		errs = append(errs, errors.New("api_key or api_key_file is required"))
	}
	if apiKey != "" && apiKeyFile != "" {
		errs = append(errs, errors.New("api_key and api_key_file are mutually exclusive"))
	}
	if timeout < 0 {
		errs = append(errs, errors.New("timeout must not be negative"))
	}
	return errors.Join(errs...)
}

// ResolveSecret returns value, or the trimmed contents of file when value
// is empty and file is set.
func ResolveSecret(value, file string) (string, error) {
	if value != "" || file == "" {
		return value, nil
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return "", fmt.Errorf("read secret file: %w", err)
	}
	return strings.TrimSpace(string(b)), nil
}

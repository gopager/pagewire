// Package config loads, validates and documents the pagewire
// configuration. The YAML file is the primary source; scalar values can be
// overridden through environment variables (PAGEWIRE__SECTION__KEY) and secrets
// can be read from files via the *_file variants.
package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Config is the root of the configuration tree.
type Config struct {
	// Instance names this gateway process. It must differ between instances
	// that share a host; it appears in logs, metrics and the {instance}
	// placeholder of delivery targets.
	Instance      string              `koanf:"instance"`
	Sources       []SourceConfig      `koanf:"sources"`
	Channels      []ChannelConfig     `koanf:"channels"`
	Targets       []TargetConfig      `koanf:"targets"`
	Queue         QueueConfig         `koanf:"queue"`
	Observability ObservabilityConfig `koanf:"observability"`
}

// SourceType selects where samples come from.
type SourceType string

// Supported source types.
const (
	SourceRTLSDR SourceType = "rtlsdr"
	SourceRTLTCP SourceType = "rtltcp"
	SourceAudio  SourceType = "audio"
)

// SourceConfig selects and configures one sample source. Channels refer to
// a source by name; a single source may be left unnamed.
type SourceConfig struct {
	Name   string       `koanf:"name"`
	Type   SourceType   `koanf:"type"`
	RTLSDR RTLSDRConfig `koanf:"rtlsdr"`
	RTLTCP RTLTCPConfig `koanf:"rtltcp"`
	Audio  AudioConfig  `koanf:"audio"`
}

// RTLSDRConfig configures a locally attached dongle.
type RTLSDRConfig struct {
	// Device selects the dongle: "serial:<usb serial>", "usb:<bus>-<port.port…>"
	// (physical socket, see "pagewire devices"), "index:<n>" or
	// empty to use the only attached device. Index selection is discouraged
	// because the order changes between boots.
	Device string `koanf:"device"`
	// SampleRate forces the capture rate in S/s; 0 picks the lowest rate
	// that covers all channels.
	SampleRate uint32 `koanf:"sample_rate"`
	Gain       Gain   `koanf:"gain"`
	PPM        int    `koanf:"ppm"`
	BiasTee    bool   `koanf:"bias_tee"`
	// Bandwidth forces the tuner IF filter width in Hz (0 = automatic from
	// the sample rate). Only tuners with a bandwidth control honour it.
	Bandwidth uint32 `koanf:"bandwidth"`
	// IFGains sets per-stage IF gains in dB for tuners that have them
	// (E4000: six stages); other tuners reject the setting.
	IFGains []float64 `koanf:"if_gains"`
}

// RTLSDRPPM returns the configured frequency correction of an SDR source.
func (s SourceConfig) RTLSDRPPM() int {
	if s.Type == SourceRTLTCP {
		return s.RTLTCP.PPM
	}
	return s.RTLSDR.PPM
}

// RTLTCPConfig configures a remote rtl_tcp server.
type RTLTCPConfig struct {
	Address    string `koanf:"address"`
	SampleRate uint32 `koanf:"sample_rate"`
	Gain       Gain   `koanf:"gain"`
	PPM        int    `koanf:"ppm"`
}

// AudioBackend names an audio input implementation.
type AudioBackend string

// Supported audio backends.
const (
	AudioPulse AudioBackend = "pulse"
	AudioStdin AudioBackend = "stdin"
	AudioFile  AudioBackend = "file"
	AudioTCP   AudioBackend = "tcp"
	AudioUDP   AudioBackend = "udp"
)

// AudioConfig configures an already demodulated audio input.
type AudioConfig struct {
	Backend AudioBackend `koanf:"backend"`
	// Format is the raw sample format: s16le or f32le. Ignored for WAV files.
	Format string `koanf:"format"`
	// Rate is the input sample rate in Hz.
	Rate uint32 `koanf:"rate"`
	// Device is the backend-specific source name, path or address.
	Device string `koanf:"device"`
}

// ChannelConfig describes one receive frequency and its decoders.
type ChannelConfig struct {
	Name string `koanf:"name"`
	// Source is the name of the source this channel is received on. It may
	// be omitted when exactly one source is configured.
	Source    string          `koanf:"source"`
	Frequency Frequency       `koanf:"frequency"`
	Squelch   Level           `koanf:"squelch"`
	PowerGate *bool           `koanf:"power_gate"`
	Decoders  []DecoderConfig `koanf:"decoders"`
}

// PowerGateEnabled reports whether idle-channel gating is on (default true).
func (c ChannelConfig) PowerGateEnabled() bool {
	return c.PowerGate == nil || *c.PowerGate
}

// DecoderConfig selects a protocol decoder and its options.
type DecoderConfig struct {
	Protocol string          `koanf:"protocol"`
	POCSAG   *POCSAGOptions  `koanf:"pocsag"`
	FLEX     *FLEXOptions    `koanf:"flex"`
	FMS      *FMSOptions     `koanf:"fms"`
	Selcall  *SelcallOptions `koanf:"selcall"`
	EAS      *EASOptions     `koanf:"eas"`
	MPT1327  *MPT1327Options `koanf:"mpt1327"`
	GSC      *GSCOptions     `koanf:"gsc"`
}

// FMSOptions tunes the FMS decoder (Funkmeldesystem).
type FMSOptions struct{}

// SelcallOptions tunes the selective-call tone decoders (ZVEI, CCIR, EEA,
// EIA, DTMF, DUALTONE).
type SelcallOptions struct {
	// SequenceTimeout is the longest pause between two DTMF keys that still
	// belongs to the same sequence, measured in audio time; a longer pause
	// (or the end of the input) completes the sequence. The other variants
	// end a sequence at the decoder's own tone pause. Default 1s.
	SequenceTimeout time.Duration `koanf:"sequence_timeout"`
}

// EASOptions tunes the EAS/SAME decoder.
type EASOptions struct {
	// EndOfMessage also emits the end-of-message marker (NNNN) as a message
	// of its own. The decoder reports every received marker (SAME sends it
	// three times), without deduplication.
	EndOfMessage bool `koanf:"end_of_message"`
}

// MPT1327Options tunes the MPT1327 decoder.
type MPT1327Options struct {
	// IncludeSystem also emits the control and data channel system
	// codewords (CCSC, DCSC); by default only traffic and control messages
	// are delivered.
	IncludeSystem bool `koanf:"include_system"`
}

// GSCOptions tunes the GSC decoder.
type GSCOptions struct {
	// IncludeVoice also emits the start and end of voice messages (as tone
	// messages with a voice payload); by default only pages are delivered.
	IncludeVoice bool `koanf:"include_voice"`
}

// POCSAGOptions tunes the POCSAG decoder.
type POCSAGOptions struct {
	// Mode selects which rendering Message.Text holds: alpha (default),
	// numeric, skyper, standard (function 0 numeric, others alpha, as the
	// POCSAG standard suggests) or auto (the more plausible rendering by
	// character statistics; a heuristic, so opt-in). All renderings are in
	// the payload regardless.
	Mode string `koanf:"mode"`
	// Charset is US, FR, DE, DK, SE or SI.
	Charset string `koanf:"charset"`
	// BCHLevel is the error correction strength 0–2 (0 disables).
	BCHLevel *int `koanf:"bch_level"`
	// Polarity is auto, normal or inverted.
	Polarity    string `koanf:"polarity"`
	ShowPartial bool   `koanf:"show_partial"`
	PruneEmpty  bool   `koanf:"prune_empty"`
	Heuristics  bool   `koanf:"heuristics"`
}

// FLEXOptions tunes the FLEX decoder.
type FLEXOptions struct {
	// ShowPartial also emits unfinished fragments and orphans; by default
	// only complete or reassembled pages are delivered.
	ShowPartial bool `koanf:"show_partial"`
	// IncludeSystem emits FLEX system messages (network identifiers,
	// time, tone-only short messages) as alert-kind messages.
	IncludeSystem bool `koanf:"include_system"`
	// ExpandGroups emits one message per member capcode of a group page
	// (default true); otherwise one message with the group address.
	ExpandGroups *bool `koanf:"expand_groups"`
}

// ExpandGroupsEnabled reports whether group pages are fanned out.
func (o *FLEXOptions) ExpandGroupsEnabled() bool {
	return o == nil || o.ExpandGroups == nil || *o.ExpandGroups
}

// TargetConfig describes one delivery target.
type TargetConfig struct {
	Name           string                `koanf:"name"`
	Type           string                `koanf:"type"`
	Pagermon       *PagermonConfig       `koanf:"pagermon"`
	PagermonIngest *PagermonIngestConfig `koanf:"pagermon_ingest"`
}

// PagermonConfig configures delivery to a PagerMon server. The sink follows
// the pagermon/pagermon server API (POST /api/messages with address,
// message, datetime, source) and reproduces what the stock PagerMon client
// (client/reader.js) sends for POCSAG, FLEX and EAS, so a server sees no
// difference to a stock installation.
type PagermonConfig struct {
	URL        string `koanf:"url"`
	APIKey     string `koanf:"api_key"`
	APIKeyFile string `koanf:"api_key_file"`
	// Source is a template for the PagerMon "source" field (the stock
	// client's "identifier"). Placeholders: {channel}, {frequency},
	// {protocol}, {hostname}, {instance}.
	Source string `koanf:"source"`
	// SendFunctionCode appends the POCSAG function digit to the address
	// before padding, like the stock client's sendFunctionCode option
	// (default false).
	SendFunctionCode bool `koanf:"send_function_code"`
	// UseTimestamp mirrors the stock client's useTimestamp option (default
	// true): a timestamp at the start of an alphanumeric page is removed
	// from the text and used as the message time.
	UseTimestamp *bool `koanf:"use_timestamp"`
	// ToneText is sent as the message body for tone-only pages, which the
	// stock client drops because the API requires a message; empty keeps
	// that behaviour.
	ToneText string        `koanf:"tone_text"`
	Timeout  time.Duration `koanf:"timeout"`
	// InsecureSkipVerify disables TLS certificate verification.
	InsecureSkipVerify bool `koanf:"insecure_skip_verify"`
}

// UseTimestampEnabled reports whether embedded timestamps are honoured.
func (p *PagermonConfig) UseTimestampEnabled() bool {
	return p == nil || p.UseTimestamp == nil || *p.UseTimestamp
}

// PagermonIngestConfig configures delivery to a PagerMon server in the
// shape the retired pagermon-ingest-core service (fed by
// pagermon-ingest-adapter-multimon) produced; see
// internal/sink/pagermoningest for the reproduced behaviour.
type PagermonIngestConfig struct {
	URL        string `koanf:"url"`
	APIKey     string `koanf:"api_key"`
	APIKeyFile string `koanf:"api_key_file"`
	// Label is ingest-core's service label (INGEST_CORE__LABEL), sent as
	// the "source" of every message (default "pagermon-ingest").
	Label   string        `koanf:"label"`
	Timeout time.Duration `koanf:"timeout"`
	// InsecureSkipVerify disables TLS certificate verification.
	InsecureSkipVerify bool `koanf:"insecure_skip_verify"`
}

// QueueConfig configures the persistent delivery queue.
type QueueConfig struct {
	Path         string        `koanf:"path"`
	MaxPerTarget int           `koanf:"max_per_target"`
	MaxAttempts  int           `koanf:"max_attempts"`
	MaxBackoff   time.Duration `koanf:"max_backoff"`
	// AutoReplay re-queues dead letters when the gateway starts and
	// whenever a target delivers again after server errors (default true).
	AutoReplay *bool `koanf:"auto_replay"`
	// MaxReplays bounds how often an entry is replayed automatically
	// before it stays dead until a manual "queue replay" (default 3).
	MaxReplays int `koanf:"max_replays"`
	// DrainTimeout is how long a run with finite sources (recordings,
	// pipes) waits for the queues to empty before it stops (default 30s).
	DrainTimeout time.Duration `koanf:"drain_timeout"`
}

// AutoReplayEnabled reports whether dead letters are replayed automatically.
func (q QueueConfig) AutoReplayEnabled() bool { return q.AutoReplay == nil || *q.AutoReplay }

// ObservabilityConfig configures metrics, health endpoints and logging.
type ObservabilityConfig struct {
	Listen  string    `koanf:"listen"`
	Metrics bool      `koanf:"metrics"`
	Pprof   bool      `koanf:"pprof"`
	Log     LogConfig `koanf:"log"`
}

// LogConfig configures slog output.
type LogConfig struct {
	// Level is debug, info, warn or error.
	Level string `koanf:"level"`
	// Format is text or json.
	Format string `koanf:"format"`
}

// Frequency is a frequency in Hz that accepts SI suffixes in text form,
// e.g. "150.000M", "150000000", "460.000 MHz".
type Frequency uint64

// UnmarshalText implements encoding.TextUnmarshaler.
func (f *Frequency) UnmarshalText(text []byte) error {
	v, err := ParseFrequency(string(text))
	if err != nil {
		return err
	}
	*f = v
	return nil
}

// String formats the frequency with the largest fitting SI prefix.
func (f Frequency) String() string {
	switch {
	case f >= 1e9 && f%1e3 == 0:
		return strconv.FormatFloat(float64(f)/1e9, 'f', -1, 64) + "G"
	case f >= 1e6 && f%1e3 == 0:
		return strconv.FormatFloat(float64(f)/1e6, 'f', -1, 64) + "M"
	case f >= 1e3 && f%1e3 == 0:
		return strconv.FormatFloat(float64(f)/1e3, 'f', -1, 64) + "k"
	default:
		return strconv.FormatUint(uint64(f), 10)
	}
}

// ParseFrequency parses a frequency with an optional SI suffix (k, M, G) and
// an optional "Hz" unit.
func ParseFrequency(s string) (Frequency, error) {
	t := strings.TrimSpace(s)
	t = strings.TrimSuffix(strings.TrimSuffix(t, "Hz"), "hz")
	t = strings.TrimSpace(t)
	mult := 1.0
	if n := len(t); n > 0 {
		switch t[n-1] {
		case 'k', 'K':
			mult, t = 1e3, t[:n-1]
		case 'M':
			mult, t = 1e6, t[:n-1]
		case 'G':
			mult, t = 1e9, t[:n-1]
		}
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
	if err != nil || v <= 0 {
		return 0, fmt.Errorf("invalid frequency %q", s)
	}
	return Frequency(v*mult + 0.5), nil
}

// Gain is a tuner gain that is either automatic or a fixed value in dB.
type Gain struct {
	Auto bool
	// TenthDB is the gain in tenths of a dB when Auto is false.
	TenthDB int
}

// UnmarshalText accepts "auto" or a number of dB such as "28.0".
func (g *Gain) UnmarshalText(text []byte) error {
	s := strings.TrimSpace(string(text))
	if s == "" || strings.EqualFold(s, "auto") {
		*g = Gain{Auto: true}
		return nil
	}
	v, err := strconv.ParseFloat(strings.TrimSuffix(s, "dB"), 64)
	if err != nil {
		return fmt.Errorf("invalid gain %q (use \"auto\" or dB)", s)
	}
	*g = Gain{TenthDB: int(v*10 + 0.5)}
	return nil
}

// String implements fmt.Stringer.
func (g Gain) String() string {
	if g.Auto {
		return "auto"
	}
	return strconv.FormatFloat(float64(g.TenthDB)/10, 'f', 1, 64)
}

// Level is a signal threshold in dB that can also be "auto". An omitted
// level is treated as auto.
type Level struct {
	Auto bool
	DB   float64
	// Set records that a value was configured at all.
	Set bool
}

// UnmarshalText accepts "auto" or a number of dB.
func (l *Level) UnmarshalText(text []byte) error {
	s := strings.TrimSpace(string(text))
	if s == "" || strings.EqualFold(s, "auto") {
		*l = Level{Auto: true, Set: true}
		return nil
	}
	v, err := strconv.ParseFloat(strings.TrimSuffix(s, "dB"), 64)
	if err != nil {
		return fmt.Errorf("invalid level %q (use \"auto\" or dB)", s)
	}
	*l = Level{DB: v, Set: true}
	return nil
}

// IsAuto reports whether the automatic squelch applies (explicit auto or
// omitted).
func (l Level) IsAuto() bool { return l.Auto || !l.Set }

// String implements fmt.Stringer.
func (l Level) String() string {
	if l.Auto {
		return "auto"
	}
	return strconv.FormatFloat(l.DB, 'f', 1, 64)
}

// Package pagermoningest delivers POCSAG pages to a PagerMon server exactly
// as the retired Node.js chain pagermon-ingest-adapter-multimon →
// pagermon-ingest-core did (rtl_fm | multimon-ng --json, parsed by the
// adapter, queued and posted by the core), so that a server fed by that
// chain sees no change when the gateway replaces it. The chain's decisions
// are reproduced on purpose, including the ones that differ from the stock
// PagerMon client (see the pagermon sink for that one):
//
//   - Only POCSAG pages are sent. The adapter matched multimon-ng's
//     demod_name against "POCSAG" and "FLEX"; FLEX_NEXT's JSON carried no
//     demod_name and the legacy FLEX decoder called itself
//     "flex_alphanumeric", so no FLEX page ever passed. Other protocols were
//     unknown to it.
//   - The address is the RIC followed by the function digit. The chain lost
//     the RIC's leading zeros because the adapter read it from a JSON
//     number ("5010" for RIC 501); that is not reproduced: the RIC is sent
//     as its seven digits ("00005010"), so every address has eight digits.
//   - The message is multimon-ng's printed text as it stood in the "alpha"
//     or "numeric" JSON field: control characters as "<NUL>"-style tokens,
//     the padding of the last codeword included, nothing trimmed, nothing
//     substituted. Pages whose text is blank are not sent. Tone-only pages
//     are sent with an empty message and the format "tone" (a stock server
//     rejects those with 500, which the chain then retried); prune_empty on
//     the decoder keeps them out, as multimon-ng's -e did in the chain.
//   - The body is what ingest-core's Message.toPayload() produced, in that
//     key order: protocol (multimon-ng's demod name such as POCSAG1200),
//     source (the service label), format, address, message, timestamp
//     (Unix seconds) and time (UTC ISO 8601 with microseconds, as
//     multimon-ng printed it with --timestamp --iso8601). There is no
//     datetime field, so a stock server stores every message with
//     datetime 1.
//   - Every 2xx reply is parsed as JSON; a reply that is not JSON (a stock
//     server's "Ignoring duplicate") counts as a retryable failure, as it
//     did in ingest-core. 5xx, timeouts and network errors are retried too;
//     401 and 403 wait for the target to pass its health check; every other
//     4xx is final.
//   - The health check is GET /api/health, which must answer
//     {"status":"ok"} within five seconds. Stock PagerMon has no such
//     route; the servers this mode is meant for do.
//
// The one thing that cannot be reproduced is multimon-ng's mode "auto",
// which printed a numeric and an alphanumeric line for the same page, so the
// chain sent two messages; the gateway's decoder selects one rendering.
package pagermoningest

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gopager/pagewire/internal/buildinfo"
	"github.com/gopager/pagewire/internal/config"
	"github.com/gopager/pagewire/internal/decoder/mmng"
	"github.com/gopager/pagewire/internal/message"
	"github.com/gopager/pagewire/internal/sink"
)

// Type is the sink type name in the configuration.
const Type = "pagermon-ingest"

// healthTimeout is ingest-core's timeout for GET /api/health.
const healthTimeout = 5 * time.Second

// ricDigits is the width of a POCSAG RIC (21 bits, at most 2097151).
const ricDigits = 7

func init() {
	sink.Register(Type, New)
}

// Sink posts to one PagerMon server in the ingest-core shape.
type Sink struct {
	name   string
	cfg    config.PagermonIngestConfig
	apiKey string
	client *http.Client
	url    string
}

// New builds the sink from a target configuration.
func New(cfg config.TargetConfig, _ sink.Env) (sink.Sink, error) {
	if cfg.PagermonIngest == nil {
		return nil, errors.New("pagermon-ingest: missing pagermon_ingest block")
	}
	key, err := config.ResolveSecret(cfg.PagermonIngest.APIKey, cfg.PagermonIngest.APIKeyFile)
	if err != nil {
		return nil, fmt.Errorf("pagermon-ingest: target %s: %w", cfg.Name, err)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.PagermonIngest.InsecureSkipVerify {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // explicit operator choice
	}
	timeout := cfg.PagermonIngest.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	return &Sink{
		name:   cfg.Name,
		cfg:    *cfg.PagermonIngest,
		apiKey: key,
		client: &http.Client{Transport: transport, Timeout: timeout},
		url:    strings.TrimRight(cfg.PagermonIngest.URL, "/"),
	}, nil
}

// Name implements sink.Sink.
func (s *Sink) Name() string { return s.name }

// Type implements sink.Sink.
func (s *Sink) Type() string { return Type }

// payload is ingest-core's Message.toPayload() result, fields in the order
// JavaScript serialised them.
type payload struct {
	Protocol  string `json:"protocol"`
	Source    string `json:"source"`
	Format    string `json:"format"`
	Address   string `json:"address"`
	Message   string `json:"message"`
	Timestamp int64  `json:"timestamp"`
	Time      string `json:"time"`
}

// Build maps a decoded message to the request body, or returns ok=false
// when the chain would not have produced a message for it.
func (s *Sink) Build(msg *message.Message) (payload, bool) {
	if !strings.HasPrefix(msg.Protocol, "pocsag") {
		return payload{}, false
	}
	format, text := "tone", ""
	switch msg.Kind {
	case message.KindAlpha:
		format, text = "alpha", mmng.PrintedText(msg.Text)
	case message.KindNumeric:
		format, text = "numeric", msg.Text
	}
	// The adapter dropped alpha and numeric pages whose text was blank
	// (String.prototype.trim). The printed text holds no whitespace other
	// than spaces, because control characters are rendered as tokens, so
	// TrimSpace decides the same way.
	if format != "tone" && strings.TrimSpace(text) == "" {
		return payload{}, false
	}
	// String(obj.address || 0) + String(obj.function || 0), with the RIC
	// restored to its seven digits.
	address := msg.Address
	if address == "" {
		address = "0"
	}
	if len(address) < ricDigits {
		address = strings.Repeat("0", ricDigits-len(address)) + address
	}
	fn := msg.Function
	if fn < 0 {
		fn = 0
	}
	utc := msg.ReceivedAt.UTC()
	return payload{
		Protocol:  strings.ToUpper(msg.Protocol),
		Source:    s.cfg.Label,
		Format:    format,
		Address:   address + strconv.Itoa(fn),
		Message:   text,
		Timestamp: utc.Unix(),
		Time:      utc.Format("2006-01-02T15:04:05.000000"),
	}, true
}

// Deliver implements sink.Sink.
func (s *Sink) Deliver(ctx context.Context, msg *message.Message) (sink.Outcome, error) {
	body, ok := s.Build(msg)
	if !ok {
		return sink.OutcomeSkipped, nil
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // JSON.stringify leaves <, > and & alone
	if err := enc.Encode(body); err != nil {
		return sink.OutcomeRejected, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url+"/api/messages", bytes.NewReader(bytes.TrimRight(buf.Bytes(), "\n")))
	if err != nil {
		return "", err
	}
	s.headers(req)
	resp, err := s.client.Do(req)
	if err != nil {
		return "", &sink.Error{Class: sink.ClassNetwork, Err: err}
	}
	defer resp.Body.Close()
	reply, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		// ingest-core: JSON.parse(data) on every 2xx body; an empty body
		// passed as {}.
		if trimmed := bytes.TrimSpace(reply); len(trimmed) > 0 && !json.Valid(trimmed) {
			return "", &sink.Error{Class: sink.ClassServer, Err: fmt.Errorf("pagermon-ingest: HTTP %d reply is not JSON: %s", resp.StatusCode, truncate(string(trimmed), 200))}
		}
		return sink.OutcomeDelivered, nil
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		return "", &sink.Error{Class: sink.ClassAuth, Err: fmt.Errorf("pagermon-ingest: HTTP %d: %s", resp.StatusCode, truncate(string(reply), 200))}
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		return sink.OutcomeRejected, nil
	default:
		return "", &sink.Error{Class: sink.ClassServer, Err: fmt.Errorf("pagermon-ingest: HTTP %d: %s", resp.StatusCode, truncate(string(reply), 200))}
	}
}

// Check implements sink.Sink with ingest-core's health probe.
func (s *Sink) Check(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, healthTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url+"/api/health", nil)
	if err != nil {
		return err
	}
	s.headers(req)
	resp, err := s.client.Do(req)
	if err != nil {
		return &sink.Error{Class: sink.ClassNetwork, Err: err}
	}
	defer resp.Body.Close()
	reply, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	switch {
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		return &sink.Error{Class: sink.ClassAuth, Err: fmt.Errorf("pagermon-ingest: health HTTP %d", resp.StatusCode)}
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		return &sink.Error{Class: sink.ClassServer, Err: fmt.Errorf("pagermon-ingest: health HTTP %d", resp.StatusCode)}
	}
	var status struct {
		Status string `json:"status"`
	}
	if json.Unmarshal(reply, &status) != nil || status.Status != "ok" {
		return &sink.Error{Class: sink.ClassServer, Err: fmt.Errorf("pagermon-ingest: health reply %s is not {\"status\":\"ok\"}", truncate(strings.TrimSpace(string(reply)), 200))}
	}
	return nil
}

// headers sets what ingest-core's ApiClient sent on every request.
func (s *Sink) headers(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apikey", s.apiKey)
	req.Header.Set("User-Agent", buildinfo.UserAgent())
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

package pagermoningest

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gopager/pagewire/internal/config"
	"github.com/gopager/pagewire/internal/message"
	"github.com/gopager/pagewire/internal/sink"
)

type seen struct {
	body, apikey, contentType, path string
}

func server(t *testing.T, reply string, status int, got *seen) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		got.body = string(raw)
		got.apikey = r.Header.Get("apikey")
		got.contentType = r.Header.Get("Content-Type")
		got.path = r.Method + " " + r.URL.Path
		if r.URL.Path == "/api/health" {
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(reply))
	}))
}

func newSink(t *testing.T, url, label string) *Sink {
	t.Helper()
	s, err := New(config.TargetConfig{Name: "t", Type: Type, PagermonIngest: &config.PagermonIngestConfig{URL: url, APIKey: "secret", Label: label}}, sink.Env{})
	if err != nil {
		t.Fatal(err)
	}
	return s.(*Sink)
}

func pocsag(addr string, fn int, kind message.Kind, text string) *message.Message {
	return &message.Message{
		ReceivedAt: time.Date(2026, 11, 18, 9, 30, 15, 156337000, time.FixedZone("CET", 3600)),
		Protocol:   "pocsag1200",
		Address:    addr,
		Function:   fn,
		Kind:       kind,
		Text:       text,
	}
}

// TestBodyShape checks the exact JSON body ingest-core produced: its field
// set and order, the seven-digit RIC plus function address, multimon-ng's
// printed text untouched, and the two time fields.
func TestBodyShape(t *testing.T) {
	var got seen
	srv := server(t, `{"id":4711}`, 200, &got)
	defer srv.Close()
	s := newSink(t, srv.URL, "pager")
	out, err := s.Deliver(context.Background(), pocsag("501", 0, message.KindAlpha, "FW Test\x04\x04\x00"))
	if err != nil || out != sink.OutcomeDelivered {
		t.Fatalf("outcome %q err %v", out, err)
	}
	want := `{"protocol":"POCSAG1200","source":"pager","format":"alpha","address":"00005010","message":"FW Test<EOT><EOT><NUL>","timestamp":1794990615,"time":"2026-11-18T08:30:15.156337"}`
	if got.body != want {
		t.Errorf("body\n got %s\nwant %s", got.body, want)
	}
	if got.apikey != "secret" || got.contentType != "application/json" || got.path != "POST /api/messages" {
		t.Errorf("request %+v", got)
	}
}

func TestMapping(t *testing.T) {
	s := newSink(t, "http://unused", "pagermon-ingest")
	cases := []struct {
		name            string
		msg             *message.Message
		sent            bool
		address, format string
		text            string
	}{
		{"alpha keeps padding and tokens", pocsag("1234567", 3, message.KindAlpha, " text \x04"), true, "12345673", "alpha", " text <EOT>"},
		{"numeric unchanged", pocsag("54321", 1, message.KindNumeric, "112-4711  "), true, "00543211", "numeric", "112-4711  "},
		{"tone sent with empty message", pocsag("777", 2, message.KindTone, ""), true, "00007772", "tone", ""},
		{"blank alpha dropped", pocsag("777", 0, message.KindAlpha, "   "), false, "", "", ""},
		{"only control chars are not blank", pocsag("777", 0, message.KindAlpha, "\x04"), true, "00007770", "alpha", "<EOT>"},
		{"address 0 like String(0)", pocsag("", 1, message.KindNumeric, "1"), true, "00000001", "numeric", "1"},
		{"flex never passed the adapter", &message.Message{Protocol: "flex", Address: "1234567", Kind: message.KindAlpha, Text: "x"}, false, "", "", ""},
		{"fms unknown to the adapter", &message.Message{Protocol: "fms", Address: "1", Kind: message.KindStatus, Text: "x"}, false, "", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, ok := s.Build(c.msg)
			if ok != c.sent {
				t.Fatalf("sent=%v want %v (%+v)", ok, c.sent, p)
			}
			if !ok {
				return
			}
			if p.Address != c.address || p.Format != c.format || p.Message != c.text || p.Source != "pagermon-ingest" {
				t.Errorf("got %+v", p)
			}
		})
	}
}

// TestOutcomes checks the reply classification ingest-core applied.
func TestOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		reply  string
		status int
		out    sink.Outcome
		class  sink.Class
	}{
		{"json id", `7`, 200, sink.OutcomeDelivered, ""},
		{"json object", `{"id":7,"reason":"stored"}`, 200, sink.OutcomeDelivered, ""},
		{"empty body", ``, 200, sink.OutcomeDelivered, ""},
		{"stock duplicate text is a retryable failure", `Ignoring duplicate`, 200, "", sink.ClassServer},
		{"missing field 500", `{"message":"Error - address or message missing"}`, 500, "", sink.ClassServer},
		{"bad gateway", `upstream down`, 502, "", sink.ClassServer},
		{"unauthorized", `Unauthorized`, 401, "", sink.ClassAuth},
		{"bad request is final", `nope`, 400, sink.OutcomeRejected, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got seen
			srv := server(t, c.reply, c.status, &got)
			defer srv.Close()
			out, err := newSink(t, srv.URL, "pager").Deliver(context.Background(), pocsag("501", 0, message.KindAlpha, "x"))
			if c.class == "" {
				if err != nil || out != c.out {
					t.Fatalf("outcome %q err %v, want %q", out, err, c.out)
				}
				return
			}
			var se *sink.Error
			if !errors.As(err, &se) || se.Class != c.class {
				t.Fatalf("err %v, want class %s", err, c.class)
			}
		})
	}
}

func TestUnreachable(t *testing.T) {
	s := newSink(t, "http://127.0.0.1:1", "pager")
	_, err := s.Deliver(context.Background(), pocsag("501", 0, message.KindAlpha, "x"))
	if sink.Classify(err) != sink.ClassNetwork {
		t.Fatalf("err %v, want network", err)
	}
	if err := s.Check(context.Background()); sink.Classify(err) != sink.ClassNetwork {
		t.Fatalf("check err %v, want network", err)
	}
}

func TestCheck(t *testing.T) {
	cases := []struct {
		name   string
		reply  string
		status int
		ok     bool
		class  sink.Class
	}{
		{"ok", `{"status":"ok"}`, 200, true, ""},
		{"degraded", `{"status":"degraded"}`, 200, false, sink.ClassServer},
		{"not json", `OK`, 200, false, sink.ClassServer},
		{"stock server has no route", `Cannot GET /api/health`, 404, false, sink.ClassServer},
		{"forbidden", `{}`, 403, false, sink.ClassAuth},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got seen
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got.path = r.Method + " " + r.URL.Path
				got.apikey = r.Header.Get("apikey")
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(c.reply))
			}))
			defer srv.Close()
			err := newSink(t, srv.URL, "pager").Check(context.Background())
			if got.path != "GET /api/health" || got.apikey != "secret" {
				t.Errorf("request %+v", got)
			}
			if c.ok {
				if err != nil {
					t.Fatalf("err %v", err)
				}
				return
			}
			if err == nil || sink.Classify(err) != c.class || !strings.Contains(err.Error(), "health") {
				t.Fatalf("err %v, want class %s", err, c.class)
			}
		})
	}
}

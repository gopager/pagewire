package pagermon

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gopager/pagewire/internal/config"
	"github.com/gopager/pagewire/internal/message"
	"github.com/gopager/pagewire/internal/sink"
)

type seen struct {
	body   map[string]any
	apikey string
	ua     string
	getKey string
}

func server(t *testing.T, reply string, status int, got *seen) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/messages" && r.Method == http.MethodGet {
			got.getKey = r.Header.Get("apikey")
			_, _ = w.Write([]byte("[]"))
			return
		}
		if r.URL.Path != "/api/messages" || r.Method != http.MethodPost {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		got.apikey = r.Header.Get("apikey")
		got.ua = r.Header.Get("User-Agent")
		got.body = nil
		_ = json.Unmarshal(raw, &got.body)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(reply))
	}))
}

func newSink(t *testing.T, url string, pc config.PagermonConfig) *Sink {
	pc.URL = url
	if pc.APIKey == "" {
		pc.APIKey = "k"
	}
	if pc.Source == "" {
		pc.Source = "{instance}/{channel}"
	}
	s, err := New(config.TargetConfig{Name: "t", Type: "pagermon", Pagermon: &pc}, sink.Env{Instance: "gw", Hostname: "h"})
	if err != nil {
		t.Fatal(err)
	}
	return s.(*Sink)
}

var received = time.Unix(1700000000, 0)

func pocsagMsg(addr string, fn int, kind message.Kind, text string) *message.Message {
	return &message.Message{ReceivedAt: received, Protocol: "pocsag1200", Channel: message.Channel{Name: "paging", FreqHz: 150000000},
		Address: addr, Function: fn, Kind: kind, Text: text,
		Payload: &message.POCSAGPayload{Baud: 1200, Alpha: text}}
}

func TestDeliverContract(t *testing.T) {
	var got seen
	srv := server(t, "42", 200, &got)
	defer srv.Close()
	s := newSink(t, srv.URL+"/", config.PagermonConfig{SendFunctionCode: true})
	out, err := s.Deliver(context.Background(), pocsagMsg("501", 2, message.KindAlpha, "FW Test\x00\x00"))
	if err != nil || out != sink.OutcomeDelivered {
		t.Fatalf("deliver = %s, %v", out, err)
	}
	if got.apikey != "k" || got.ua == "" {
		t.Errorf("headers: %+v", got)
	}
	// Exactly the four fields of the server API, shaped like the stock client:
	// function digit appended then padded to 7, NUL padding tokens removed.
	want := map[string]any{"address": "0005012", "message": "FW Test", "datetime": float64(1700000000), "source": "gw/paging"}
	if len(got.body) != len(want) {
		t.Errorf("body has extra fields: %+v", got.body)
	}
	for k, v := range want {
		if got.body[k] != v {
			t.Errorf("%s = %v, want %v", k, got.body[k], v)
		}
	}
	if err := s.Check(context.Background()); err != nil || got.getKey != "k" {
		t.Errorf("check: %v (apikey %q)", err, got.getKey)
	}
}

// TestPOCSAGTextRules covers reader.js' text handling on multimon-ng's
// printed form of a page.
func TestPOCSAGTextRules(t *testing.T) {
	s := newSink(t, "http://127.0.0.1:1", config.PagermonConfig{})
	cases := []struct {
		name       string
		kind       message.Kind
		in         string
		want       string
		wantSkip   bool
		wantUnixTS int64
	}{
		{"three-letter tokens removed, two-letter kept", message.KindAlpha, "A\x04B\x0aC\x19D\x7f", "AB<LF>C<EM>D", false, 0},
		{"trailing spaces and NUL padding trimmed", message.KindAlpha, "  Text  \x00\x00", "Text", false, 0},
		{"German charset reverted for Ä and Ü only", message.KindAlpha, "Ärger Übung Öl äöüß", "[rger ]bung Öl äöüß", false, 0},
		{"EOT inside text does not cut it", message.KindAlpha, "AES\x04CIPHER\x04", "AESCIPHER", false, 0},
		{"numeric: trimmed once, no final trim after tokens", message.KindNumeric, " 112-4711 \x04", "112-4711 ", false, 0},
		{"empty after trim is not sent", message.KindAlpha, "\x00\x00\x00", "", true, 0},
		{"tone without placeholder is not sent", message.KindTone, "", "", true, 0},
		{"long timestamp becomes message time and is removed", message.KindAlpha, "17 November 2026 20:14:00 Alarm", "Alarm",
			false, time.Date(2026, 11, 17, 20, 14, 0, 0, time.Local).Unix()},
		{"ISO timestamp", message.KindAlpha, "Einsatz 2026-11-17 20:14:00 Brand", "Einsatz  Brand",
			false, time.Date(2026, 11, 17, 20, 14, 0, 0, time.Local).Unix()},
		{"timestamp with unknown month stays", message.KindAlpha, "17 Nebelmonat 2026 20:14:00 Alarm", "17 Nebelmonat 2026 20:14:00 Alarm", false, 0},
		{"invalid date stays", message.KindAlpha, "31 February 2026 20:14:00 Alarm", "31 February 2026 20:14:00 Alarm", false, 0},
	}
	for _, c := range cases {
		r, ok := s.Build(pocsagMsg("1234567", 1, c.kind, c.in))
		if ok == c.wantSkip {
			t.Errorf("%s: sent=%v", c.name, ok)
			continue
		}
		if !ok {
			continue
		}
		if r.Message != c.want {
			t.Errorf("%s: message %q, want %q", c.name, r.Message, c.want)
		}
		wantTS := c.wantUnixTS
		if wantTS == 0 {
			wantTS = received.Unix()
		}
		if r.Datetime != wantTS {
			t.Errorf("%s: datetime %d, want %d", c.name, r.Datetime, wantTS)
		}
	}
}

func TestAddressRules(t *testing.T) {
	plain := newSink(t, "http://127.0.0.1:1", config.PagermonConfig{})
	withFn := newSink(t, "http://127.0.0.1:1", config.PagermonConfig{SendFunctionCode: true})
	if r, ok := plain.Build(pocsagMsg("501", 3, message.KindAlpha, "x")); !ok || r.Address != "0000501" {
		t.Errorf("plain: %+v %v", r, ok)
	}
	if r, ok := withFn.Build(pocsagMsg("501", 3, message.KindAlpha, "x")); !ok || r.Address != "0005013" {
		t.Errorf("with function: %+v %v", r, ok)
	}
	if r, ok := withFn.Build(pocsagMsg("1234567", 0, message.KindAlpha, "x")); !ok || r.Address != "12345670" {
		t.Errorf("8 digits: %+v %v", r, ok)
	}
	// reader.js: address.length > 2 is checked before padding.
	if _, ok := plain.Build(pocsagMsg("12", 3, message.KindAlpha, "x")); ok {
		t.Error("two-digit address must not be sent")
	}
	if r, ok := withFn.Build(pocsagMsg("12", 3, message.KindAlpha, "x")); !ok || r.Address != "0000123" {
		t.Errorf("two digits plus function: %+v %v", r, ok)
	}
	off := false
	noTS := newSink(t, "http://127.0.0.1:1", config.PagermonConfig{UseTimestamp: &off})
	if r, _ := noTS.Build(pocsagMsg("501", 3, message.KindAlpha, "17 November 2026 20:14:00 Alarm")); r.Message != "17 November 2026 20:14:00 Alarm" || r.Datetime != received.Unix() {
		t.Errorf("use_timestamp off: %+v", r)
	}
}

func TestFLEXAndOthers(t *testing.T) {
	s := newSink(t, "http://127.0.0.1:1", config.PagermonConfig{ToneText: "[tone]"})
	flexMsg := func(kind message.Kind, frag, text string) *message.Message {
		return &message.Message{ReceivedAt: received, Protocol: "flex", Channel: message.Channel{Name: "paging"},
			Address: "1234567", Function: -1, Kind: kind, Text: text, Payload: &message.FLEXPayload{Fragment: frag}}
	}
	if r, ok := s.Build(flexMsg(message.KindAlpha, "complete", " Hello\x00junk ")); !ok || r.Address != "001234567" || r.Message != "Hello" {
		t.Errorf("flex alpha: %+v %v", r, ok)
	}
	if r, ok := s.Build(flexMsg(message.KindNumeric, "reassembled", "12345")); !ok || r.Message != "12345" {
		t.Errorf("flex numeric: %+v %v", r, ok)
	}
	if _, ok := s.Build(flexMsg(message.KindTone, "complete", "")); ok {
		t.Error("flex tone must not be sent")
	}
	if _, ok := s.Build(flexMsg(message.KindAlpha, "fragment_start", "part")); ok {
		t.Error("flex fragment must not be sent")
	}
	if _, ok := s.Build(&message.Message{ReceivedAt: received, Protocol: "fms", Address: "123456789", Kind: message.KindStatus, Text: "x"}); ok {
		t.Error("protocols unknown to the stock client must not be sent")
	}
	if r, ok := s.Build(pocsagMsg("777", 1, message.KindTone, "")); !ok || r.Message != "[tone]" || r.Address != "0000777" {
		t.Errorf("tone placeholder: %+v %v", r, ok)
	}
}

func TestEAS(t *testing.T) {
	s := newSink(t, "http://127.0.0.1:1", config.PagermonConfig{})
	rx := time.Date(2026, 3, 10, 18, 0, 0, 0, time.UTC)
	m := &message.Message{ReceivedAt: rx, Protocol: "eas", Channel: message.Channel{Name: "wx"}, Address: "KOAX/NWS", Function: -1, Kind: message.KindAlert,
		Payload: &message.EASPayload{Kind: message.EASHeader, Valid: true, Originator: "WXR", EventCode: "TOR",
			Locations: []string{"031109", "031055"}, Purge: "0030", PurgeMinutes: 30, Issued: "0691745",
			IssuedDay: 69, IssuedHour: 17, IssuedMinute: 45, Callsign: "KOAX/NWS"}}
	r, ok := s.Build(m)
	if !ok || r.Address != "KOAX-WXR-W" {
		t.Fatalf("eas: %+v %v", r, ok)
	}
	want := "Tornado Warning (TOR) issued by National Weather Service for 031109, 031055 valid until 2026-03-10 18:15 UTC (KOAX/NWS)"
	if r.Message != want {
		t.Errorf("eas message\n got %q\nwant %q", r.Message, want)
	}
	if easClass("SVA") != "A" || easClass("RWT") != "T" || easClass("EVI") != "" || easClass("XYZ") != "" {
		t.Errorf("classes: %q %q %q %q", easClass("SVA"), easClass("RWT"), easClass("EVI"), easClass("XYZ"))
	}
	eom := *m
	eom.Payload = &message.EASPayload{Kind: message.EASEOM, Header: "NNNN"}
	if _, ok := s.Build(&eom); ok {
		t.Error("end of message must not be sent")
	}
}

func TestOutcomes(t *testing.T) {
	cases := []struct {
		reply  string
		status int
		want   sink.Outcome
		class  sink.Class
	}{
		{"Ignoring duplicate", 200, sink.OutcomeDuplicate, ""},
		{"Ignoring filtered", 200, sink.OutcomeFiltered, ""},
		{`{"message":"Error - address or message missing"}`, 500, sink.OutcomeRejected, ""},
		{`{"error":"Authentication failed."}`, 401, "", sink.ClassAuth},
		{"forbidden", 403, "", sink.ClassAuth},
		{"down", 503, "", sink.ClassServer},
		{`{"code":"22021"}`, 500, "", sink.ClassServer},
	}
	for _, c := range cases {
		var got seen
		srv := server(t, c.reply, c.status, &got)
		s := newSink(t, srv.URL, config.PagermonConfig{})
		out, err := s.Deliver(context.Background(), pocsagMsg("501", 1, message.KindAlpha, "x"))
		srv.Close()
		if (err != nil) != (c.class != "") || out != c.want || (err != nil && sink.Classify(err) != c.class) {
			t.Errorf("reply %q/%d: out=%s err=%v", c.reply, c.status, out, err)
		}
	}
}

func TestUnreachable(t *testing.T) {
	s := newSink(t, "http://127.0.0.1:1", config.PagermonConfig{Timeout: time.Second})
	if _, err := s.Deliver(context.Background(), pocsagMsg("501", 1, message.KindAlpha, "x")); err == nil || sink.Classify(err) != sink.ClassNetwork {
		t.Errorf("expected network error, got %v", err)
	}
	if err := s.Check(context.Background()); err == nil {
		t.Error("expected check error")
	}
}

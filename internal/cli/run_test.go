//go:build cgo

package cli

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gopager/pagewire/internal/queue"
	"github.com/gopager/pagewire/internal/testsignal"
)

// TestRunReplaysRecordingToPagerMon runs the whole gateway on a synthetic
// POCSAG recording with a file audio source and checks what arrives at a
// fake PagerMon server.
func TestRunReplaysRecordingToPagerMon(t *testing.T) {
	dir := t.TempDir()
	bits, err := testsignal.EncodePOCSAG([]testsignal.POCSAGMessage{
		{Address: 1234567, Function: 3, Text: "HELLO GATEWAY"},
		{Address: 54321, Function: 0, Numeric: true, Text: "112-4711"},
		{Address: 777, Function: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	audio := testsignal.FSKAudio(bits, testsignal.POCSAGFSK(1200), 22050, 0.5, 0.5, 0.02, nil)
	pcm := make([]byte, 2*len(audio))
	for i, v := range audio {
		binary.LittleEndian.PutUint16(pcm[2*i:], uint16(int16(v*32767)))
	}
	rec := filepath.Join(dir, "rec.s16le")
	if err := os.WriteFile(rec, pcm, 0o644); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var posts []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			_, _ = w.Write([]byte("[]"))
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		m["apikey"] = r.Header.Get("apikey")
		mu.Lock()
		posts = append(posts, m)
		mu.Unlock()
		_, _ = w.Write([]byte("7"))
	}))
	defer srv.Close()
	var legacyPosts []map[string]any
	legacy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" {
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		mu.Lock()
		legacyPosts = append(legacyPosts, m)
		mu.Unlock()
		_, _ = w.Write([]byte(`{"id":7}`))
	}))
	defer legacy.Close()

	cfgPath := filepath.Join(dir, "gw.yaml")
	cfg := fmt.Sprintf(`
instance: itest
sources:
  - name: rec
    type: audio
    audio: { backend: file, format: s16le, rate: 22050, device: %q }
channels:
  - name: paging
    decoders:
      - { protocol: pocsag1200, pocsag: { mode: standard, prune_empty: false } }
targets:
  - name: pm
    type: pagermon
    pagermon: { url: %q, api_key: secret, source: "{instance}/{channel}", send_function_code: true, tone_text: "[tone]" }
  - name: legacy
    type: pagermon-ingest
    pagermon_ingest: { url: %q, api_key: secret, label: "pager" }
queue: { path: %q }
observability: { listen: "", metrics: false, log: { level: warn, format: text } }
`, rec, srv.URL, legacy.URL, filepath.Join(dir, "q.db"))
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Main([]string{"-c", cfgPath, "run"}, &stdout, &stderr); code != 0 {
		t.Fatalf("run exit %d\n%s", code, stderr.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(posts) != 3 {
		t.Fatalf("got %d posts: %+v\n%s", len(posts), posts, stderr.String())
	}
	// Addresses and texts arrive as the stock PagerMon client sends them:
	// function digit appended, padded to seven digits, padding of the last
	// codeword and trailing spaces removed.
	want := []struct{ addr, text string }{{"12345673", "HELLO GATEWAY"}, {"0543210", "112-4711"}, {"0007771", "[tone]"}}
	for i, w := range want {
		if posts[i]["address"] != w.addr || posts[i]["message"] != w.text || posts[i]["source"] != "itest/paging" || posts[i]["apikey"] != "secret" {
			t.Errorf("post %d = %+v, want %+v", i, posts[i], w)
		}
	}
	// The pagermon-ingest target gets the ingest-core shape: seven-digit
	// RIC and function digit, multimon-ng's printed text with its padding,
	// the tone page with an empty message, ingest-core's field set.
	if len(legacyPosts) != 3 {
		t.Fatalf("got %d legacy posts: %+v", len(legacyPosts), legacyPosts)
	}
	wantLegacy := []struct{ addr, format, prefix string }{{"12345673", "alpha", "HELLO GATEWAY"}, {"00543210", "numeric", "112-4711"}, {"00007771", "tone", ""}}
	for i, w := range wantLegacy {
		p := legacyPosts[i]
		text, _ := p["message"].(string)
		if p["address"] != w.addr || p["format"] != w.format || !strings.HasPrefix(text, w.prefix) || p["source"] != "pager" || p["protocol"] != "POCSAG1200" || p["timestamp"] == nil || p["time"] == nil || p["datetime"] != nil || len(p) != 7 {
			t.Errorf("legacy post %d = %+v, want %+v", i, p, w)
		}
	}
}

// TestTargetsAreIndependent runs the gateway with two PagerMon targets: one
// rejects the credentials permanently, the other must still receive every
// message without delay, and the blocked target's messages must stay
// queued (not dead-lettered) for when the credentials are fixed.
func TestTargetsAreIndependent(t *testing.T) {
	dir := t.TempDir()
	bits, err := testsignal.EncodePOCSAG([]testsignal.POCSAGMessage{
		{Address: 1000, Function: 3, Text: "ONE"},
		{Address: 1001, Function: 3, Text: "TWO"},
	})
	if err != nil {
		t.Fatal(err)
	}
	audio := testsignal.FSKAudio(bits, testsignal.POCSAGFSK(1200), 22050, 0.3, 0.3, 0.02, nil)
	pcm := make([]byte, 2*len(audio))
	for i, v := range audio {
		binary.LittleEndian.PutUint16(pcm[2*i:], uint16(int16(v*32767)))
	}
	rec := filepath.Join(dir, "rec.s16le")
	if err := os.WriteFile(rec, pcm, 0o644); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	good, bad := 0, 0
	goodSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			mu.Lock()
			good++
			mu.Unlock()
		}
		_, _ = w.Write([]byte("1"))
	}))
	defer goodSrv.Close()
	badSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			mu.Lock()
			bad++
			mu.Unlock()
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("Unauthorized"))
			return
		}
		_, _ = w.Write([]byte("[]"))
	}))
	defer badSrv.Close()
	queuePath := filepath.Join(dir, "q.db")
	cfgPath := filepath.Join(dir, "gw.yaml")
	cfg := fmt.Sprintf(`
instance: itest
sources:
  - { name: rec, type: audio, audio: { backend: file, format: s16le, rate: 22050, device: %q } }
channels:
  - { name: paging, decoders: [ { protocol: pocsag1200, pocsag: { mode: alpha } } ] }
targets:
  - { name: good, type: pagermon, pagermon: { url: %q, api_key: k } }
  - { name: bad,  type: pagermon, pagermon: { url: %q, api_key: wrong } }
queue: { path: %q, max_backoff: 100ms, drain_timeout: 2s }
observability: { listen: "", metrics: false, log: { level: error, format: text } }
`, rec, goodSrv.URL, badSrv.URL, queuePath)
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	start := time.Now()
	if code := Main([]string{"-c", cfgPath, "run"}, &stdout, &stderr); code != 0 {
		t.Fatalf("run exit %d\n%s", code, stderr.String())
	}
	elapsed := time.Since(start)
	mu.Lock()
	defer mu.Unlock()
	if good != 2 {
		t.Errorf("healthy target received %d messages, want 2", good)
	}
	if bad == 0 {
		t.Errorf("blocked target was never tried")
	}
	// The run ends after the drain timeout because "bad" never empties;
	// the healthy target must have been served long before that.
	if elapsed > 15*time.Second {
		t.Errorf("run took %s", elapsed)
	}
	q, err := queue.Open(queuePath, 100)
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	if p, d := q.Depth("bad"); p != 2 || d != 0 {
		t.Errorf("blocked target queue pending=%d dead=%d, want 2/0 (waits for valid credentials)", p, d)
	}
	if p, d := q.Depth("good"); p != 0 || d != 0 {
		t.Errorf("healthy target queue pending=%d dead=%d, want empty", p, d)
	}
}

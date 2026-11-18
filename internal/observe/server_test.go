package observe

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gopager/pagewire/internal/config"
)

func TestEndpoints(t *testing.T) {
	s := NewServer(config.ObservabilityConfig{Metrics: true, Pprof: true}, "test")
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	get := func(path string) (int, string) {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var sb strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := resp.Body.Read(buf)
			sb.Write(buf[:n])
			if err != nil {
				break
			}
		}
		return resp.StatusCode, sb.String()
	}
	if code, _ := get("/healthz"); code != 200 {
		t.Errorf("healthz = %d", code)
	}
	if code, body := get("/readyz"); code != 503 || !strings.Contains(body, "starting") {
		t.Errorf("readyz before ready = %d %q", code, body)
	}
	s.SetReady(true, "")
	if code, _ := get("/readyz"); code != 200 {
		t.Errorf("readyz after ready = %d", code)
	}
	if code, body := get("/metrics"); code != 200 || !strings.Contains(body, "pagewire_build_info") {
		t.Errorf("metrics = %d, build info missing", code)
	}
	if code, _ := get("/debug/pprof/"); code != 200 {
		t.Errorf("pprof = %d", code)
	}
}

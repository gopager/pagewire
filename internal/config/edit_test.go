package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetSourceRTLSDRKeepsComments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gw.yaml")
	in := `# head comment
instance: x
sources:
  - name: vhf
    type: rtlsdr
    rtlsdr:
      device: ""      # keep me
      gain: auto
      ppm: 0          # old value
channels: []
`
	if err := os.WriteFile(path, []byte(in), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetSourceRTLSDR(path, "vhf", "ppm", 12); err != nil {
		t.Fatal(err)
	}
	if err := SetSourceRTLSDR(path, "vhf", "gain", "28.0"); err != nil {
		t.Fatal(err)
	}
	if err := SetSourceRTLSDR(path, "vhf", "bias_tee", true); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(path)
	s := string(out)
	for _, want := range []string{"# head comment", "# keep me", "ppm: 12", "# old value", "gain: \"28.0\"", "bias_tee: true"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
	if err := SetSourceRTLSDR(path, "nope", "ppm", 1); err == nil {
		t.Error("unknown source must fail")
	}
}

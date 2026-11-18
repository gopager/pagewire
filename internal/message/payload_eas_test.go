package message

import (
	"reflect"
	"testing"
)

func TestParseSAME(t *testing.T) {
	p := ParseSAME("ZCZC-WXR-TOR-039173-039051+0030-1591829-KCLE/NWS-")
	want := &EASPayload{
		Kind: EASHeader, Header: "ZCZC-WXR-TOR-039173-039051+0030-1591829-KCLE/NWS-", Valid: true,
		Originator: "WXR", EventCode: "TOR", Locations: []string{"039173", "039051"},
		Purge: "0030", PurgeMinutes: 30, Issued: "1591829", IssuedDay: 159, IssuedHour: 18, IssuedMinute: 29,
		Callsign: "KCLE/NWS",
	}
	if !reflect.DeepEqual(p, want) {
		t.Errorf("got %+v\nwant %+v", p, want)
	}
}

func TestParseSAMEInvalid(t *testing.T) {
	for _, h := range []string{
		"", "ZCZC", "ZCZC-WXR-TOR-039173+0030-1591829-KCLE/NWS", // no final dash
		"ZCZC-WXR-TOR-03917+0030-1591829-KCLE/NWS-",  // short location
		"ZCZC-WXR-TOR-039173+0090-1591829-KCLE/NWS-", // minutes 90
		"ZCZC-WXR-TOR-039173+0030-1592529-KCLE/NWS-", // hour 25
		"ZCZC-WXR-TOR+0030-1591829-KCLE/NWS-",        // no location
	} {
		if p := ParseSAME(h); p.Valid || p.Header != h || p.Kind != EASHeader {
			t.Errorf("ParseSAME(%q) = %+v, want invalid with raw header kept", h, p)
		}
	}
}

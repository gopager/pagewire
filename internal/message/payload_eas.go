package message

import (
	"encoding/gob"
	"strconv"
	"strings"
)

func init() {
	gob.Register(&EASPayload{})
}

// EAS record kinds.
const (
	EASHeader = "header" // a SAME header (ZCZC-...)
	EASEOM    = "eom"    // the end-of-message marker (NNNN)
)

// EASPayload is the protocol record of an EAS/SAME transmission. Header
// holds the string exactly as received; the other fields are its fields as
// defined by the SAME protocol,
//
//	ZCZC-ORG-EEE-PSSCCC-PSSCCC+TTTT-JJJHHMM-LLLLLLLL-
//
// split without any interpretation of their meaning (codes are not mapped
// to names).
type EASPayload struct {
	// Kind is EASHeader or EASEOM.
	Kind string `json:"kind"`
	// Header is the complete header as received, or "NNNN" for EASEOM.
	Header string `json:"header"`
	// Valid is set when Header follows the SAME grammar completely; the
	// fields below are filled as far as the header could be split.
	Valid bool `json:"valid"`
	// Originator is ORG (3 characters, e.g. WXR, EAS, CIV, PEP).
	Originator string `json:"originator,omitempty"`
	// EventCode is EEE (3 characters, e.g. TOR, SVR).
	EventCode string `json:"event_code,omitempty"`
	// Locations are the PSSCCC location codes in transmission order.
	Locations []string `json:"locations,omitempty"`
	// Purge is TTTT, the valid time period as hhmm.
	Purge string `json:"purge,omitempty"`
	// PurgeMinutes is Purge in minutes.
	PurgeMinutes int `json:"purge_minutes,omitempty"`
	// Issued is JJJHHMM: ordinal day of the year and UTC time of issue.
	Issued string `json:"issued,omitempty"`
	// IssuedDay, IssuedHour and IssuedMinute are the parts of Issued.
	IssuedDay    int `json:"issued_day,omitempty"`
	IssuedHour   int `json:"issued_hour,omitempty"`
	IssuedMinute int `json:"issued_minute,omitempty"`
	// Callsign is LLLLLLLL, the identification of the sending station.
	Callsign string `json:"callsign,omitempty"`

	// Burst is the sequence number of the header burst whose arrival
	// completed the decoder's two-of-three vote.
	Burst int `json:"burst,omitempty"`
	// Votes is the number of stored copies equal to Header when the vote
	// was decided (2 or 3).
	Votes int `json:"votes,omitempty"`
	// Copies are the header copies the decoder holds from the last bursts,
	// in the order received and as received; copies of an earlier
	// transmission can be among them when fewer than three bursts of this
	// one have arrived. The decoder accepts the header at the second
	// agreeing copy, so the third burst is normally not part of the record.
	Copies []string `json:"copies,omitempty"`
	// AllCopiesAgree is set when three copies are present and identical.
	AllCopiesAgree bool `json:"all_copies_agree"`
}

// ParseSAME splits a SAME header into its fields. The result always carries
// Kind EASHeader and the raw Header; Valid tells whether the whole grammar
// matched (ORG and EEE of three upper-case letters or digits, one or more
// six-digit location codes, hhmm purge time with minutes below 60, JJJHHMM
// issue time, a non-empty callsign, and the terminating dash).
func ParseSAME(header string) *EASPayload {
	p := &EASPayload{Kind: EASHeader, Header: header}
	rest, ok := strings.CutPrefix(header, "ZCZC-")
	if !ok {
		return p
	}
	lead, tail, ok := strings.Cut(rest, "+")
	if !ok {
		return p
	}
	parts := strings.Split(lead, "-")
	if len(parts) >= 1 {
		p.Originator = parts[0]
	}
	if len(parts) >= 2 {
		p.EventCode = parts[1]
	}
	if len(parts) >= 3 {
		p.Locations = parts[2:]
	}
	// tail is TTTT-JJJHHMM-LLLLLLLL- ; the callsign may itself contain
	// dashes in practice, so everything between the issue time and the
	// final dash belongs to it.
	t := strings.SplitN(tail, "-", 3)
	if len(t) >= 1 {
		p.Purge = t[0]
	}
	if len(t) >= 2 {
		p.Issued = t[1]
	}
	if len(t) == 3 {
		p.Callsign = strings.TrimSuffix(t[2], "-")
	}
	valid := len(t) == 3 && strings.HasSuffix(t[2], "-") && p.Callsign != "" &&
		len(p.Originator) == 3 && len(p.EventCode) == 3 && len(p.Locations) > 0
	for _, l := range p.Locations {
		valid = valid && len(l) == 6 && allDigits(l)
	}
	if len(p.Purge) == 4 && allDigits(p.Purge) {
		h, _ := strconv.Atoi(p.Purge[:2])
		m, _ := strconv.Atoi(p.Purge[2:])
		p.PurgeMinutes = h*60 + m
		valid = valid && m < 60
	} else {
		valid = false
	}
	if len(p.Issued) == 7 && allDigits(p.Issued) {
		p.IssuedDay, _ = strconv.Atoi(p.Issued[:3])
		p.IssuedHour, _ = strconv.Atoi(p.Issued[3:5])
		p.IssuedMinute, _ = strconv.Atoi(p.Issued[5:])
		valid = valid && p.IssuedDay >= 1 && p.IssuedDay <= 366 && p.IssuedHour < 24 && p.IssuedMinute < 60
	} else {
		valid = false
	}
	p.Valid = valid
	return p
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

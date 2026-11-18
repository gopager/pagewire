package pagermon

import (
	"strings"
	"time"

	"github.com/gopager/pagewire/internal/message"
)

// EAS/SAME headers follow 47 CFR § 11.31:
// ZCZC-ORG-EEE-PSSCCC+TTTT-JJJHHMM-LLLLLLLL-. The stock PagerMon client turns
// them into an address "LLLL-ORG-<class>" (station call sign before the
// slash, originator, event class letter such as W for warnings) and a prose message produced by
// the jsame package (last released 2022, 3 stars, a port of dsame). The
// gateway keeps the address scheme, because alias tables on servers depend
// on it, and builds the message from the code tables of the regulation
// itself instead of porting jsame's sentence templates and its FIPS county
// list: event and originator names below are those of § 11.31(d) and (e);
// location codes are sent as PSSCCC codes.

// easOriginators is 47 CFR § 11.31(d).
var easOriginators = map[string]string{
	"EAS": "EAS Participant",
	"CIV": "Civil authorities",
	"WXR": "National Weather Service",
	"PEP": "United States Government",
}

// easEvents is 47 CFR § 11.31(e), national and state/local codes.
var easEvents = map[string]string{
	"EAN": "Emergency Action Notification",
	"NIC": "National Information Center",
	"NPT": "National Periodic Test",
	"RMT": "Required Monthly Test",
	"RWT": "Required Weekly Test",
	"ADR": "Administrative Message",
	"AVW": "Avalanche Warning",
	"AVA": "Avalanche Watch",
	"BZW": "Blizzard Warning",
	"BLU": "Blue Alert",
	"CAE": "Child Abduction Emergency",
	"CDW": "Civil Danger Warning",
	"CEM": "Civil Emergency Message",
	"CFW": "Coastal Flood Warning",
	"CFA": "Coastal Flood Watch",
	"DSW": "Dust Storm Warning",
	"EQW": "Earthquake Warning",
	"EVI": "Evacuation Immediate",
	"EWW": "Extreme Wind Warning",
	"FRW": "Fire Warning",
	"FFW": "Flash Flood Warning",
	"FFA": "Flash Flood Watch",
	"FFS": "Flash Flood Statement",
	"FLW": "Flood Warning",
	"FLA": "Flood Watch",
	"FLS": "Flood Statement",
	"HMW": "Hazardous Materials Warning",
	"HWW": "High Wind Warning",
	"HWA": "High Wind Watch",
	"HUW": "Hurricane Warning",
	"HUA": "Hurricane Watch",
	"HLS": "Hurricane Statement",
	"LEW": "Law Enforcement Warning",
	"LAE": "Local Area Emergency",
	"MEP": "Missing and Endangered Persons",
	"NMN": "Network Message Notification",
	"TOE": "911 Telephone Outage Emergency",
	"NUW": "Nuclear Power Plant Warning",
	"DMO": "Practice/Demo Warning",
	"RHW": "Radiological Hazard Warning",
	"SVR": "Severe Thunderstorm Warning",
	"SVA": "Severe Thunderstorm Watch",
	"SVS": "Severe Weather Statement",
	"SPW": "Shelter in Place Warning",
	"SMW": "Special Marine Warning",
	"SPS": "Special Weather Statement",
	"SSA": "Storm Surge Watch",
	"SSW": "Storm Surge Warning",
	"TOR": "Tornado Warning",
	"TOA": "Tornado Watch",
	"TRW": "Tropical Storm Warning",
	"TRA": "Tropical Storm Watch",
	"TSW": "Tsunami Warning",
	"TSA": "Tsunami Watch",
	"VOW": "Volcano Warning",
	"WSW": "Winter Storm Warning",
	"WSA": "Winter Storm Watch",
}

// easClasses maps the last word of an event name in § 11.31(e) to the class
// letter PagerMon aliases use (W warning, A watch, S statement, E emergency,
// T test, M message, N notification).
var easClasses = map[string]string{
	"Warning": "W", "Watch": "A", "Statement": "S", "Emergency": "E", "Test": "T", "Message": "M", "Notification": "N",
}

// easClass derives the class letter of an event code from its name in the
// regulation; codes without a class word (e.g. EVI, BLU, MEP) get none.
func easClass(code string) string {
	name, ok := easEvents[code]
	if !ok {
		return ""
	}
	words := strings.Fields(name)
	return easClasses[words[len(words)-1]]
}

// eas maps a SAME header to the PagerMon address and message. End-of-message
// markers and headers that do not follow the grammar are not sent.
func eas(msg *message.Message, received time.Time) (address, text string) {
	p, ok := msg.Payload.(*message.EASPayload)
	if !ok || !p.Valid || p.Kind != message.EASHeader {
		return "", ""
	}
	station, _, _ := strings.Cut(p.Callsign, "/")
	address = station + "-" + p.Originator
	if class := easClass(p.EventCode); class != "" {
		address += "-" + class
	}

	var b strings.Builder
	if name, ok := easEvents[p.EventCode]; ok {
		b.WriteString(name)
	} else {
		b.WriteString("Unknown event")
	}
	b.WriteString(" (" + p.EventCode + ") issued by ")
	if name, ok := easOriginators[p.Originator]; ok {
		b.WriteString(name)
	} else {
		b.WriteString(p.Originator)
	}
	b.WriteString(" for ")
	b.WriteString(strings.Join(p.Locations, ", "))
	if until, ok := easValidUntil(p, received); ok {
		b.WriteString(" valid until " + until.Format("2006-01-02 15:04 MST"))
	}
	b.WriteString(" (" + p.Callsign + ")")
	return address, b.String()
}

// easValidUntil computes the end of the valid period from JJJHHMM (UTC, the
// year taken from the reception time) and TTTT.
func easValidUntil(p *message.EASPayload, received time.Time) (time.Time, bool) {
	if p.IssuedDay < 1 || p.IssuedDay > 366 || p.PurgeMinutes <= 0 {
		return time.Time{}, false
	}
	year := received.UTC().Year()
	issued := time.Date(year, 1, 1, p.IssuedHour, p.IssuedMinute, 0, 0, time.UTC).AddDate(0, 0, p.IssuedDay-1)
	// A header received in the first days of January may carry a day late in
	// the previous year.
	if issued.Sub(received) > 180*24*time.Hour {
		issued = issued.AddDate(-1, 0, 0)
	}
	return issued.Add(time.Duration(p.PurgeMinutes) * time.Minute).In(received.Location()), true
}

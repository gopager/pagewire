package pagermon

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gopager/pagewire/internal/decoder/mmng"
	"github.com/gopager/pagewire/internal/message"
)

// This file reproduces what the stock PagerMon client (pagermon/pagermon
// client/reader.js, 2017-06-04, unchanged since) sends to the server for
// the protocols it understands, so that a server fed by the gateway holds
// the same addresses and texts as one fed by rtl_fm | multimon-ng |
// reader.js. reader.js parses multimon-ng's printed lines; here the same
// rules are applied to the decoded fields directly. Its decisions are kept
// even where they are questionable, because alias tables, filters and
// stored history on the server depend on them:
//
//   - POCSAG: the address gets the function digit appended when
//     sendFunctionCode is on and is then left-padded with zeros to seven
//     digits. The text is multimon-ng's printed form (control characters as
//     "<NUL>"-style tokens), trimmed; a leading timestamp becomes the
//     message time (useTimestamp); then every three-letter token is
//     removed, Ä becomes [ and Ü becomes ] (undoing the German charset for
//     those two letters only), and alphanumeric text is trimmed again.
//     Pages whose address has fewer than three digits, tone-only pages and
//     pages whose text is empty are not sent.
//   - FLEX: the address is the nine-digit zero-padded capcode; the text is
//     taken up to the first NUL (multimon-ng printed a C string) and
//     trimmed; only alphanumeric and numeric pages are sent, tone pages
//     and fragments are not (fragments are reassembled by the decoder, so
//     the result equals reader.js joining an F and a C fragment).
//   - EAS: see eas.go.
//   - Every other protocol is unknown to reader.js and is not sent.

// jsWhitespace is the set String.prototype.trim removes (ECMAScript
// WhiteSpace and LineTerminator).
func jsWhitespace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xA0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return r >= 0x2000 && r <= 0x200A
}

// jsTrim is String.prototype.trim.
func jsTrim(s string) string { return strings.TrimFunc(s, jsWhitespace) }

// padDigits is reader.js' padDigits(number, 7).
func padDigits(s string, digits int) string {
	if len(s) >= digits {
		return s
	}
	return strings.Repeat("0", digits-len(s)) + s
}

var (
	// tokenRe removes multimon-ng's three-letter control tokens such as
	// <NUL> and <EOT>; two-letter ones (<LF>, <CR>, <EM>, …) stay, as in
	// reader.js.
	tokenRe = regexp.MustCompile(`<[A-Za-z]{3}>`)
	// Timestamp detection and extraction use different expressions in
	// reader.js; both are kept.
	longGateRe    = regexp.MustCompile(`\d{2} \w+ \d{4} \d{2}:\d{2}:\d{2}`)
	longExtractRe = regexp.MustCompile(`\d+ \w+ \d+ \d{2}:\d{2}:\d{2}`)
	isoRe         = regexp.MustCompile(`\d+-\d+-\d+ \d{2}:\d{2}:\d{2}`)
)

// stripTokens is the common tail of reader.js' trimMessage for POCSAG:
// remove tokens, revert Ä and Ü.
func stripTokens(s string) string {
	s = tokenRe.ReplaceAllString(s, "")
	return strings.NewReplacer("Ä", "[", "Ü", "]").Replace(s)
}

// replaceFirst is String.prototype.replace with a non-global RegExp.
func replaceFirst(re *regexp.Regexp, s string) string {
	loc := re.FindStringIndex(s)
	if loc == nil {
		return s
	}
	return s[:loc[0]] + s[loc[1]:]
}

// embeddedTimestamp applies reader.js' useTimestamp handling to an
// alphanumeric text: a timestamp in one of the two recognised shapes is
// parsed in local time and removed from the text.
func embeddedTimestamp(text string, now time.Time) (string, time.Time) {
	switch {
	case longGateRe.MatchString(text):
		if t, ok := parseMomentLong(longExtractRe.FindString(text), now.Location()); ok {
			return replaceFirst(longGateRe, text), t
		}
	case isoRe.MatchString(text):
		if t, ok := parseMomentISO(isoRe.FindString(text), now.Location()); ok {
			return replaceFirst(isoRe, text), t
		}
	}
	return text, now
}

// momentMonths lists moment's English month names, long and short, longest
// first so that the alternation prefers the full name.
var momentMonths = func() *regexp.Regexp {
	names := []string{
		"january", "february", "march", "april", "june", "july", "august", "september", "october", "november", "december",
		"jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec",
	}
	return regexp.MustCompile(`(?i)` + strings.Join(names, "|"))
}()

// monthNumber maps a matched month name to 1–12.
func monthNumber(name string) int {
	switch strings.ToLower(name[:3]) {
	case "jan":
		return 1
	case "feb":
		return 2
	case "mar":
		return 3
	case "apr":
		return 4
	case "may":
		return 5
	case "jun":
		return 6
	case "jul":
		return 7
	case "aug":
		return 8
	case "sep":
		return 9
	case "oct":
		return 10
	case "nov":
		return 11
	case "dec":
		return 12
	}
	return 0
}

// parseMomentLong parses s like moment(s, "DD MMMM YYYY HH:mm:ss") in
// non-strict mode: each token is searched in the remaining input (text in
// between is skipped), and the result is valid only when every field is in
// range.
func parseMomentLong(s string, loc *time.Location) (time.Time, bool) {
	rest := s
	next := func(re *regexp.Regexp) (string, bool) {
		m := re.FindStringIndex(rest)
		if m == nil {
			return "", false
		}
		v := rest[m[0]:m[1]]
		rest = rest[m[1]:]
		return v, true
	}
	oneOrTwo := regexp.MustCompile(`\d\d?`)
	space := regexp.MustCompile(` `)
	colon := regexp.MustCompile(`:`)
	fields := make([]string, 0, 6)
	for _, re := range []*regexp.Regexp{oneOrTwo, space, momentMonths, space, regexp.MustCompile(`\d{1,4}`), space, oneOrTwo, colon, oneOrTwo, colon, oneOrTwo} {
		v, ok := next(re)
		if !ok {
			return time.Time{}, false
		}
		if re != space && re != colon {
			fields = append(fields, v)
		}
	}
	day, _ := strconv.Atoi(fields[0])
	month := monthNumber(fields[1])
	year, _ := strconv.Atoi(fields[2])
	hour, _ := strconv.Atoi(fields[3])
	minute, _ := strconv.Atoi(fields[4])
	second, _ := strconv.Atoi(fields[5])
	return civilTime(year, month, day, hour, minute, second, loc)
}

// parseMomentISO parses s like moment(s) for the shape reader.js extracts:
// moment's ISO 8601 parser takes YYYY-MM-DD HH:mm:ss, and the Date
// fallback also accepts one-digit month and day.
func parseMomentISO(s string, loc *time.Location) (time.Time, bool) {
	m := regexp.MustCompile(`^(\d{4})-(\d{1,2})-(\d{1,2}) (\d{2}):(\d{2}):(\d{2})$`).FindStringSubmatch(s)
	if m == nil {
		return time.Time{}, false
	}
	n := make([]int, 6)
	for i := range n {
		n[i], _ = strconv.Atoi(m[i+1])
	}
	return civilTime(n[0], n[1], n[2], n[3], n[4], n[5], loc)
}

// civilTime builds a local time and rejects out-of-range fields the way
// moment's overflow check does.
func civilTime(year, month, day, hour, minute, second int, loc *time.Location) (time.Time, bool) {
	if month < 1 || month > 12 || day < 1 || hour > 23 || minute > 59 || second > 59 {
		return time.Time{}, false
	}
	t := time.Date(year, time.Month(month), day, hour, minute, second, 0, loc)
	if t.Day() != day {
		return time.Time{}, false
	}
	return t, true
}

// request is the body of PagerMon's POST /api/messages (server/routes/api.js):
// address and message must be non-empty, datetime is Unix seconds (the
// server stores 1 when it is missing), source is free text (default "UNK").
type request struct {
	Address  string `json:"address"`
	Message  string `json:"message"`
	Datetime int64  `json:"datetime"`
	Source   string `json:"source"`
}

// Build maps a decoded message to the request body, or returns ok=false
// when reader.js would not have sent it (or the server would reject it with
// its "address or message missing" error).
func (s *Sink) Build(msg *message.Message) (request, bool) {
	var address, text string
	datetime := msg.ReceivedAt
	switch {
	case strings.HasPrefix(msg.Protocol, "pocsag"):
		address, text, datetime = s.pocsag(msg)
	case msg.Protocol == "flex":
		address, text = flex(msg)
	case msg.Protocol == "eas":
		address, text = eas(msg, msg.ReceivedAt)
	default:
		return request{}, false
	}
	// reader.js: if (address.length > 2 && message); the server additionally
	// rejects an empty message after reader.js' trimming.
	if len(address) <= 2 || text == "" {
		return request{}, false
	}
	// The server interpolates the address into SQL (… LIKE address) without
	// quoting, so a quote would break the request; addresses are digits or
	// EAS code letters, so this is only a guard.
	if strings.ContainsAny(address, `"'`) {
		return request{}, false
	}
	return request{
		Address:  padDigits(address, 7),
		Message:  text,
		Datetime: datetime.Unix(),
		Source:   s.source(msg),
	}, true
}

// pocsag applies the POCSAG branch of reader.js.
func (s *Sink) pocsag(msg *message.Message) (address, text string, datetime time.Time) {
	address = msg.Address
	if s.cfg.SendFunctionCode && msg.Function >= 0 {
		address += strconv.Itoa(msg.Function)
	}
	datetime = msg.ReceivedAt
	switch msg.Kind {
	case message.KindNumeric:
		// Numeric: message.trim(), then token removal without a final trim.
		text = stripTokens(jsTrim(mmng.PrintedText(msg.Text)))
	case message.KindTone:
		if s.cfg.ToneText == "" {
			return address, "", datetime
		}
		text = jsTrim(stripTokens(jsTrim(s.cfg.ToneText)))
	default:
		printed := jsTrim(mmng.PrintedText(msg.Text))
		if printed == "" {
			return address, "", datetime
		}
		if s.cfg.UseTimestampEnabled() {
			printed, datetime = embeddedTimestamp(printed, msg.ReceivedAt)
		}
		text = jsTrim(stripTokens(printed))
	}
	return address, text, datetime
}

// flex applies the FLEX branch of reader.js to a page from the embedded
// FLEX decoder.
func flex(msg *message.Message) (address, text string) {
	p, _ := msg.Payload.(*message.FLEXPayload)
	if msg.Kind != message.KindAlpha && msg.Kind != message.KindNumeric {
		return "", ""
	}
	if p != nil && p.Fragment != "" && p.Fragment != "complete" && p.Fragment != "reassembled" {
		return "", ""
	}
	capcode, err := strconv.ParseInt(msg.Address, 10, 64)
	if err != nil {
		return "", ""
	}
	address = fmt.Sprintf("%09d", capcode)
	text = msg.Text
	if i := strings.IndexByte(text, 0); i >= 0 {
		text = text[:i]
	}
	return address, jsTrim(text)
}

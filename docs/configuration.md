# Configuration reference

`pagewire config example` prints a complete, commented configuration;
`config validate` checks a file. Values of the form `${VAR}` or
`${VAR:-default}` are expanded from the environment, and any scalar can be
overridden with `PAGEWIRE__SECTION__KEY` (list elements by index:
`PAGEWIRE__SOURCES__0__RTLSDR__PPM=12`). Secrets can be read from files via the
`*_file` variants.

## Top level

| key | meaning |
|---|---|
| `instance` | Name of this process; must differ between instances on one host. Appears in logs, metrics and the `{instance}` placeholder. Default `default`. |
| `sources` | List of sample sources (`rtlsdr`, `rtltcp`, `audio`). |
| `channels` | Receive frequencies with their decoders; each refers to a source. |
| `targets` | Delivery targets, each with a type-specific block (`pagermon` or `pagermon_ingest`, the compatibility sinks for PagerMon servers and pagermon-ingest consumers). |
| `queue` | Persistent delivery queue. |
| `observability` | Metrics, health endpoints, logging. |

## sources[]

`name`, `type`. For `rtlsdr`: `device` (`""` only dongle, `serial:<s>`,
`usb:<bus>-<port.port…>`, `index:<n>`), `sample_rate` (0 = lowest rate that
covers the channels), `gain` (`auto` or dB), `ppm`, `bias_tee`,
`bandwidth` (Hz, 0 = automatic), `if_gains` (E4000 only). For `rtltcp`:
`address`, `sample_rate`, `gain`, `ppm`. For `audio`: `backend` (`pulse`,
`stdin`, `file`, `tcp`, `udp`), `format` (`s16le`, `f32le`), `rate`,
`device`; an audio source has exactly one channel.

## channels[]

`name`, `source` (optional with one source), `frequency` (Hz with k/M/G
suffix), `squelch` (`auto`, or dB of SNR), `power_gate` (skip decoding
while idle, default true), `decoders[]` with `protocol` and options:

- `pocsag512|pocsag1200|pocsag2400`: `pocsag.mode` (`alpha` default,
  `numeric`, `skyper`, `standard`, `auto`), `charset` (US, FR, DE, DK, SE,
  SI), `bch_level` (0–2), `polarity` (auto, normal, inverted),
  `show_partial`, `prune_empty`, `heuristics`.
- `flex`: `flex.show_partial`, `include_system`, `expand_groups`.

## targets[]

`name`, `type` and one block named after the type. Every message goes to
every target; the gateway does not filter, servers do:

- `type: pagermon` with `pagermon`: `url`, `api_key` or `api_key_file`,
  `source` (template with `{channel}`, `{frequency}`, `{protocol}`,
  `{hostname}`, `{instance}`), `send_function_code` (default false),
  `use_timestamp` (default true), `tone_text`, `timeout`,
  `insecure_skip_verify`. Sends what the stock PagerMon client sends.
- `type: pagermon-ingest` with `pagermon_ingest`: `url`, `api_key` or
  `api_key_file`, `label` (default `pagermon-ingest`), `timeout`,
  `insecure_skip_verify`. Sends what the retired pagermon-ingest-core
  service with its multimon adapter sent.

### The PagerMon target

The sink speaks the server API of pagermon/pagermon (`server/routes/api.js`):
`POST {url}/api/messages` with the header `apikey` and a JSON body of exactly
`address`, `message`, `datetime` (Unix seconds) and `source`. Server
behaviour the sink relies on: both `address` and `message` must be
non-empty (the server answers 500 "address or message missing", which the
sink treats as a final rejection); a missing `datetime` is stored as 1 and a
missing `source` as `UNK`; duplicates (same address and message within
`duplicateTime`/`duplicateLimit`) and filtered aliases are answered with
200 and the text `Ignoring duplicate` or `Ignoring filtered`; aliases are
matched with SQL `LIKE` against the address string; unknown body fields are
ignored. Health checks use `GET /api/messages?limit=1` with the API key, so
they work with `apiSecurity` on or off.

What is sent is what the stock PagerMon client (`client/reader.js`)
produces from multimon-ng's output, so that aliases, filters and history on
a server fed by the stock chain stay valid:

| Protocol | Address | Message |
|---|---|---|
| POCSAG | RIC, plus the function digit when `send_function_code` is on, then left-padded with zeros to 7 digits. Addresses with fewer than three digits (before padding) are not sent. | multimon-ng's printed text (control characters as `<NUL>`-style tokens), trimmed; with `use_timestamp` a leading `17 November 2026 20:14:00` or `2026-11-17 20:14:00` becomes `datetime` and is removed; then all three-letter tokens (`<NUL>`, `<EOT>`, `<ETX>`, `<ESC>`, `<DEL>`, …) are removed while two-letter ones (`<LF>`, `<CR>`, `<EM>`, …) stay, `Ä` becomes `[` and `Ü` becomes `]` (reader.js undoes the German charset for these two letters only), and alphanumeric text is trimmed again. Tone-only pages are dropped unless `tone_text` is set; pages that end up empty are dropped. |
| FLEX | Capcode as 9 digits, zero-padded. | Text up to the first NUL (multimon-ng printed a C string), trimmed. Alphanumeric and numeric pages only; tone pages and partial fragments are not sent. The decoder reassembles fragments, which equals reader.js joining the F and C parts. |
| EAS/SAME | `<station>-<ORG>-<class>`: call sign before the slash, originator, and the class letter of the event (W warning, A watch, S statement, E emergency, T test, M message, N notification) derived from the event name in 47 CFR § 11.31(e); events without such a word (EVI, BLU, MEP, …) get no suffix. | `<Event name> (<EEE>) issued by <Originator name> for <PSSCCC, …> valid until <end> (<LLLLLLLL>)`, names from § 11.31(d)/(e). The stock client used the jsame package (a dsame port, last released 2022) for a sentence with county names; the gateway uses the regulation's tables and sends location codes instead of county names. End-of-message markers are not sent. |
| others (FMS, ZVEI, MPT1327, GSC, DTMF, …) | not sent: the stock client does not know them. | |

Deviations from reader.js, all deliberate: the `use_timestamp` option can be
switched off (reader.js ignores its own setting because of a typo), the
timestamp parser accepts what moment.js accepts for the two shapes above
but not moment's fallbacks for malformed dates, and an orphaned FLEX
continuation is dropped instead of being sent with the text `undefined`
prepended.

### The pagermon-ingest target

For servers that were fed by `pagermon-ingest-adapter-multimon` →
`pagermon-ingest-core` (rtl_fm | multimon-ng `--json`, a Node.js parser, a
BullMQ queue and a poster). The sink reproduces that chain's output so the
server sees no change, including where the chain differed from the stock
client (one exception, the address, is noted in the table):

| | pagermon-ingest chain, reproduced |
|---|---|
| Protocols | POCSAG only. The adapter matched multimon-ng's `demod_name` against `POCSAG` and `FLEX`; FLEX_NEXT emitted no `demod_name` and the legacy FLEX decoder called itself `flex_alphanumeric`, so FLEX never passed. Everything else was unknown to it. |
| Address | RIC as seven digits followed by the function digit: `00005010` for RIC 501 function 0, `12345673` for RIC 1234567 function 3. This is the one deliberate deviation: the chain read the RIC from a JSON number and so lost its leading zeros (`5010`). |
| Message | multimon-ng's printed `alpha` or `numeric` text unchanged: control characters as `<NUL>`-style tokens, the padding of the last codeword included, nothing trimmed or substituted. Blank texts are not sent. Tone-only pages are sent with an empty message and `format: tone` (a stock server rejects them with 500; `prune_empty: true` on the decoder keeps them out like `-e` did in the chain). |
| Body | `POST {url}/api/messages`, header `apikey`, JSON with exactly `protocol` (multimon-ng demod name, e.g. `POCSAG1200`), `source` (the `label`), `format` (`alpha`, `numeric`, `tone`), `address`, `message`, `timestamp` (Unix seconds) and `time` (UTC ISO 8601 with microseconds as multimon-ng printed it). No `datetime`: a stock server stores 1. |
| Replies | Every 2xx body is parsed as JSON; one that is not (a stock server's `Ignoring duplicate`) is a retryable failure, as in ingest-core. 5xx and network errors are retried, 401/403 wait for a passing health check, other 4xx are final. |
| Health | `GET /api/health` must answer `{"status":"ok"}` within 5 s. Stock PagerMon has no such route; the forks this mode exists for do. |

Not reproducible: multimon-ng in mode `auto` printed a numeric and an
alphanumeric line for the same page and the chain sent both; the gateway's
decoder selects one rendering (`mode: alpha` corresponds to the chain's
`-f alpha`).

## queue

`path` (default `pagewire-<instance>.db`), `max_per_target`,
`max_attempts` (server errors per message), `max_backoff`, `auto_replay`,
`max_replays`, `drain_timeout`.

## observability

`listen` (must differ per instance), `metrics`, `pprof`, `log.level`,
`log.format`.

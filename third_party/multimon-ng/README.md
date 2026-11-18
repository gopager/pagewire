# multimon-ng decoders (vendored, modified)

Decoder sources of multimon-ng (https://github.com/EliasOenal/multimon-ng),
commit `10c0aeb68bc78a58b7019a45ab4e1bfee4fc9592` (2026-11-15), GPL-2.0-or-later
(see `COPYING`; `bch.c` is public domain). Only the demodulators and their
helpers are included, not the command line program, audio input or cJSON.

## Local modifications

The goal is to run the unmodified demodulation and decoding logic as a
library with per-instance options and structured output:

- `multimon.h`: `cJSON.h` include removed; `struct demod_state` gained
  `struct pagewire_hooks *pagewire` (callbacks and options of the instance).
- `pocsag.c`: the global option variables became macros that read the
  options of the current instance (`pagewire_current`); the charset table is
  per instance; `pocsag_printmessage()` no longer prints or builds JSON but
  hands address, function, raw nibbles and the numeric/alpha/skyper
  renderings to the `pocsag_message` callback. Output mode selection,
  partial-decode and empty-message pruning moved to the Go host.
- `demod_flex_next.c`: `flex_next_json_emit()`, the single funnel through
  which every page passes in `--json` mode, hands its parameters to the
  host as a native `struct pagewire_flex_message` and returns before building
  the JSON object; the glue sets `json_mode = 1` permanently so the text
  output paths are never taken. The seven `fprintf(stdout, …)` sites of the
  network bookkeeping records call `pagewire_flex_json_out()`, which discards
  them. cJSON (MIT) stays compiled because the call sites still build their
  `extra` objects.
- Control characters in POCSAG alphanumeric pages are passed as the raw
  bytes (the glue overrides the `<NUL>`-style entries of the charset table)
  so the host receives data, not a textual rendering of it.
- `demod_flex_next.c` `add_ch()`: stores every received 7-bit character
  instead of dropping non-printables and escaping tab/LF/CR; NUL is carried
  as 0x80 (impossible in 7-bit text) because the fragment store and the
  deduplication use C strings, and the host maps it back.
- `pocsag.c` `print_msg_alpha()`: reports the rendered length so NUL
  characters survive; rendering buffers enlarged to 2048 bytes.
- `demod_eas.c`: a no-op `deinit` (the glue calls it unconditionally); the printing/JSON output of `eas_frame()` became
  `pagewire_eas_emit_header()` / `pagewire_eas_emit_eom()`, which fill the native
  `struct pagewire_eas_message` (new header `pagewire_eas.h`: the accepted header,
  the vote count and the stored copies of the last bursts, or the `NNNN`
  marker) and call the `eas_message` hook. `multimon.h` `l2_state_eas`
  gained `burst` and `slot_burst[]` to number the header copies; decoding is
  unchanged.
- `fms.c`: the rendering that went to the log is collected in a buffer;
  `fms_disp_packet()` hands every telegram to the host as a native
  `struct pagewire_fms_message` (`pagewire_fms.h`, new) with all fields, the raw 48
  bits (repaired and as received) and the CRC result. The display of the
  uncorrected packet before a one-bit repair is only logged, so each
  telegram is delivered once. `demod_fmsfsk.c` gained an empty `deinit`
  because the host calls it unconditionally.
- `selcall.c`, `demod_dtmf.c`, `demod_dualtone.c`: the printed lines
  (`ZVEI1: 12345`, `DTMF: 5`, `DUALTONE: fs4,fs5`) became native
  `struct pagewire_selcall_message` records (`pagewire_selcall.h`, new) passed to
  the `selcall_message` callback. `selcall.c` collects the tones of a
  sequence (buffer added to `l1_state_selcall` in `multimon.h`) and emits
  it where upstream ended the line; `demod_dtmf.c` emits one record per key
  with the sample position (`pos` added to `l1_state_dtmf`), the host joins
  keys into sequences. `dtmf` and `dualtone` got an empty deinit because the
  glue calls it unconditionally.
- `demod_mpt1327.c`, `demod_gsc.c`: `mpt1327_print()` and `gsc_json_emit()`,
  the single funnels of their decoders, hand the complete record to the host
  as a native `struct pagewire_mpt1327_message` / `struct pagewire_gsc_message`
  (`pagewire_mpt1327.h`, `pagewire_gsc.h`) instead of building JSON; MPT1327 collects
  the fields the JSON object would carry (hex and bit strings as numbers),
  GSC additionally reports the BCH-corrected data blocks of the message
  (thread-local buffer filled in the two block loops).
- `pagewire_hooks.h`, `pagewire_glue.c` (new): instance management, sample carry
  buffer with multimon-ng's overlap semantics, `_verbprintf` routed to a log
  callback, counters accessor.

Rule for every further decoder (FMS, selcall, EAS, …): results cross into
Go as native C structs through a callback in `pagewire_hooks.h`, never as text
or JSON to be parsed.

Everything else is byte-identical to upstream. When updating, re-apply the
changes above (they are small and marked with `pagewire`).

Files: `multimon.h`, `pocsag.c`, `demod_poc5.c`, `demod_poc12.c`,
`demod_poc24.c`, `demod_flex_next.c`, `bch.c`, `bch.h`, `filter.h`,
`cJSON.c`, `cJSON.h`.

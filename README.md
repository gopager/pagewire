# pagewire

Single-binary SDR receiver, decoder and radio-to-network gateway for pager and
emergency-service radio protocols (POCSAG, FLEX, FMS, ZVEI, …). It replaces
the `rtl_fm | multimon-ng | client` process chain: the RTL-SDR driver, the
FM demodulation, the multimon-ng decoders, a persistent delivery queue,
multi-target delivery and Prometheus metrics live in one process that runs
in Docker or bare on anything from a Raspberry Pi 2 to a server.

Status: feature complete for the first release. Decoders (embedded
multimon-ng, verified against the multimon-ng binary): POCSAG 512/1200/2400,
FLEX, FMS, ZVEI 1/2/3, DZVEI, PZVEI, CCIR, EEA, EIA, DTMF, DUALTONE,
EAS/SAME, MPT1327, GSC. Plus SDR front end with automatic reconnect, audio
inputs, recording and offline decoding, persistent per-target queue,
delivery through sinks, calibration, metrics. Documentation:
`docs/`.

## Install

Releases ship ready-made binaries and images, so nothing has to be built:

- Linux: static binaries for amd64, arm64 and arm/v7 (no shared libraries
  needed) and multi-arch images at `ghcr.io/gopager/pagewire`.
- macOS: binaries for Apple silicon and Intel.
- Windows (amd64): `pagewire.exe`; install the WinUSB driver for the
  dongle once with Zadig, as for any RTL-SDR software.
- A manual page `pagewire.1`.

## Build from source

Requires Go 1.27 and a C compiler. The USB driver (libusb, librtlsdr) and
the decoders (multimon-ng) are compiled from the vendored sources in
`third_party/`; nothing has to be installed on the host.

    make build            # bin/pagewire for the host
    make test
    make lint
    make binaries         # static Linux binaries (amd64, arm64, arm/v7) into dist/
    make docker           # multi-arch images via docker buildx
    make man              # dist/pagewire.1

## Development

CI runs vet, race tests, lint, govulncheck and the full build matrix on
every push. Releases are cut by release-please from conventional commits
(`feat:`, `fix:`, …) on `main`: merging the release PR tags `vX.Y.Z` and the
release workflow attaches binaries, the man page and pushes images.
Dependabot keeps Go modules, actions and base images current; the vendored
C sources are updated by hand (see the READMEs in `third_party/`).

## Quick start

    pagewire devices --probe            # shows usb:<bus>-<port> and serial: selectors
    pagewire config example > pagewire.yaml
    pagewire config validate
    pagewire run

Several instances may share a host: give each its own `instance` name,
`observability.listen` port and (by default derived from the instance name)
queue file. Select dongles by `serial:` or by physical port `usb:1-1.3`;
enumeration indices change between boots.

## Message model

Every decoded page carries two layers. The raw protocol content
(`payload.raw`, with the exact bit count for POCSAG) is what was received:
every message codeword including the padding of the last one, so a server can
interpret or decrypt it later; the derivation rules are documented in
`internal/message/payloads.go`. The renderings (`numeric`, `alpha`, and
`text` as the one selected by the decoder mode) map the protocol's character
set to Unicode one to one: control characters and NUL stay, nothing is
trimmed or substituted. Sinks adapt to their target: the `pagermon` sink
sends exactly what the stock PagerMon client would have sent (addresses
padded, multimon-ng's control tokens removed, text trimmed), the
`pagermon-ingest` sink what the retired pagermon-ingest-core service sent
(seven-digit RIC plus function digit, multimon-ng's printed text untouched), so
a server keeps its aliases, filters and history either way; see
`docs/configuration.md`. Both are compatibility sinks for existing PagerMon
servers and pagermon-ingest consumers; the sink interface is what new
targets implement, and more sinks are planned.

## Delivery

The gateway only translates received messages into network deliveries;
filtering and evaluation happen on the receiving server. Every message is
written to a persistent queue (one FIFO per target in a bbolt file) before a worker
delivers them with exponential back-off. Nothing is lost while a server is
down or the gateway restarts. A `run` with only finite sources (recordings,
pipes) stops after the queues are drained, so a recording can be replayed
into a server.

    pagewire queue inspect           # depths and oldest entries (gateway stopped)
    pagewire queue replay <target>   # dead letters back into the queue
    pagewire queue purge <target> --yes

## Calibration

    pagewire calibrate --apply              # ppm from the channels' own traffic, gain sweep
    pagewire calibrate --ppm --gsm gsm900   # ppm against GSM base stations where 2G exists

See `docs/calibration.md`.

## Recording and offline decoding

    pagewire record --duration 60s --iq      # <source>.cu8 and <channel>.s16le
    multimon-ng -t raw -a POCSAG1200 paging.s16le       # cross-check the audio
    pagewire decode --iq vhf.cu8 --rate 1014300 --center 150050000 -f 150.000M -a pocsag1200

## License

GPL-3.0-or-later (`LICENSE`). The embedded C sources are GPL-2.0-or-later
(rtl-sdr, multimon-ng) and LGPL-2.1-or-later (libusb), several Go
dependencies are Apache-2.0; GPL-3.0 is the license compatible with all of
them. Component licenses are listed in `THIRD_PARTY_NOTICES.md`.

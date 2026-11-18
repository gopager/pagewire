# Protocol status

Decoders are the multimon-ng implementations compiled into the binary
(`third_party/multimon-ng`, see its README for the local modifications).

| protocol | status | notes |
|---|---|---|
| POCSAG 512/1200/2400 | done | all multimon-ng options; output verified bit-for-bit against the multimon-ng binary on real recordings |
| FLEX | done | FLEX_NEXT: 1600/3200/6400 bps, 2/4 level, fragment reassembly, group pages, system messages optional |
| FMS | done | multimon-ng FMSFSK (1200 baud AFSK); every field, the 48 raw telegram bits (also as received before a one-bit repair) and the CRC result are delivered as `FMSPayload`; address is `<service><state><location><vehicle>` in 8 hex digits |
| ZVEI 1/2/3, DZVEI, PZVEI, CCIR, EEA, EIA, DTMF, DUALTONE | planned (M5) | |
| EAS/SAME | done | protocol `eas`: SAME headers (kind alert, address `ORG-EEE`, text the raw header, parsed fields and the received burst copies in the payload) and optionally the `NNNN` end-of-message marker (`end_of_message: true`); a header is delivered when two of the three bursts agree |
| MPT1327, GSC | planned (M6) | |
| AFSK/FSK packet, Morse, X10 | not planned | |

Every page is delivered with its byte-exact raw content and all renderings;
see `internal/message/payloads.go`.

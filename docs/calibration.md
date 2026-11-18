# Calibration

RTL-SDR dongles have a cheap crystal; the error is a few to a few dozen ppm
and drifts with temperature. At 150 MHz one ppm is 150 Hz. The channel
filter is ±8 kHz wide and the decoders tolerate a few hundred Hz, so the
correction only has to be good to about one ppm.

    pagewire calibrate [--source NAME] [--ppm] [--gain] [--apply]

## Frequency correction (ppm)

Three references are available; the first works everywhere:

1. **The configured channels** (default). Every transmission the gateway
   hears is measured: the mean discriminator output while the signal is
   present is the carrier offset. The median over all bursts of a pass gives
   the correction; the correction is applied and the pass repeated (up to
   `--passes`, default 3) until the residual is under 1 kHz, because a
   larger offset lets the channel filter edge bias the estimate. Needs
   traffic on the channel; `--duration` sets the listening time per pass.
2. **A reference carrier** (`--reference-hz`): any unmodulated carrier of
   known frequency in reach, measured the same way.
3. **GSM base stations** (`--gsm gsm850|gsm900|egsm900|dcs1800|pcs1900`),
   the classic kalibrate-rtl method: the FCCH burst is a pure tone exactly
   67.7083 kHz above the carrier, and base stations are accurate to
   0.05 ppm. The band is scanned for the strongest carriers and their FCCH
   bursts are measured. Whether 2G is still on air depends on the country;
   PCS 1900 lies above the range of the R820T tuner.

Sign convention: a positive ppm raises the frequency
the dongle receives at, so a carrier seen below its nominal frequency needs
a larger ppm.

## Gain

The sweep tries every tuner gain step for `--step-time` and records the
noise floor, the share of clipped ADC samples and, when transmissions were
seen, their SNR. The recommended step is the best SNR without clipping; if
no transmission was seen, the knee of the noise-floor curve (where more
gain stops raising the floor) is taken as a conservative choice to verify
with traffic later. `auto` (tuner AGC) remains the default and is fine for
most installations.

## Applying

`--apply` writes `ppm` and `gain` under the source's `rtlsdr` block of the
configuration file, keeping comments. The carrier offset is also tracked at
run time (`pagewire_channel_freq_offset_hz`); a drift of more than about 500 Hz
is a hint to calibrate again.

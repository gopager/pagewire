/*
 * pagewire: record of the selective-call tone decoders (ZVEI1/2/3,
 * DZVEI, PZVEI, CCIR, EEA, EIA, DTMF, DUALTONE). Included by selcall.c,
 * demod_dtmf.c, demod_dualtone.c and the Go binding. GPL-2.0-or-later like
 * the rest.
 */
#ifndef PAGEWIRE_SELCALL_H
#define PAGEWIRE_SELCALL_H

#include <stdint.h>

/* Longest tone sequence in one record; a longer run is delivered in several
 * records without losing a tone. */
#define PAGEWIRE_SELCALL_MAXSEQ 64

/* One tone sequence as decoded. Pointers are valid only during the callback.
 *
 * ZVEI*, DZVEI, PZVEI, CCIR, EEA, EIA: one record per received sequence
 *   (emitted when the tone run ends). tones[] holds the tone indices 0..15
 *   in order, symbols[] the same as multimon-ng prints them (%1X: 0-9, A-F),
 *   freq_count is 0 (the frequency table belongs to the variant).
 * DTMF: one record per detected digit, count 1. tones[0] is the key index
 *   row*4+column, symbols[0] the key as multimon-ng prints it ("123A456B
 *   789C*0#D"), freq_hz[] the row and column frequency, sample_pos the
 *   number of samples the decoder had consumed when the key was detected.
 *   The host assembles keys into sequences.
 * DUALTONE: one record per trigger. tones[] holds the tone numbers {4, 5}
 *   or {4, 7}, symbols is "fs4,fs5" or "fs4,fs7" as multimon-ng prints it,
 *   freq_hz[] the nominal frequencies of the two tones. */
struct pagewire_selcall_message {
	const char *variant;  /* multimon-ng demodulator name, e.g. "ZVEI1" */
	uint32_t count;       /* number of tones in tones[] */
	uint8_t tones[PAGEWIRE_SELCALL_MAXSEQ];
	char symbols[PAGEWIRE_SELCALL_MAXSEQ + 1]; /* NUL terminated rendering */
	uint32_t symbols_len;
	uint32_t freq_count;  /* 0 or 2 */
	float freq_hz[2];
	uint64_t sample_pos;  /* DTMF only, else 0 */
};

#endif

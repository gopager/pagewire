/*
 * pagewire: native record of one GSC (Golay Sequential Code) page or
 * voice event, handed to the host by gsc_json_emit() in demod_gsc.c through
 * the gsc_message callback of struct pagewire_hooks.
 */
#ifndef PAGEWIRE_GSC_H
#define PAGEWIRE_GSC_H

#include <stdint.h>

/* All pointers are valid only during the callback. Texts may be NULL where
 * the decoder produced none; use the explicit lengths rather than strlen. */
struct pagewire_gsc_message {
	const char *address;       /* six address digits plus the function suffix digit */
	int function;              /* 1..4 as printed by multimon-ng */
	const char *msg_type;      /* "alpha", "numeric", "tone" or "voice" */
	const char *message;       /* text as decoded; NULL for tone and voice */
	unsigned int message_len;
	const char *alt_type;      /* other rendering of the same data blocks, or NULL */
	const char *alt_message;
	unsigned int alt_message_len;
	int score;                 /* content score of msg_type (valid with alt_type) */
	int alt_score;             /* content score of alt_type */
	int blocks;                /* data blocks, -1 when not applicable */
	int error_count;           /* BCH bit errors corrected so far in the stream, -1 n/a */
	int uncorrectable_count;   /* uncorrectable BCH words so far, -1 n/a */
	const char *voice_state;   /* "start" or "end" for voice events, else NULL */
	int polarity_inverted;
	/* Data blocks after BCH correction: 8 bytes per block (seven data words
	 * and the checksum word, 7 bits each; 0 for an uncorrectable word). */
	const uint8_t *raw;
	unsigned int raw_len;
};

#endif

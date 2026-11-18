/*
 * pagewire: record of the EAS/SAME decoder (demod_eas.c).
 *
 * Results cross into Go as this native struct; nothing is rendered to text.
 * It is its own header so that the decoder family can be worked on without
 * touching pagewire_hooks.h.
 */
#ifndef PAGEWIRE_EAS_H
#define PAGEWIRE_EAS_H

#include <stdint.h>

/* Longest header the decoder stores (MAX_MSG_LEN 268 plus NUL) preceded by
 * the four characters "ZCZC". */
#define PAGEWIRE_EAS_TEXT_MAX 273

enum pagewire_eas_kind {
	PAGEWIRE_EAS_HEADER = 0, /* a SAME header, accepted by the 2-of-3 vote */
	PAGEWIRE_EAS_EOM = 1     /* an end-of-message marker "NNNN" */
};

/* One received copy of a header, exactly as stored by the decoder. */
struct pagewire_eas_copy {
	uint32_t burst; /* sequence number of the burst, counted from 1 */
	uint32_t len;   /* length of text */
	char text[PAGEWIRE_EAS_TEXT_MAX]; /* "ZCZC" followed by the received bytes, NUL terminated */
};

/* One EAS record. Pointers and arrays are valid only during the callback.
 *
 * For PAGEWIRE_EAS_HEADER, header is the accepted header as received
 * ("ZCZC-ORG-EEE-PSSCCC+TTTT-JJJHHMM-LLLLLLLL-"; the decoder drops anything
 * after the last '-'). The decoder accepts a header as soon as two copies
 * agree, which is normally at the second of the three bursts: agree is the
 * number of stored copies equal to the header at that moment (2 or 3),
 * burst is the sequence number of the burst that completed the vote, and
 * copies lists the stored copies of the last bursts in the order received
 * (the newest last; fewer than 3 when fewer were received yet; copies of an
 * earlier alert can still be present). For PAGEWIRE_EAS_EOM, header is "NNNN"
 * and ncopies is 0. */
struct pagewire_eas_message {
	int kind;
	const char *header;
	uint32_t header_len;
	int agree;
	uint32_t burst;
	int ncopies;
	struct pagewire_eas_copy copies[3];
};

#endif

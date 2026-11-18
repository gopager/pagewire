/*
 * pagewire: native record of one MPT 1327 codeword, handed to the
 * host by mpt1327_print() in demod_mpt1327.c through the mpt1327_message
 * callback of struct pagewire_hooks.
 */
#ifndef PAGEWIRE_MPT1327_H
#define PAGEWIRE_MPT1327_H

#include <stdint.h>

#define PAGEWIRE_MPT1327_MAX_FIELDS 16

/* Pointers are valid only during the callback. The decoder classifies the
 * codeword (message) and extracts its fields exactly as it names them in its
 * JSON output; the fields follow in the decoder's order with numeric values
 * (what the decoder prints as hex or bit strings is carried as the number:
 * sys, info, params, cat, type, func). */
struct pagewire_mpt1327_message {
	const char *message;   /* CCSC, DCSC, DATA, GTC, ALH.., ACK.., AHY.., MARK, MAINT,
	                        * CLEAR, MOVE, BCAST or UNDECODED */
	uint64_t codeword;     /* the 64 bits after correction; bit 1 (sent first) is the MSB */
	int nfields;
	const char *field_name[PAGEWIRE_MPT1327_MAX_FIELDS];
	uint64_t field_value[PAGEWIRE_MPT1327_MAX_FIELDS];
};

#endif

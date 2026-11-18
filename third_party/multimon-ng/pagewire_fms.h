/*
 * pagewire: record of one received FMS (Funkmeldesystem) telegram,
 * handed to the host through pagewire_hooks.fms_message. Pointers are valid only
 * during the callback.
 *
 * A telegram is 48 bits after the sync pattern. Field values are extracted
 * exactly as fms.c does; the first transmitted bit is the least significant
 * bit of every field. Bit positions below count transmitted bits from 0.
 */
#ifndef PAGEWIRE_FMS_H
#define PAGEWIRE_FMS_H

#include <stdint.h>

struct pagewire_fms_message {
	int service_id;   /* BOS-Kennung, bits 0-3 */
	int state_id;     /* Landeskennung, bits 4-7 */
	int loc_id_raw;   /* Ortskennung as transmitted, bits 8-15 */
	int loc_id;       /* Ortskennung with the nibbles swapped (TR-BOS) */
	int vehicle_id;   /* Fahrzeugkennung, bits 16-31, first bit least significant */
	char vehicle[5];  /* the four hex digits in transmission order, as shown by multimon-ng */
	int status;       /* Status, bits 32-35 */
	int model;        /* Baustufenkennung, bit 36 */
	int direction;    /* Richtungskennung, bit 37: 0 FZG->LST, 1 LST->FZG */
	int short_info;   /* taktische Kurzinformation, bits 38-39 */
	int crc;          /* CRC field as transmitted, bits 40-46 (first bit least significant) */
	int crc_ok;       /* the 48 bits are consistent with the CRC (after correction, if any) */
	int corrected;    /* a single bit error was repaired; raw holds the repaired bits */
	/* The 48 bits in transmission order, packed most significant bit
	 * first. raw is what the decoder delivered (repaired when corrected is
	 * set), received is what came off the air. */
	uint8_t raw[6];
	uint8_t received[6];
	/* multimon-ng's human-readable rendering of the telegram (one line,
	 * without the trailing newline). */
	const char *text;
};

#endif

/*
 * pagewire glue for the embedded multimon-ng decoders.
 *
 * Every decoder instance carries a pagewire_hooks block with its options and
 * callbacks; the decoders find it through pagewire_current, which the glue sets
 * before calling into a decoder (one thread drives one decoder at a time).
 * Messages and diagnostics reach the host through callbacks instead of
 * stdout; counters are read from the decoder state after each block.
 */
#ifndef PAGEWIRE_HOOKS_H
#define PAGEWIRE_HOOKS_H

#include <stdint.h>

struct demod_state;

struct pagewire_pocsag_options {
	int invert_input;      /* invert the bit stream before decoding */
	int error_correction;  /* BCH correction limit 0..2 */
	int polarity;          /* 0 auto, 1 normal only, 2 inverted only */
	const char *trtab[128]; /* active charset translation table */
};

/* One received POCSAG page. Strings are valid only during the callback.
 * address/function are -1 when unknown and -2 when the page started
 * before sync (partial decode). */
struct pagewire_pocsag_message {
	int32_t address;
	int32_t function;
	int sync;
	const unsigned char *nibbles; /* raw 4-bit payload units, two per byte */
	uint32_t numnibbles;
	const char *numeric;
	/* alpha and skyper may contain NUL characters; use the lengths. */
	const char *alpha;
	unsigned int alpha_len;
	const char *skyper;
	unsigned int skyper_len;
	int guess_numeric;
	int guess_alpha;
	int guess_skyper;
};

/* One FLEX page or system message as assembled by FLEX_NEXT. Pointers are
 * valid only during the callback. msg_type follows FLEX: 0 secure,
 * 1 instruction, 2 short message, 3 numeric, 4 special numeric,
 * 5 alphanumeric, 6 binary, 7 numbered numeric, 8 tone only, -1 unknown. */
struct pagewire_flex_message {
	int64_t capcode;
	char addr_type;
	char phase;
	int baud;
	int levels;
	int cycle;
	int frame;
	int msg_type;
	const char *type_tag;   /* ALN, NUM, NNUM, TON, HEX, SMSG, ... */
	const char *fragment;   /* complete, reassembled, fragment_start, ... */
	int msg_n, msg_r, msg_m;
	int k_ok, sig_ok;
	int is_priority;
	int is_group;
	const char *message;    /* 7-bit characters as received; 0x80 stands for NUL */
	const int64_t *group_capcodes;
	int group_count;
	const char *flextime;   /* last network time string, may be empty */
};

/* Records of the other decoders are defined in their own headers so that
 * each decoder family can be worked on independently. */
struct pagewire_fms_message;
struct pagewire_selcall_message;
struct pagewire_eas_message;
struct pagewire_mpt1327_message;
struct pagewire_gsc_message;

struct pagewire_hooks {
	void *ctx;
	int verbosity; /* diagnostics with a level above this are not formatted */
	void (*log)(void *ctx, int level, const char *text);
	void (*pocsag_message)(void *ctx, const struct pagewire_pocsag_message *msg);
	void (*flex_message)(void *ctx, const struct pagewire_flex_message *msg);
	void (*fms_message)(void *ctx, const struct pagewire_fms_message *msg);
	void (*selcall_message)(void *ctx, const struct pagewire_selcall_message *msg);
	void (*eas_message)(void *ctx, const struct pagewire_eas_message *msg);
	void (*mpt1327_message)(void *ctx, const struct pagewire_mpt1327_message *msg);
	void (*gsc_message)(void *ctx, const struct pagewire_gsc_message *msg);
	struct pagewire_pocsag_options pocsag;
};

/* pagewire_flex_json_out receives the network bookkeeping records that
 * demod_flex_next.c would print in --json mode; the host ignores them. */
void pagewire_flex_json_out(const char *json);

extern _Thread_local struct demod_state *pagewire_current;

/* Opaque decoder instance: one multimon-ng demod_state plus its hooks and
 * sample carry buffer. */
struct pagewire_decoder;

/* pagewire_decoder_new allocates an instance for a multimon-ng demodulator name
 * such as "POCSAG1200"; it returns NULL for unknown names. Set the hooks,
 * then call pagewire_decoder_init before processing. */
struct pagewire_decoder *pagewire_decoder_new(const char *name);
struct pagewire_hooks *pagewire_decoder_hooks(struct pagewire_decoder *d);
unsigned pagewire_decoder_samplerate(struct pagewire_decoder *d);
void pagewire_decoder_init(struct pagewire_decoder *d);
/* pagewire_pocsag_set_charset selects US, FR, DE, DK, SE or SI and keeps
 * control characters as the raw bytes instead of "<NUL>" tokens; returns 0
 * on success. Call after pagewire_decoder_init. */
int pagewire_pocsag_set_charset(struct pagewire_decoder *d, const char *name);
/* pagewire_pocsag_default_token returns multimon-ng's printed form of a 7-bit
 * character in the default (US) table, e.g. "<NUL>" for 0. */
const char *pagewire_pocsag_default_token(unsigned ch);
void pagewire_decoder_process(struct pagewire_decoder *d, const float *samples, int n);
void pagewire_decoder_reset(struct pagewire_decoder *d);
void pagewire_decoder_free(struct pagewire_decoder *d);

/* POCSAG counters in order: words with errors, corrected, corrected 1 bit,
 * corrected 2 bits, uncorrectable, bits total, bits in sync, bits out of
 * sync, synced flag (0/1). */
#define PAGEWIRE_POCSAG_STATS 9
void pagewire_pocsag_stats(struct pagewire_decoder *d, uint32_t out[PAGEWIRE_POCSAG_STATS]);

/* pagewire_bch_init builds the BCH tables once; call before using decoders
 * from several threads. */
void pagewire_bch_init(void);

#endif

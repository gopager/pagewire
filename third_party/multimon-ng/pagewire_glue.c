/* pagewire glue, see pagewire_hooks.h. GPL-2.0-or-later like the rest. */
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "multimon.h"
#include "bch.h"
#include "pagewire_hooks.h"

_Thread_local struct demod_state *pagewire_current;

struct pagewire_decoder {
	struct demod_state st;
	struct pagewire_hooks hooks;
	const struct demod_param *par;
	float *buf;
	int cap;
	int overlap;
	int carry; /* samples kept at the start of buf from the previous block */
};

static const struct demod_param *const demods[] = {
	&demod_poc5, &demod_poc12, &demod_poc24, &demod_flex_next,
	&demod_fmsfsk,
	&demod_zvei1, &demod_zvei2, &demod_zvei3, &demod_dzvei, &demod_pzvei,
	&demod_ccir, &demod_eea, &demod_eia, &demod_dtmf, &demod_dualtone,
	&demod_eas, &demod_mpt1327, &demod_gsc,
	NULL,
};

/* demod_flex_next.c builds its records with cJSON when json_mode is set;
 * the host always wants the structured form. */
int json_mode = 1;

/* JSON output helpers of multimon-ng's main program: decoders that still
 * use them produce nothing until their output is converted to a callback. */
void addJsonTimestamp(cJSON *json)
{
	(void)json;
}

void printJson(cJSON *json)
{
	(void)json;
}

void pagewire_flex_json_out(const char *json)
{
	(void)json; /* network bookkeeping only; pages arrive through flex_message */
}

bool pocsag_init_charset(char *charset);

void _verbprintf(int verb_level, const char *fmt, ...)
{
	struct demod_state *s = pagewire_current;
	struct pagewire_hooks *h;
	char text[512];
	va_list ap;

	if (!s || !(h = s->pagewire) || !h->log || verb_level > h->verbosity)
		return;
	va_start(ap, fmt);
	vsnprintf(text, sizeof(text), fmt, ap);
	va_end(ap);
	h->log(h->ctx, verb_level, text);
}

void pagewire_bch_init(void)
{
	bch_init();
}

struct pagewire_decoder *pagewire_decoder_new(const char *name)
{
	struct pagewire_decoder *d;
	int i;

	for (i = 0; demods[i]; i++)
		if (strcmp(demods[i]->name, name) == 0)
			break;
	if (!demods[i])
		return NULL;
	d = calloc(1, sizeof(*d));
	if (!d)
		return NULL;
	d->par = demods[i];
	d->st.dem_par = d->par;
	d->st.pagewire = &d->hooks;
	d->overlap = (int)d->par->overlap;
	d->hooks.pocsag.error_correction = 2;
	return d;
}

struct pagewire_hooks *pagewire_decoder_hooks(struct pagewire_decoder *d)
{
	return &d->hooks;
}

unsigned pagewire_decoder_samplerate(struct pagewire_decoder *d)
{
	return d->par->samplerate;
}

void pagewire_decoder_init(struct pagewire_decoder *d)
{
	pagewire_current = &d->st;
	if (d->par->init)
		d->par->init(&d->st);
	d->carry = 0;
	/* Default charset until the host selects one. */
	pocsag_init_charset("US");
	pagewire_current = NULL;
}

/* Control characters as themselves: entry i is the one-byte string {i}.
 * multimon-ng renders them as "<NUL>" style tokens, which turns data into
 * text; the host wants the characters untouched. */
static const char raw_controls[33][2] = {
	{0x00, 0}, {0x01, 0}, {0x02, 0}, {0x03, 0}, {0x04, 0}, {0x05, 0}, {0x06, 0}, {0x07, 0},
	{0x08, 0}, {0x09, 0}, {0x0a, 0}, {0x0b, 0}, {0x0c, 0}, {0x0d, 0}, {0x0e, 0}, {0x0f, 0},
	{0x10, 0}, {0x11, 0}, {0x12, 0}, {0x13, 0}, {0x14, 0}, {0x15, 0}, {0x16, 0}, {0x17, 0},
	{0x18, 0}, {0x19, 0}, {0x1a, 0}, {0x1b, 0}, {0x1c, 0}, {0x1d, 0}, {0x1e, 0}, {0x1f, 0},
	{0x7f, 0},
};

int pagewire_pocsag_set_charset(struct pagewire_decoder *d, const char *name)
{
	char buf[8];
	int ok, i;

	snprintf(buf, sizeof(buf), "%s", name);
	pagewire_current = &d->st;
	ok = pocsag_init_charset(buf);
	pagewire_current = NULL;
	if (ok) {
		for (i = 1; i < 32; i++)
			d->hooks.pocsag.trtab[i] = raw_controls[i];
		d->hooks.pocsag.trtab[0x7f] = raw_controls[32];
		/* NUL cannot be a C string; a NULL entry makes the renderer emit
		 * the raw byte and report the length separately. */
		d->hooks.pocsag.trtab[0] = NULL;
	}
	return ok ? 0 : -1;
}

void pagewire_decoder_process(struct pagewire_decoder *d, const float *samples, int n)
{
	buffer_t buffer;
	int need = d->carry + n;

	if (n <= 0)
		return;
	if (need > d->cap) {
		float *nb = realloc(d->buf, (size_t)need * sizeof(float));
		if (!nb)
			return;
		d->buf = nb;
		d->cap = need;
	}
	memcpy(d->buf + d->carry, samples, (size_t)n * sizeof(float));
	/* Like multimon-ng's input loop: the demodulator consumes everything
	 * but the last `overlap` samples, which are carried into the next
	 * block so filters can look ahead. */
	if (need <= d->overlap) {
		d->carry = need;
		return;
	}
	buffer.fbuffer = d->buf;
	buffer.sbuffer = NULL;
	pagewire_current = &d->st;
	d->par->demod(&d->st, buffer, need - d->overlap);
	pagewire_current = NULL;
	memmove(d->buf, d->buf + need - d->overlap, (size_t)d->overlap * sizeof(float));
	d->carry = d->overlap;
}

void pagewire_decoder_reset(struct pagewire_decoder *d)
{
	struct pagewire_hooks hooks = d->hooks;

	pagewire_current = &d->st;
	if (d->par->deinit)
		d->par->deinit(&d->st);
	memset(&d->st, 0, sizeof(d->st));
	d->st.dem_par = d->par;
	d->st.pagewire = &d->hooks;
	d->hooks = hooks;
	if (d->par->init)
		d->par->init(&d->st);
	d->carry = 0;
	pagewire_current = NULL;
}

void pagewire_decoder_free(struct pagewire_decoder *d)
{
	if (!d)
		return;
	pagewire_current = &d->st;
	if (d->par->deinit)
		d->par->deinit(&d->st);
	pagewire_current = NULL;
	free(d->buf);
	free(d);
}

void pagewire_pocsag_stats(struct pagewire_decoder *d, uint32_t out[PAGEWIRE_POCSAG_STATS])
{
	const struct l2_state_pocsag *p = &d->st.l2.pocsag;

	out[0] = p->pocsag_total_error_count;
	out[1] = p->pocsag_corrected_error_count;
	out[2] = p->pocsag_corrected_1bit_error_count;
	out[3] = p->pocsag_corrected_2bit_error_count;
	out[4] = p->pocsag_uncorrected_error_count;
	out[5] = p->pocsag_total_bits_received;
	out[6] = p->pocsag_bits_processed_while_synced;
	out[7] = p->pocsag_bits_processed_while_not_synced;
	out[8] = (p->state & 64) ? 1u : 0u;
}

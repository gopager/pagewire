/*
 *      pocsag.c -- POCSAG protocol decoder
 *
 *      Copyright (C) 1996
 *          Thomas Sailer (sailer@ife.ee.ethz.ch, hb9jnx@hb9w.che.eu)
 *
 *      Copyright (C) 2012-2014
 *          Elias Oenal    (multimon-ng@eliasoenal.com)
 *
 *      Copyright (C) 2022
 *          Tobias Girstmair (https://gir.st/)
 *
 *      Copyright (C) 2024
 *          Jason Lingohr (jason@lucid.net.au)
 *
 *      POCSAG (Post Office Code Standard Advisory Group)
 *      Radio Paging Decoder
 *
 *  This program is free software; you can redistribute it and/or modify
 *  it under the terms of the GNU General Public License as published by
 *  the Free Software Foundation; either version 2 of the License, or
 *  (at your option) any later version.
 *
 *  This program is distributed in the hope that it will be useful,
 *  but WITHOUT ANY WARRANTY; without even the implied warranty of
 *  MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 *  GNU General Public License for more details.
 *
 *  You should have received a copy of the GNU General Public License
 *  along with this program; if not, write to the Free Software
 *  Foundation, Inc., 675 Mass Ave, Cambridge, MA 02139, USA.
 */

/* ---------------------------------------------------------------------- */

#include "multimon.h"
#include "bch.h"
#include <string.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <stdbool.h>

/* ---------------------------------------------------------------------- */

//#define CHARSET_LATIN1
//#define CHARSET_UTF8 //ÄÖÜäöüß

/* ---------------------------------------------------------------------- */



/*
 * some codewords with special POCSAG meaning
 */
#define POCSAG_SYNC     0x7cd215d8
#define POCSAG_IDLE     0x7a89c197
#define POCSAG_SYNCINFO 0x7cf21436 // what is this value?

#define POCSAG_SYNC_WORDS ((2000000 >> 3) << 13)

#define POCSAG_MESSAGE_DETECTION 0x80000000 // Most significant bit is a one

#define CAESAR_ALPHA 0
#define CAESAR_SKYPER 1 // skyper messages are ROT-1 enciphered

#define POSCAG
/* ---------------------------------------------------------------------- */

/* pagewire: options are per decoder instance (pagewire_hooks.h). The
 * output mode, partial/empty/heuristic pruning moved to the host, which
 * receives every decoded page through the pocsag_message callback. */
#include "pagewire_hooks.h"
#define pocsag_invert_input     (pagewire_current->pagewire->pocsag.invert_input)
#define pocsag_error_correction (pagewire_current->pagewire->pocsag.error_correction)
#define pocsag_polarity         (pagewire_current->pagewire->pocsag.polarity)

/* ---------------------------------------------------------------------- */


enum states{
    NO_SYNC = 0,            //0b00000000
    SYNC = 64,              //0b10000000
    LOSING_SYNC = 65,       //0b10000001
    LOST_SYNC = 66,         //0b10000010
    ADDRESS = 67,           //0b10000011
    MESSAGE = 68,           //0b10000100
    END_OF_MESSAGE = 69,    //0b10000101
};


static inline unsigned char even_parity(uint32_t data)
{
    unsigned int temp = data ^ (data >> 16);

    temp = temp ^ (temp >> 8);
    temp = temp ^ (temp >> 4);
    temp = temp ^ (temp >> 2);
    temp = temp ^ (temp >> 1);
    return temp & 1;
}

/* ---------------------------------------------------------------------- */
	// ISO 646 national variant: US / IRV (1991)
    static const char *const trtab_default[128] = {
			"<NUL>", 	//  0x0
			"<SOH>", 	//  0x1
			"<STX>", 	//  0x2
			"<ETX>", 	//  0x3
			"<EOT>", 	//  0x4
			"<ENQ>", 	//  0x5
			"<ACK>", 	//  0x6
			"<BEL>", 	//  0x7
			"<BS>",  	//  0x8
			"<HT>",  	//  0x9
			"<LF>",  	//  0xa
			"<VT>",  	//  0xb
			"<FF>",  	//  0xc
			"<CR>",  	//  0xd
			"<SO>",  	//  0xe
			"<SI>",  	//  0xf
			"<DLE>", 	// 0x10
			"<DC1>", 	// 0x11
			"<DC2>", 	// 0x12
			"<DC3>", 	// 0x13
			"<DC4>", 	// 0x14
			"<NAK>", 	// 0x15
			"<SYN>", 	// 0x16
			"<ETB>", 	// 0x17
			"<CAN>", 	// 0x18
			"<EM>",  	// 0x19
			"<SUB>", 	// 0x1a
			"<ESC>", 	// 0x1b
			"<FS>",  	// 0x1c
			"<GS>",  	// 0x1d
			"<RS>",  	// 0x1e
			"<US>",  	// 0x1f
			" ", 		// 0x20
			"!", 		// 0x21
			"\"", 		// 0x22

						// national variant
			"#", 		// 0x23
			"$", 		// 0x24

			"%", 		// 0x25
			"&", 		// 0x26
			"'", 		// 0x27
			"(", 		// 0x28
			")", 		// 0x29
			"*", 		// 0x2a
			"+", 		// 0x2b
			",", 		// 0x2c
			"-", 		// 0x2d
			".", 		// 0x2e
			"/", 		// 0x2f
			"0", 		// 0x30
			"1", 		// 0x31
			"2", 		// 0x32
			"3", 		// 0x33
			"4", 		// 0x34
			"5", 		// 0x35
			"6", 		// 0x36
			"7", 		// 0x37
			"8", 		// 0x38
			"9", 		// 0x39
			":", 		// 0x3a
			";", 		// 0x3b
			"<", 		// 0x3c
			"=", 		// 0x3d
			">", 		// 0x3e
			"?", 		// 0x3f
			"@", 		// 0x40
			"A", 		// 0x41
			"B", 		// 0x42
			"C", 		// 0x43
			"D", 		// 0x44
			"E", 		// 0x45
			"F", 		// 0x46
			"G", 		// 0x47
			"H", 		// 0x48
			"I", 		// 0x49
			"J", 		// 0x4a
			"K", 		// 0x4b
			"L", 		// 0x4c
			"M", 		// 0x4d
			"N", 		// 0x4e
			"O", 		// 0x4f
			"P", 		// 0x50
			"Q", 		// 0x51
			"R", 		// 0x52
			"S", 		// 0x53
			"T", 		// 0x54
			"U", 		// 0x55
			"V", 		// 0x56
			"W", 		// 0x57
			"X", 		// 0x58
			"Y", 		// 0x59
			"Z", 		// 0x5a
			
						// national variant
			"[", 		// 0x5b
			"\\", 		// 0x5c
			"]", 		// 0x5d
			"^", 		// 0x5e
			
			"_", 		// 0x5f

						// national variant
			"`", 		// 0x60

			"a", 		// 0x61
			"b", 		// 0x62
			"c", 		// 0x63
			"d", 		// 0x64
			"e", 		// 0x65
			"f", 		// 0x66
			"g", 		// 0x67
			"h", 		// 0x68
			"i", 		// 0x69
			"j", 		// 0x6a
			"k", 		// 0x6b
			"l", 		// 0x6c
			"m", 		// 0x6d
			"n", 		// 0x6e
			"o", 		// 0x6f
			"p", 		// 0x70
			"q", 		// 0x71
			"r", 		// 0x72
			"s", 		// 0x73
			"t", 		// 0x74
			"u", 		// 0x75
			"v", 		// 0x76
			"w", 		// 0x77
			"x", 		// 0x78
			"y", 		// 0x79
			"z", 		// 0x7a
			
						// national variant
			"{", 		// 0x7b
			"|", 		// 0x7c
			"}", 		// 0x7d
			"~", 		// 0x7e
			
			"<DEL>"		// 0x7f  
		};


/*
						// national variant
			"#", 		// 0x23
			"$", 		// 0x24

			"[", 		// 0x5b
			"\\", 		// 0x5c
			"]", 		// 0x5d
			"^", 		// 0x5e

			"`", 		// 0x60

			"{", 		// 0x7b
			"|", 		// 0x7c
			"}", 		// 0x7d
			"~", 		// 0x7e
*/

/* pagewire: multimon-ng's own text for one 7-bit character in the
 * default table ("<NUL>" style tokens for control characters), so hosts can
 * reproduce multimon-ng's printed output without copying the table. */
const char *pagewire_pocsag_default_token(unsigned ch)
{
	return trtab_default[ch & 0x7f];
}

bool pocsag_init_charset(char *charset)
{
	/* pagewire: the table lives in the instance; start from the defaults. */
	const char **trtab = pagewire_current->pagewire->pocsag.trtab;
	memcpy(trtab, trtab_default, sizeof(trtab_default));
	if(strcmp(charset,"DE")==0) // German charset
	{
		#ifdef CHARSET_UTF8
			trtab[0x5b] = "Ä";
			trtab[0x5c] = "Ö";
			trtab[0x5d] = "Ü";

			trtab[0x7b] = "ä";
			trtab[0x7c] = "ö";
			trtab[0x7d] = "ü";
			trtab[0x7e] = "ß";
		#elif defined CHARSET_LATIN1
			trtab[0x5b] = "\304";
			trtab[0x5c] = "\326";
			trtab[0x5d] = "\334";

			trtab[0x7b] = "\344";
			trtab[0x7c] = "\366";
			trtab[0x7d] = "\374";
			trtab[0x7e] = "\337";
		#else
			trtab[0x5b] = "AE";
			trtab[0x5c] = "OE";
			trtab[0x5d] = "UE";

			trtab[0x7b] = "ae";
			trtab[0x7c] = "oe";
			trtab[0x7d] = "ue";
			trtab[0x7e] = "ss";
		#endif
	}
	else if (strcmp(charset,"DK")==0) // Danish charset /JT Ref. https://www.ascii-code.com
	{
		#ifdef CHARSET_UTF8
			trtab[0x5b] = "Æ";
			trtab[0x5c] = "Ø";
			trtab[0x5d] = "Å";

			trtab[0x7b] = "æ";
			trtab[0x7c] = "ø";
			trtab[0x7d] = "å";
		#elif defined CHARSET_LATIN1
			trtab[0x5b] = "\306";
			trtab[0x5c] = "\330";
			trtab[0x5d] = "\305";

			trtab[0x7b] = "\346";
			trtab[0x7c] = "\370";
			trtab[0x7d] = "\345";
		#else
			trtab[0x5b] = "AE";
			trtab[0x5c] = "OE";
			trtab[0x5d] = "Aa";

			trtab[0x7b] = "ae";
			trtab[0x7c] = "oe";
			trtab[0x7d] = "aa";
		#endif
	}
	else if (strcmp(charset,"SE")==0) // Swedish charset
	{
		#ifdef CHARSET_UTF8
			trtab[0x5b] = "Ä";
			trtab[0x5c] = "Ö";
			trtab[0x5d] = "Å";

			trtab[0x7b] = "ä";
			trtab[0x7c] = "ö";
			trtab[0x7d] = "å";
		#elif defined CHARSET_LATIN1
			trtab[0x5b] = "\304";
			trtab[0x5c] = "\326";
			trtab[0x5d] = "\305";

			trtab[0x7b] = "\344";
			trtab[0x7c] = "\366";
			trtab[0x7d] = "\345";
		#else
			trtab[0x5b] = "AE";
			trtab[0x5c] = "OE";
			trtab[0x5d] = "AO";

			trtab[0x7b] = "ae";
			trtab[0x7c] = "oe";
			trtab[0x7d] = "ao";
		#endif
	}
	else if (strcmp(charset,"FR")==0) // French charset
	{
		trtab[0x24] = "£";

		trtab[0x40] = "à";

		trtab[0x5b] = "°";
		trtab[0x5c] = "ç";
		trtab[0x5d] = "§";
		trtab[0x5e] = "^";
		trtab[0x5f] = "_";
		trtab[0x60] = "µ";

		trtab[0x7b] = "é";
		trtab[0x7c] = "ù";
		trtab[0x7d] = "è";
		trtab[0x7e] = "¨";
	}
	else if (strcmp(charset,"SI")==0) // Slovenian charset
	{
		#ifdef CHARSET_UTF8
			trtab[0x40] = "Ž";
			trtab[0x5b] = "Š";
			trtab[0x5c] = "Đ";
			trtab[0x5d] = "Ć";
			trtab[0x5e] = "Č";

			trtab[0x60] = "ž";
			trtab[0x7b] = "š";
			trtab[0x7c] = "đ";
			trtab[0x7d] = "ć";
			trtab[0x7e] = "č";
		#else
			trtab[0x40] = "Z";
			trtab[0x5b] = "S";
			trtab[0x5c] = "Dj";
			trtab[0x5d] = "C";
			trtab[0x5e] = "C";

			trtab[0x60] = "z";
			trtab[0x7b] = "s";
			trtab[0x7c] = "dj";
			trtab[0x7d] = "c";
			trtab[0x7e] = "c";
		#endif
	}
	else if (strcmp(charset,"US")==0) // US charset
	{
		// default
	}
	else
	{
		fprintf(stderr, "Error: invalid POCSAG charset %s\n", charset);
		fprintf(stderr, "Use: US,FR,DE,DK,SE,SI\n");
		charset = "US";
		return false; 
	}
	return true;
}

static const char *translate_alpha(unsigned char chr)
{
	return pagewire_current->pagewire->pocsag.trtab[chr & 0x7f];
}

/* ---------------------------------------------------------------------- */
static int guesstimate_alpha(const unsigned char cp)
{
    if((cp > 0 && cp < 32) || cp == 127)
        return -5; // Non printable characters are uncommon
    else if((cp > 32 && cp < 48)
            || (cp > 57 && cp < 65)
            || (cp > 90 && cp < 97)
            || (cp > 122 && cp < 127))
        return -2; // Penalize special characters
    else
        return 1;
}

static int guesstimate_numeric(const unsigned char cp, int pos)
{
    if(cp == 'U')
        return -10;
    else if(cp == '[' || cp == ']')
        return -5;
    else if(cp == ' ' || cp == '.' || cp == '-')
        return -2;
    else if(pos < 10) // Penalize long messages
        return 5;
    else
        return 0;
}

static int print_msg_numeric(struct l2_state_pocsag *rx, char* buff, unsigned int size)
{
    static const char *conv_table = "084 2.6]195-3U7[";
    unsigned char *bp = rx->buffer;
    int len = rx->numnibbles;
    char* cp = buff;
    int guesstimate = 0;

    if ( (unsigned int) len >= size)
        len = size-1;
    for (; len > 0; bp++, len -= 2) {
        *cp++ = conv_table[(*bp >> 4) & 0xf];
        if (len > 1)
            *cp++ = conv_table[*bp & 0xf];
    }
    *cp = '\0';

    cp = buff;
    for(int i = 0; *(cp+i); i++)
        guesstimate += guesstimate_numeric(*(cp+i), i);
    return guesstimate;
}

static unsigned char get7(const unsigned char *buf, int n)
{
    /* returns the n-th seven bit word */
    return ( buf[(n*7)/8]<<8 | buf[(n*7+6)/8] ) >> (n+1)%8;
}

static unsigned char rev7(unsigned char b)
{
    /* reverses the bit order of a seven bit word */
    return ((b << 6) & 64) | ((b >> 6) & 1) |
           ((b << 4) & 32) | ((b >> 4) & 2) |
           ((b << 2) & 16) | ((b >> 2) & 4) |
           ((b << 0) & 8);
}

/* pagewire: *outlen receives the rendered length so that NUL
 * characters (table entry NULL) can be part of the output. */
static int print_msg_alpha(struct l2_state_pocsag *rx, char* buff, unsigned int size, int caesar, unsigned int *outlen)
{
    int len = rx->numnibbles * 4 / 7;
    char* cp = buff;
    int buffree = size-1;
    unsigned char curchr;
    const char *tstr;
    int guesstimate = 0;

    for (int i = 0; i < len; i++)
    {
        curchr = rev7(get7(rx->buffer, i)) - caesar;

        guesstimate += guesstimate_alpha(curchr);

        tstr = translate_alpha(curchr);
        if (tstr)
        {
            int tlen = strlen(tstr);
            if (buffree >= tlen)
            {
                memcpy(cp, tstr, tlen);
                cp += tlen;
                buffree -= tlen;
            }
        } else if (buffree > 0) {
            *cp++ = curchr;
            buffree--;
        }
    }
    *cp = '\0';
    if (outlen)
        *outlen = (unsigned int)(cp - buff);

    return guesstimate;
}

/* ---------------------------------------------------------------------- */

static void pocsag_printmessage(struct demod_state *s, bool sync)
{
    /* pagewire: hand every received page to the host with all
     * three renderings; the host decides on format, partial decodes and
     * pruning (upstream did this here based on global options). */
    struct pagewire_hooks *h = s->pagewire;
    struct pagewire_pocsag_message m;
    /* pagewire: sized for the largest payload (1024 nibbles) with
     * two-byte charset expansions, so nothing is ever cut off. */
    char num_string[2048];
    char alpha_string[2048];
    char skyper_string[2048];

    if (!h || !h->pocsag_message)
        return;
    if ((s->l2.pocsag.address == -1) && (s->l2.pocsag.function == -1))
        return;

    memset(&m, 0, sizeof(m));
    num_string[0] = alpha_string[0] = skyper_string[0] = '\0';
    m.address = s->l2.pocsag.address;
    m.function = s->l2.pocsag.function;
    m.sync = sync;
    m.nibbles = s->l2.pocsag.buffer;
    m.numnibbles = s->l2.pocsag.numnibbles;
    if (s->l2.pocsag.numnibbles) {
        m.guess_numeric = print_msg_numeric(&s->l2.pocsag, num_string, sizeof(num_string));
        m.guess_alpha = print_msg_alpha(&s->l2.pocsag, alpha_string, sizeof(alpha_string), CAESAR_ALPHA, &m.alpha_len);
        m.guess_skyper = print_msg_alpha(&s->l2.pocsag, skyper_string, sizeof(skyper_string), CAESAR_SKYPER, &m.skyper_len);
    }
    m.numeric = num_string;
    m.alpha = alpha_string;
    m.skyper = skyper_string;
    h->pocsag_message(h->ctx, &m);
}

/* ---------------------------------------------------------------------- */

void pocsag_init(struct demod_state *s)
{
    memset(&s->l2.pocsag, 0, sizeof(s->l2.pocsag));
    s->l2.pocsag.address = -1;
    s->l2.pocsag.function = -1;
}

void pocsag_deinit(struct demod_state *s)
{
    if(s->l2.pocsag.pocsag_total_error_count)
        verbprintf(1, "\n===%s stats===\n"
                   "Words BCH checked: %u\n"
                   "Corrected errors: %u\n"
                   "Corrected 1bit errors: %u\n"
                   "Corrected 2bit errors: %u\n"
                   "Invalid word or >2 bits errors: %u\n\n"
                   "Total bits processed: %u\n"
                   "Bits processed while in sync: %u\n"
                   "Bits processed while out of sync: %u\n"
                   "Successfully decoded: %f%%\n",
                   s->dem_par->name,
                   s->l2.pocsag.pocsag_total_error_count,
                   s->l2.pocsag.pocsag_corrected_error_count,
                   s->l2.pocsag.pocsag_corrected_1bit_error_count,
                   s->l2.pocsag.pocsag_corrected_2bit_error_count,
                   s->l2.pocsag.pocsag_uncorrected_error_count,
                   s->l2.pocsag.pocsag_total_bits_received,
                   s->l2.pocsag.pocsag_bits_processed_while_synced,
                   s->l2.pocsag.pocsag_bits_processed_while_not_synced,
                   (100./s->l2.pocsag.pocsag_total_bits_received)*s->l2.pocsag.pocsag_bits_processed_while_synced);
    fflush(stdout);
}

/* ---------------------------------------------------------------------- */

// BCH error correction using unified bch library
int pocsag_brute_repair(struct l2_state_pocsag *rx, uint32_t* data)
{
    int result = bch_pocsag_correct(data);
    
    if (result == 0) {
        return 0;  /* No errors */
    }
    
    rx->pocsag_total_error_count++;
    verbprintf(6, "Error in syndrome detected!\n");
    
    if (pocsag_error_correction == 0) {
        rx->pocsag_uncorrected_error_count++;
        verbprintf(6, "Couldn't correct error!\n");
        return 1;
    }
    
    if (result < 0) {
        /* Uncorrectable error */
        rx->pocsag_uncorrected_error_count++;
        verbprintf(6, "Couldn't correct error!\n");
        return 1;
    }
    
    /* Check if we're allowed to correct this many errors */
    if (result > pocsag_error_correction) {
        rx->pocsag_uncorrected_error_count++;
        verbprintf(6, "Couldn't correct error!\n");
        return 1;
    }
    
    /* Correction was applied by bch_pocsag_correct */
    rx->pocsag_corrected_error_count++;
    
    if (result == 1) {
        rx->pocsag_corrected_1bit_error_count++;
    } else {
        rx->pocsag_corrected_2bit_error_count++;
    }
    
    return 0;
}

static inline bool word_complete(struct demod_state *s)
{    
    // Do nothing for 31 bits
    // When the word is complete let the program counter pass
    s->l2.pocsag.rx_bit = (s->l2.pocsag.rx_bit + 1) % 32;
    return s->l2.pocsag.rx_bit == 0;
}

static inline bool is_sync(const uint32_t * const rx_data)
{
    if(*rx_data == POCSAG_SYNC)
        return true; // Sync found!
    return false;
}

static inline bool is_idle(const uint32_t * const rx_data)
{
    if(*rx_data == POCSAG_IDLE)
        return true; // Idle found!
    return false;
}

static void do_one_bit(struct demod_state *s, uint32_t rx_data)
{
    s->l2.pocsag.pocsag_total_bits_received++;

    switch(s->l2.pocsag.state & SYNC)
    {
    case NO_SYNC:
    {
        uint32_t rx_data_try;
        
        s->l2.pocsag.pocsag_bits_processed_while_not_synced++;

        /* Try normal polarity with error correction (unless inverted-only mode) */
        if(pocsag_polarity != 2)
        {
            rx_data_try = rx_data;
            pocsag_brute_repair(&s->l2.pocsag, &rx_data_try);
            if(rx_data_try == POCSAG_SYNC)
            {
                verbprintf(4, "Acquired sync!\n");
                s->l2.pocsag.state = SYNC;
                s->l2.pocsag.inverted = 0;
                return;
            }
        }
        
        /* Try inverted polarity with error correction (unless normal-only mode) */
        if(pocsag_polarity != 1)
        {
            rx_data_try = ~rx_data;
            pocsag_brute_repair(&s->l2.pocsag, &rx_data_try);
            if(rx_data_try == POCSAG_SYNC)
            {
                verbprintf(3, "Acquired sync (inverted polarity detected)!\n");
                s->l2.pocsag.state = SYNC;
                s->l2.pocsag.inverted = 1;
                return;
            }
        }
        return;
    }

    case SYNC:
    {
        s->l2.pocsag.pocsag_bits_processed_while_synced++;

        /* Apply inversion if auto-detected */
        if(s->l2.pocsag.inverted)
            rx_data = ~rx_data;

        if(!word_complete(s))
            return; // Wait for more bits to arrive.

        // it is always 17 words: position 0 is sync, positions 1-16 are data
        unsigned char rxword = s->l2.pocsag.rx_word; // for address calculation
        s->l2.pocsag.rx_word = (s->l2.pocsag.rx_word + 1) % 17;

        if(s->l2.pocsag.state == SYNC)
            s->l2.pocsag.state = ADDRESS; // We're in sync, move on.

        if(pocsag_brute_repair(&s->l2.pocsag, &rx_data))
        {
            // Arbitration lost
            if(s->l2.pocsag.state != LOST_SYNC)
                s->l2.pocsag.state = LOSING_SYNC;
        }
        else
        {
            if(s->l2.pocsag.state == LOST_SYNC)
            {
                verbprintf(4, "Recovered sync!\n");
                s->l2.pocsag.state = ADDRESS;
            }
        }

        if(is_sync(&rx_data))
            return; // Already sync'ed.

        while(true)
            switch(s->l2.pocsag.state)
            {
            case LOSING_SYNC:
            {
                verbprintf(4, "Losing sync!\n");
                // Output what we've received so far.
                pocsag_printmessage(s, false);
                s->l2.pocsag.numnibbles = 0;
                s->l2.pocsag.address = -1;
                s->l2.pocsag.function = -1;
                s->l2.pocsag.state = LOST_SYNC;
                return;
            }

            case LOST_SYNC:
            {
                verbprintf(4, "Lost sync!\n");
                s->l2.pocsag.state = NO_SYNC;
                s->l2.pocsag.rx_word = 0;
                return;
            }

            case ADDRESS:
            {
                if(is_idle(&rx_data)) // Idle codewords have a magic address
                    return;

                if(rx_data & POCSAG_MESSAGE_DETECTION)
                {
                    verbprintf(4, "Got a message: %u\n", rx_data);
                    s->l2.pocsag.function = -2;
                    s->l2.pocsag.address  = -2;
                    s->l2.pocsag.state = MESSAGE;
                    break; // Performing partial decode
                }

                verbprintf(4, "Got an address: %u\n", rx_data);
                s->l2.pocsag.function = (rx_data >> 11) & 3;
                s->l2.pocsag.address  = ((rx_data >> 10) & 0x1ffff8) | ((rxword >> 1) & 7);
                s->l2.pocsag.state = MESSAGE;
                return;
            }

            case MESSAGE:
            {
                if(rx_data & POCSAG_MESSAGE_DETECTION)
                    verbprintf(4, "Got a message: %u\n", rx_data);
                else
                {
                    // Address/idle signals end of message
                    verbprintf(4, "Got an address: %u\n", rx_data);
                    s->l2.pocsag.state = END_OF_MESSAGE;
                    break;
                }

                if (s->l2.pocsag.numnibbles > sizeof(s->l2.pocsag.buffer)*2 - 5) {
                    verbprintf(2, "%s: Warning: Message too long\n",
                               s->dem_par->name);
                    /* Message too long indicates we're decoding garbage - lose sync */
                    s->l2.pocsag.state = LOSING_SYNC;
                    break;
                }

                uint32_t data;
                unsigned char *bp;
                bp = s->l2.pocsag.buffer + (s->l2.pocsag.numnibbles >> 1);
                data = (rx_data >> 11);
                if (s->l2.pocsag.numnibbles & 1) {
                    bp[0] = (bp[0] & 0xf0) | ((data >> 16) & 0xf);
                    bp[1] = data >> 8;
                    bp[2] = data;
                } else {
                    bp[0] = data >> 12;
                    bp[1] = data >> 4;
                    bp[2] = data << 4;
                }
                s->l2.pocsag.numnibbles += 5;
                verbprintf(5, "We received something!\n");
                return;
            }

            case END_OF_MESSAGE:
            {
                verbprintf(4, "End of message!\n");
                pocsag_printmessage(s, true);
                s->l2.pocsag.numnibbles = 0;
                s->l2.pocsag.address = -1;
                s->l2.pocsag.function = -1;
                s->l2.pocsag.state = ADDRESS;
                break;
            }

            default:
                break;
            }

    }

    default:
        break;
    }
}



/* ---------------------------------------------------------------------- */

void pocsag_rxbit(struct demod_state *s, int32_t bit)
{
    s->l2.pocsag.rx_data <<= 1;
    s->l2.pocsag.rx_data |= !bit;
    verbprintf(9, " %c ", '1'-(s->l2.pocsag.rx_data & 1));
    if(pocsag_invert_input)
        do_one_bit(s, ~(s->l2.pocsag.rx_data)); // this tries the inverted signal
    else
        do_one_bit(s, s->l2.pocsag.rx_data);
}

/* ---------------------------------------------------------------------- */

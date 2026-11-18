/*
 *      fms.c -- fms decoder and packet dump
 *
 *      Copyright (C) 2007
 *
 *      This program is free software; you can redistribute it and/or modify
 *      it under the terms of the GNU General Public License as published by
 *      the Free Software Foundation; either version 2 of the License, or
 *      (at your option) any later version.
 *
 *      This program is distributed in the hope that it will be useful,
 *      but WITHOUT ANY WARRANTY; without even the implied warranty of
 *      MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 *      GNU General Public License for more details.
 *
 *      You should have received a copy of the GNU General Public License
 *      along with this program; if not, write to the Free Software
 *      Foundation, Inc., 675 Mass Ave, Cambridge, MA 02139, USA.
 */

/* ---------------------------------------------------------------------- */

#include "multimon.h"
#include <string.h>
/* pagewire: telegrams reach the host as struct pagewire_fms_message. */
#include <stdarg.h>
#include <stdio.h>
#include "pagewire_hooks.h"
#include "pagewire_fms.h"

/* ---------------------------------------------------------------------- */

bool fms_justhex = false;

/* pagewire: the rendering that used to go to the log at level 0 is
 * collected here and delivered as the text of the record. */
static _Thread_local char fms_text[512];
static _Thread_local size_t fms_text_len;

static void fms_out(const char *fmt, ...)
{
    va_list ap;
    int n;

    if (fms_text_len >= sizeof(fms_text) - 1)
        return;
    va_start(ap, fmt);
    n = vsnprintf(fms_text + fms_text_len, sizeof(fms_text) - fms_text_len, fmt, ap);
    va_end(ap);
    if (n > 0)
        fms_text_len += (size_t)n < sizeof(fms_text) - fms_text_len ? (size_t)n : sizeof(fms_text) - fms_text_len - 1;
}

/* ---------------------------------------------------------------------- */

static void fms_disp_service_id(uint8_t service_id)
{
    fms_out("%1x=", service_id);
    switch (service_id)
    {
        case 0x0: fms_out("UNKNOWN       \t"); break;
        case 0x1: fms_out("POLIZEI       \t"); break;
        case 0x2: fms_out("BGS           \t"); break;
        case 0x3: fms_out("BKA           \t"); break;
        case 0x4: fms_out("KatS          \t"); break;
        case 0x5: fms_out("ZOLL          \t"); break;
        case 0x6: fms_out("Feuerwehr     \t"); break;
        case 0x7: fms_out("THW           \t"); break;
        case 0x8: fms_out("ASB           \t"); break;
        case 0x9: fms_out("Rotkreuz      \t"); break;
        case 0xa: fms_out("Johanniter    \t"); break;
        case 0xb: fms_out("Malteser      \t"); break;
        case 0xc: fms_out("DLRG          \t"); break;
        case 0xd: fms_out("Rettungsdienst\t"); break;
        case 0xe: fms_out("ZivilSchutz   \t"); break;
        case 0xf: fms_out("FernWirk      \t"); break;
    }
}

static void fms_disp_state_id(uint8_t state_id, uint8_t loc_id)
{
    fms_out("%1x=", state_id);
    switch (state_id)
    {
        case 0x0: fms_out("Sachsen         \t"); break;
        case 0x1: fms_out("Bund            \t"); break;
        case 0x2: fms_out("Baden-Wurtemberg\t"); break;
        case 0x3: fms_out("Bayern 1        \t"); break;
        case 0x4: fms_out("Berlin          \t"); break;
        case 0x5: fms_out("Bremen          \t"); break;
        case 0x6: fms_out("Hamburg         \t"); break;
        case 0x7: fms_out("Hessen          \t"); break;
        case 0x8: fms_out("Niedersachsen   \t"); break;
        case 0x9: fms_out("Nordrhein-Wesfal\t"); break;
        case 0xa: fms_out("Rheinland-Pfalz \t"); break;
        case 0xb: fms_out("Schleswig-Holste\t"); break;
        case 0xc: fms_out("Saarland        \t"); break;
        case 0xd: fms_out("Bayern 2        \t"); break;
        case 0xe: if (loc_id < 50) fms_out("Meckl-Vorpommern\t"); else fms_out("Sachsen-Anhalt  \t"); break;
        case 0xf: if (loc_id < 50) fms_out("Brandenburg     \t"); else fms_out("Thuringen       \t"); break;
    }
}

static void fms_disp_loc_id(uint8_t loc_id)
{
    //fix due to wrong location id
    //now we are according to TR-BOS
    uint8_t tmp = 0;
    tmp = loc_id;
    loc_id <<= 4;
    tmp >>= 4;
    loc_id = loc_id^tmp;
    
    fms_out("Ort 0x%2x=%03d\t", loc_id, loc_id);
}

static void fms_disp_vehicle_id(uint16_t vehicle_id)
{
    uint8_t nib0 = (vehicle_id) & 0xF;
    uint8_t nib1 = (vehicle_id >> 4) & 0xF;
    uint8_t nib2 = (vehicle_id >> 8) & 0xF;
    uint8_t nib3 = (vehicle_id >> 12) & 0xF;

    fms_out("FZG %1x%1x%1x%1x\t", nib0, nib1, nib2, nib3);
}

static void fms_disp_state(uint8_t state, uint8_t direction)
{
    fms_out("Status %1x=", state);

    // TODO: Other services?
    if (direction == 0){
        // FZG -> LST
        switch (state)
        {
            case 0x0: fms_out("Notfall       \t"); break;
            case 0x1: fms_out("Einbuchen     \t"); break;
            case 0x2: fms_out("Bereit Wache  \t"); break;
            case 0x3: fms_out("Einsatz Ab    \t"); break; // Einsatz übernommen, "Ab"
            case 0x4: fms_out("Am EinsatzZiel\t"); break; // "AN"
            case 0x5: fms_out("Sprechwunsch  \t"); break;
            case 0x6: fms_out("Nicht Bereit  \t"); break;
            case 0x7: fms_out("Patient aufgen\t"); break; // Patient aufgenommen
            case 0x8: fms_out("Am TranspZiel \t"); break;
            case 0x9: fms_out("Arzt Aufgenomm\t"); break; // Arzt aufgenommen / Handquittung / Anmeldung im Fremdkreis / Dringender Sprechwunsch
            case 0xa: fms_out("Vorbertg Folge\t"); break; // Vorbereitung Folgetelegram
            case 0xb: fms_out("Beendig  Folge\t"); break; // Beendigung Folgetelegram
            case 0xc: fms_out("Sonder 1      \t"); break;
            case 0xd: fms_out("Sonder 2      \t"); break;
            case 0xe: fms_out("AutomatQuittun\t"); break; // Automatische Quittung
            case 0xf: fms_out("Sprechtaste   \t"); break;
        }
    }
    else
    {
        // LST -> FZG
        switch (state)
        {
            case 0x0: fms_out("StatusAbfrage \t"); break;
            case 0x1: fms_out("SammelRuf     \t"); break;
            case 0x2: fms_out("Einrucken/Abbr\t"); break; // Einrücken / Einsatz abgebrochen
            case 0x3: fms_out("Ubernahme     \t"); break; // Melden für Einsatzübernahme
            case 0x4: fms_out("Kommen Draht  \t"); break; // "Kommen Sie über Draht"
            case 0x5: fms_out("Fahre Wache   \t"); break; // "Fahren Sie Wache an"
            case 0x6: fms_out("Sprechaufford \t"); break; // Sprechaufforderung
            case 0x7: fms_out("Lagemeldung   \t"); break; // "Geben Sie Lagemeldung"
            case 0x8: fms_out("FernWirk 1    \t"); break;
            case 0x9: fms_out("FernWirk 2    \t"); break;
            case 0xa: fms_out("Vorbertg TXT  \t"); break;
            case 0xb: fms_out("Beendig  TXT  \t"); break;
            case 0xc: fms_out("KurzTXT C     \t"); break;
            case 0xd: fms_out("KurzTXT D     \t"); break;
            case 0xe: fms_out("KurzTXT E     \t"); break;
            case 0xf: fms_out("AutomatQuittun\t"); break; // Automatische Quittung
        }
    }
}

static void fms_disp_direction(uint8_t direction)
{
    if (direction)
    {
        fms_out("1=LST->FZG\t");
    }
    else
    {
        fms_out("0=FZG->LST\t");
    }
}

static void fms_disp_shortinfo(uint8_t short_info)
{
    fms_out("%1x=", short_info);

    switch (short_info)
    {
        case 0x0: fms_out("I  (ohneNA,ohneSIGNAL)\t"); break;
        case 0x1: fms_out("II (ohneNA,mit SIGNAL)\t"); break;
        case 0x2: fms_out("III(mit NA,ohneSIGNAL)\t"); break;
        case 0x3: fms_out("IV (mit NA,mit SIGNAL)\t"); break;
        }
}

static void fms_print_crc(char crc[7]) {
  int i;

  verbprintf(2, "FMS CRC:");

  for (i = 0; i < 7; ++i) {
    if (crc[i]) {
      verbprintf(2, "1");
    } else {
      verbprintf(2, "0");
    }
  }

  verbprintf(2, "\n");
}

void fms_print_message_hex(uint64_t message)
{
    verbprintf(2, "FMS MESSAGE HIGH: %08x\n", message >> 32);
    verbprintf(2, "FMS MESSAGE  LOW: %08x\n", message);
}


static char fms_is_crc_correct(uint64_t message)
{
    int i;
    char crc[7];
    char doinvert;

    // Initialize the crc to 0s
    for (i=0; i<7; ++i)
    {
        crc[i] = 0;
    }


    for (i=0; i<48; ++i)
    {
        // update the running CRC
        // The implementation is not the most performant one but it suffices for my basic understanding of CRCs.
        // Code generated by http://ghsi.de/CRC/index.php
        // ==========================================================================
        // CRC Generation Unit - Linear Feedback Shift Register implementation
        // (c) Kay Gorontzi, GHSi.de, distributed under the terms of LGPL
        // ==========================================================================
        verbprintf(4, "FMS CRC BIT: %1x\n", ((message >> (16+i)) & 1));

        doinvert = ((message >> (16+i)) & 1) ^ crc[6];         // XOR required?

        crc[6] = crc[5] ^ doinvert;
        crc[5] = crc[4];
        crc[4] = crc[3];
        crc[3] = crc[2];
        crc[2] = crc[1] ^ doinvert;
        crc[1] = crc[0];
        crc[0] = doinvert;

        //fms_print_crc(crc);
    }

    for (i=0; i<7; ++i)
    {
        if (crc[i])
        {
            fms_print_crc(crc);
            return 0;
        }
    }
    return 1;
}

/*
 *  As specified in http://www.lfs-bw.de/Fachthemen/Digitalfunk-Funk/Documents/Pruefstelle/TRBOS-FMS.pdf
 */
static void fms_pack_bits(uint64_t message, uint8_t out[6])
{
    int i;

    memset(out, 0, 6);
    for (i = 0; i < 48; i++)
        if ((message >> (16 + i)) & 1)
            out[i / 8] |= (uint8_t)(0x80 >> (i % 8));
}

/* pagewire: fills a record and hands it to the host when emit is
 * set; the intermediate display of an uncorrected packet (before a one bit
 * repair) only goes to the log. received is the packet as it came off the
 * air. */
static void fms_disp_packet(struct demod_state *s, uint64_t message, uint64_t received, int emit)
{
    uint8_t service_id;  // BOS-Kennung
    uint8_t state_id;    // Landeskennung
    uint8_t loc_id;      // Ortskennung
    uint16_t vehicle_id; // Fahrzeugkennung
    uint8_t state;       // Status
    uint8_t model;       // Baustufenkennung
    uint8_t direction;   // Richtungskennung
    uint8_t short_info;  // taktische Kurzinformation
    uint8_t crc;         // Redundanz
    struct pagewire_hooks *h = s->pagewire;
    struct pagewire_fms_message m;
    int crc_ok;

    fms_text_len = 0;
    fms_text[0] = '\0';
    fms_print_message_hex(message);

    fms_out("FMS: %08x%04x", (uint32_t)(message >> 32), ((uint32_t)message >> 16));

    service_id = (message >> 16) & 0xF;
    state_id = (message >> 20) & 0xF;
    loc_id = (message >> 24) & 0xFF;
    vehicle_id = (message >> 32) & 0xFFFF;
    state = (message >> 48) & 0xF;
    model = (message >> 52) & 0x1;
    direction = (message >> 53) & 0x1;
    short_info = (message >> 54) & 0x3;
    crc = (message >> 55) & 0x3F;

    if(!fms_justhex)
    {
        fms_out(" (");
        fms_disp_service_id(service_id);
        fms_disp_state_id(state_id, loc_id);
        fms_disp_loc_id(loc_id);
        fms_disp_vehicle_id(vehicle_id);
        fms_disp_state(state, direction);
        fms_disp_direction(direction);
        fms_disp_shortinfo(short_info);
        fms_out(") ");
    }
    else fms_out(" ");

    crc_ok = fms_is_crc_correct(message);
    if (crc_ok)
    {
        fms_out("CRC correct");
        if (message & 1)
        {
            fms_out(" AFTER SWAPPING ONE BIT");
        }
    }
    else fms_out("CRC INCORRECT (%x)", crc);

    if (!emit || !h || !h->fms_message)
    {
        verbprintf(2, "%s\n", fms_text);
        return;
    }
    memset(&m, 0, sizeof(m));
    m.service_id = service_id;
    m.state_id = state_id;
    m.loc_id_raw = loc_id;
    m.loc_id = (uint8_t)((loc_id << 4) ^ (loc_id >> 4));
    m.vehicle_id = vehicle_id;
    snprintf(m.vehicle, sizeof(m.vehicle), "%1x%1x%1x%1x", vehicle_id & 0xF,
             (vehicle_id >> 4) & 0xF, (vehicle_id >> 8) & 0xF, (vehicle_id >> 12) & 0xF);
    m.status = state;
    m.model = model;
    m.direction = direction;
    m.short_info = short_info;
    m.crc = (int)((message >> 56) & 0x7F);
    m.crc_ok = crc_ok;
    m.corrected = (int)(message & 1);
    fms_pack_bits(message, m.raw);
    fms_pack_bits(received, m.received);
    m.text = fms_text;
    h->fms_message(h->ctx, &m);
}

/* ---------------------------------------------------------------------- */

void fms_init(struct demod_state *s)
{
    memset(&s->l2.uart, 0, sizeof(s->l2.uart));
    s->l2.fmsfsk.rxstate = 0;
    s->l2.fmsfsk.rxbitstream = 0;
    s->l2.fmsfsk.rxbitcount = 0;
}

/* ---------------------------------------------------------------------- */

void fms_rxbit(struct demod_state *s, int bit)
{
    uint64_t msg;
    uint64_t received;
    int i;

    // General note on performance and logic: For the generation of the
    // variable that tracks if a SYNC-frame has been received, we use
    // a << since it is significantly faster (5s vs. 16s on a 20m test wave file)
    // For the message itself, we use a >> since that makes the message easier to decode

    // Append the bit to our "state machine tracker" so we can properly detect sync bits
    s->l2.fmsfsk.rxstate = ((s->l2.fmsfsk.rxstate << 1) & 0x000FFFFE) | bit;

    // Check if the sync pattern is in the buffer
    if ((s->l2.fmsfsk.rxstate & 0x0007FFFF) == 0x7FF1A)
    {
        verbprintf(1, "FMS ->SYNC<-\n");
        s->l2.fmsfsk.rxbitstream = 0; // reset RX buffer
        s->l2.fmsfsk.rxbitcount = 1;  // > 1 means we have a valid SYNC
    }

    // If we have a valid SYNC, record the message by appending it (from the left) to the RX buffer
    else if (s->l2.fmsfsk.rxbitcount >= 1) {
        verbprintf(4, "FMS BIT: %1x\n", bit);

        s->l2.fmsfsk.rxbitstream = (s->l2.fmsfsk.rxbitstream >> 1) | ((uint64_t) bit << 63);
        s->l2.fmsfsk.rxbitcount++;

        // Wait until message has been completely received. If so, decode and display it and reset
        if (s->l2.fmsfsk.rxbitcount == 49)
        {
            received = s->l2.fmsfsk.rxbitstream; /* pagewire: keep the unrepaired packet */
            // If the CRC check fails, try to fix a one bit error
            if (!fms_is_crc_correct(s->l2.fmsfsk.rxbitstream))
            {
                i = 0;
                msg = s->l2.fmsfsk.rxbitstream;
                while (i <= 47)
                {
                    if (fms_is_crc_correct(msg ^ (1ULL << (i+16)) /* pagewire: was 1 << (i+16), undefined for i >= 15 */))
                    {
                        verbprintf(2, "FMS was able to correct a one bit error by swapping bit %d Original packet:\n", i);
                        fms_disp_packet(s, s->l2.fmsfsk.rxbitstream, s->l2.fmsfsk.rxbitstream, 0);
                        s->l2.fmsfsk.rxbitstream = (msg ^ (1ULL << (i+16))) | 1; // lowest bit set means that the CRC has been corrected by us
                        break;
                    }
                    i++;
                }
                if (i == 48)
                {
                    verbprintf(2, "FMS: unable to correct CRC error\n");
                }
            }

            fms_disp_packet(s, s->l2.fmsfsk.rxbitstream, received, 1);
            s->l2.fmsfsk.rxbitcount = 0; // Reset counter, meaning "no valid SYNC yet"
            s->l2.fmsfsk.rxstate = 0;    // Reset message input buffer
        }
    }
}

/* ---------------------------------------------------------------------- */

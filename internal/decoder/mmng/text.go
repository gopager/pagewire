//go:build cgo

package mmng

/*
#include "pagewire_hooks.h"
*/
import "C"

import "strings"

// ControlToken returns the text multimon-ng prints for the 7-bit character
// ch in its default table: "<NUL>", "<EOT>", "<DEL>" and so on for control
// characters, the character itself otherwise.
func ControlToken(ch byte) string {
	return C.GoString(C.pagewire_pocsag_default_token(C.uint(ch)))
}

// PrintedText renders decoded text the way the multimon-ng command line
// prints it: every control character (U+0000–U+001F, U+007F) becomes its
// token from multimon-ng's table; all other characters, including the
// national-charset mappings already applied by the decoder, stay as they
// are.
func PrintedText(text string) string {
	if !strings.ContainsFunc(text, isControl) {
		return text
	}
	var b strings.Builder
	b.Grow(len(text) + 16)
	for _, r := range text {
		if isControl(r) {
			b.WriteString(ControlToken(byte(r)))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func isControl(r rune) bool { return r < 0x20 || r == 0x7f }

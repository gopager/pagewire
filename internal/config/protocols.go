package config

// Protocols lists every decoder protocol name the gateway knows about, in
// lowercase. Names follow multimon-ng so documentation and recordings stay
// comparable; whether a decoder is compiled in is checked at startup by the
// decoder registry, not here.
var Protocols = []string{
	"pocsag512", "pocsag1200", "pocsag2400",
	"flex",
	"fms",
	"zvei1", "zvei2", "zvei3", "dzvei", "pzvei",
	"ccir", "eea", "eia",
	"dtmf", "dualtone",
	"eas",
	"mpt1327",
	"gsc",
}

// KnownProtocol reports whether name (lowercase) is a configurable protocol.
func KnownProtocol(name string) bool {
	for _, p := range Protocols {
		if p == name {
			return true
		}
	}
	return false
}

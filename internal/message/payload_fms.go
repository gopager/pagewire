package message

import (
	"encoding/gob"
	"encoding/hex"
)

func init() {
	gob.Register(&FMSPayload{})
}

// FMSPayload is the protocol record of an FMS (Funkmeldesystem) status
// telegram as the multimon-ng decoder reports it. The numeric fields are
// the codes as transmitted; no meaning is attached to them here. Message.Text
// holds multimon-ng's human-readable rendering of the telegram, and
// Message.Address the station identifier:
//
//	<service><state><location><vehicle>
//
// as eight lower case hex digits: service (1 digit), state (1 digit),
// location (2 digits, nibbles swapped as multimon-ng shows it, i.e.
// LocationID) and vehicle (4 digits in transmission order, i.e. Vehicle).
type FMSPayload struct {
	// ServiceID is the BOS organisation (BOS-Kennung).
	ServiceID int `json:"service_id"`
	// StateID is the country/state code (Landeskennung); for 0xe and 0xf the
	// meaning depends on the location, see Text.
	StateID int `json:"state_id"`
	// LocationRaw is the location code (Ortskennung) as transmitted;
	// LocationID is the same with its nibbles swapped (TR-BOS order).
	LocationRaw int `json:"location_raw"`
	LocationID  int `json:"location_id"`
	// VehicleID is the 16-bit vehicle code as transmitted (first bit least
	// significant); Vehicle shows its four hex digits in transmission order.
	VehicleID int    `json:"vehicle_id"`
	Vehicle   string `json:"vehicle"`
	// Status is the status code (0-15); its meaning depends on Direction.
	Status int `json:"status"`
	// Model is the build stage bit (Baustufenkennung).
	Model int `json:"model"`
	// Direction is 0 for vehicle to control centre (FZG->LST) and 1 for
	// control centre to vehicle (LST->FZG).
	Direction int `json:"direction"`
	// ShortInfo is the tactical short information (TKI, 0-3).
	ShortInfo int `json:"short_info"`
	// CRC is the 7-bit check field as transmitted (first bit least
	// significant). CRCOK says whether the telegram verified against it.
	CRC   int  `json:"crc"`
	CRCOK bool `json:"crc_ok"`
	// Corrected is set when the decoder repaired a single bit error; Raw
	// then holds the repaired telegram and Received the original bits.
	Corrected bool `json:"corrected"`
	// Raw is the 48-bit telegram after the sync pattern, in transmission
	// order, most significant bit first (6 bytes). Received is the same as
	// it came off the air.
	Raw      []byte `json:"raw"`
	Received []byte `json:"received"`
}

// RawHex returns Raw as hex digits.
func (p *FMSPayload) RawHex() string { return hex.EncodeToString(p.Raw) }

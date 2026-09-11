package gaia

import (
	"encoding/binary"
	"fmt"
)

// The equalizer payload is a count of entries followed by fixed-size entries of
// the form:
//
//	01 <id> <value hi> <value lo>
//
// The leading 0x01 appears to be a type tag for a 16-bit value; every entry
// observed in a capture of the official app used it. Entry IDs are grouped by
// band: the high nibble selects the band (1..4) and the low nibble the field.
//
//	0x01        overall preamp gain
//	0xN0        filter type      (0x000B on every band)
//	0xN1        centre frequency (Hz: 360, 1200, 9000, 18000)
//	0xN2        gain             (signed, hundredths of a dB)
//	0xN3        Q factor         (0x0B50)
//
// This was recovered by watching Sennheiser's app, not from documentation, so
// the field names are descriptions of observed behaviour.
const (
	eqEntryTag  byte = 0x01
	eqEntrySize      = 4

	// EQGlobalGain is the entry ID of the overall preamp gain.
	EQGlobalGain byte = 0x01

	// Field offsets within a band's entry IDs.
	eqFieldType byte = 0x0
	eqFieldFreq byte = 0x1
	eqFieldGain byte = 0x2
	eqFieldQ    byte = 0x3
)

// EQBands is the number of equalizer bands the device exposes.
const EQBands = 4

// EQBand is one parametric band.
type EQBand struct {
	// Type is the filter type. Every band observed used 0x000B.
	Type uint16

	// Frequency is the centre frequency in Hz.
	Frequency uint16

	// GainCentiDB is the band gain in hundredths of a decibel, so +3 dB is 300.
	// It is signed: attenuation is negative.
	GainCentiDB int16

	// Q is the filter's Q factor in the device's own units, observed as 0x0B50.
	Q uint16
}

// Gain returns the band gain in decibels.
func (b EQBand) Gain() float64 { return float64(b.GainCentiDB) / 100 }

// EQ is a full equalizer configuration.
type EQ struct {
	// GlobalGainCentiDB is an overall gain applied across the bands.
	GlobalGainCentiDB int16

	Bands [EQBands]EQBand
}

// bandEntryID builds the entry ID for a band's field. Bands are numbered from 1.
func bandEntryID(band int, field byte) byte {
	return byte(band)<<4 | field
}

// EncodeEQ builds the payload for CmdSetEQ.
func EncodeEQ(eq EQ) []byte {
	entries := make([][2]any, 0, 1+EQBands*4)
	entries = append(entries, [2]any{EQGlobalGain, uint16(eq.GlobalGainCentiDB)})

	for i, band := range eq.Bands {
		n := i + 1
		entries = append(entries,
			[2]any{bandEntryID(n, eqFieldType), band.Type},
			[2]any{bandEntryID(n, eqFieldFreq), band.Frequency},
			[2]any{bandEntryID(n, eqFieldGain), uint16(band.GainCentiDB)},
			[2]any{bandEntryID(n, eqFieldQ), band.Q},
		)
	}

	// Count, then the entries, then a two-byte terminator the app always sends.
	out := make([]byte, 2, 2+len(entries)*eqEntrySize+2)
	binary.BigEndian.PutUint16(out, uint16(len(entries)))

	for _, e := range entries {
		value := make([]byte, 2)
		binary.BigEndian.PutUint16(value, e[1].(uint16))
		out = append(out, eqEntryTag, e[0].(byte), value[0], value[1])
	}
	return append(out, 0x00, 0x00)
}

// DecodeEQ reads an equalizer payload, as returned by CmdGetEQ.
//
// Entries for fields the device did not report are left at zero, and unknown
// entry IDs are skipped rather than treated as an error: the device answers a
// read with only the fields that were asked for.
func DecodeEQ(payload []byte) (EQ, error) {
	var eq EQ

	if len(payload) < 2 {
		return eq, fmt.Errorf("gaia: equalizer payload of %d bytes is too short", len(payload))
	}

	count := int(binary.BigEndian.Uint16(payload[:2]))
	body := payload[2:]

	if len(body) < count*eqEntrySize {
		return eq, fmt.Errorf("gaia: equalizer payload claims %d entries but carries %d bytes",
			count, len(body))
	}

	for i := 0; i < count; i++ {
		entry := body[i*eqEntrySize : (i+1)*eqEntrySize]
		id := entry[1]
		value := binary.BigEndian.Uint16(entry[2:4])

		if id == EQGlobalGain {
			eq.GlobalGainCentiDB = int16(value)
			continue
		}

		band := int(id>>4) - 1
		if band < 0 || band >= EQBands {
			continue
		}

		switch id & 0x0F {
		case eqFieldType:
			eq.Bands[band].Type = value
		case eqFieldFreq:
			eq.Bands[band].Frequency = value
		case eqFieldGain:
			eq.Bands[band].GainCentiDB = int16(value)
		case eqFieldQ:
			eq.Bands[band].Q = value
		}
	}

	return eq, nil
}

// EncodeEQQuery builds the payload for CmdGetEQ, asking for one field of every
// band. The app reads the gain fields this way.
func EncodeEQQuery(field byte) []byte {
	entries := 1 + EQBands

	out := make([]byte, 2, 2+entries*eqEntrySize)
	binary.BigEndian.PutUint16(out, uint16(entries))
	out = append(out, eqEntryTag, EQGlobalGain, 0, 0)

	for band := 1; band <= EQBands; band++ {
		out = append(out, eqEntryTag, bandEntryID(band, field), 0, 0)
	}
	return out
}

// EQGainQuery is the field selector for reading band gains.
const EQGainQuery = eqFieldGain

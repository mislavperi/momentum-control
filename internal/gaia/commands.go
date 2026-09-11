package gaia

import (
	"encoding/binary"
	"fmt"
)

// Known MOMENTUM 4 command IDs.
//
// Sennheiser's protocol is undocumented; these were recovered by reverse
// engineering and confirmed against hardware. The device follows a consistent
// convention: an even command ID sets a value and the next odd ID reads it back.
// That convention is what makes read-only discovery of unknown commands safe,
// see cmd/m4ctl probe.
const (
	CmdRegisterNotification uint16 = 0x0007

	CmdSetBassBoost uint16 = 0x1008
	CmdGetBassBoost uint16 = 0x1009

	CmdSetTransparency uint16 = 0x1A02
	CmdGetTransparency uint16 = 0x1A03

	CmdSetANC uint16 = 0x1A04
	CmdGetANC uint16 = 0x1A05

	// Recovered by watching Sennheiser's own app, see cmd/m4snoop.

	// Sound mode, carrying the keys below rather than a single value.
	CmdSetSoundMode    uint16 = 0x1A00
	CmdGetSoundMode    uint16 = 0x1A01
	NotifySoundMode    uint16 = 0x1A81
	CmdGetBattery      uint16 = 0x0603
	NotifyBattery      uint16 = 0x0683
	CmdGetChargeState  uint16 = 0x0602
	NotifyChargeState  uint16 = 0x0682
	CmdGetDeviceName uint16 = 0x1206

	// CmdGetFirmware lists per-component firmware versions; CmdGetSystemVersion
	// is the release version users recognise.
	CmdGetFirmware      uint16 = 0x1202
	CmdGetSystemVersion uint16 = 0x1201
	CmdGetHardware      uint16 = 0x1200

	// CmdGetCodec reports the Bluetooth audio codec currently in use.
	CmdGetCodec uint16 = 0x0800
	CmdGetCapabilities uint16 = 0x0001

	// Equalizer. The app writes the bands with CmdSetEQ, follows with the
	// preamp, then commits.
	CmdSetEQ     uint16 = 0x1002
	CmdGetEQ     uint16 = 0x1003
	CmdCommitEQ  uint16 = 0x1004
	CmdSetPreamp uint16 = 0x1006
)

// ANC settings. The sound mode payload is a sequence of (setting, value) pairs:
// each is adjusted independently rather than one being selected out of several,
// and they do not all take the same range of values.
const (
	// AncWindNoise is wind-noise reduction, which takes three values: see the
	// WindNoise constants.
	AncWindNoise byte = 0x01

	// AncComfort was reported at 0 throughout the captures. Another
	// implementation calls it Comfort; its effect has not been confirmed here.
	AncComfort byte = 0x02

	// AncAdaptive is adaptive noise cancelling, on or off.
	AncAdaptive byte = 0x03
)

// Values of AncWindNoise.
const (
	WindNoiseOff       byte = 0x00
	WindNoiseAutomatic byte = 0x01
	WindNoiseMaximum   byte = 0x02
)

// Feature IDs accepted by CmdRegisterNotification. Registering asks the device
// to push unsolicited frames when the corresponding setting changes, for example
// when the user adjusts it via the headphone's own controls.
const (
	FeatureUserEQ       byte = 8
	FeatureTransparency byte = 12
	FeatureANC          byte = 13
)

// CommandName returns a human-readable name for a command ID, including replies.
// Unknown IDs render as hex, which keeps probe output readable.
func CommandName(cmd uint16) string {
	base := cmd &^ MaskError
	suffix := ""
	switch {
	case cmd&MaskError == MaskError:
		suffix = " [error]"
	case cmd&MaskACK == MaskACK:
		suffix = " [ack]"
	}

	name, ok := commandNames[base]
	if !ok {
		return fmt.Sprintf("%#04x%s", cmd, suffix)
	}
	return name + suffix
}

var commandNames = map[uint16]string{
	CmdRegisterNotification: "RegisterNotification",
	CmdSetBassBoost:         "SetBassBoost",
	CmdGetBassBoost:         "GetBassBoost",
	CmdSetTransparency:      "SetTransparency",
	CmdGetTransparency:      "GetTransparency",
	CmdSetANC:               "SetANC",
	CmdGetANC:               "GetANC",
	CmdSetSoundMode:         "SetSoundMode",
	CmdGetSoundMode:         "GetSoundMode",
	NotifySoundMode:         "SoundModeChanged",
	CmdGetBattery:           "GetBattery",
	NotifyBattery:           "BatteryChanged",
	CmdGetChargeState:       "GetChargeState",
	NotifyChargeState:       "ChargeStateChanged",
	CmdGetDeviceName:        "GetDeviceName",
	CmdGetFirmware:          "GetFirmware",
	CmdGetSystemVersion:     "GetSystemVersion",
	CmdGetHardware:          "GetHardware",
	CmdGetCodec:             "GetCodec",
	CmdGetCapabilities:      "GetCapabilities",
	CmdSetEQ:                "SetEQ",
	CmdGetEQ:                "GetEQ",
	CmdCommitEQ:             "CommitEQ",
	CmdSetPreamp:            "SetPreamp",
}

// IsGetter reports whether cmd follows the device's read-only (odd ID)
// convention. Probing is restricted to these so discovery cannot alter settings.
func IsGetter(cmd uint16) bool { return cmd&1 == 1 }

// codecNames maps the codec IDs the device reports. Values above the documented
// range have been observed on newer firmware.
var codecNames = map[byte]string{
	0: "SBC", 1: "AAC", 2: "aptX", 3: "aptX Low Latency", 4: "MP3",
	5: "aptX HD", 6: "FastStream", 7: "LHDC", 8: "aptX Adaptive",
	9: "aptX Lossless", 10: "LC3 (LE Audio)", 11: "aptX HD",
	255: "not streaming",
}

// CodecName renders a codec ID.
func CodecName(id byte) string {
	if name, ok := codecNames[id]; ok {
		return name
	}
	return fmt.Sprintf("unknown (%#02x)", id)
}

// ChargeStateName renders a charging status.
func ChargeStateName(state byte) string {
	switch state {
	case 0:
		return "not charging"
	case 1:
		return "charging"
	case 2:
		return "charge complete"
	default:
		return fmt.Sprintf("unknown (%#02x)", state)
	}
}

// DecodeCapabilities reads the feature list returned by CmdGetCapabilities.
//
// The payload is a 16-bit count followed by that many two-byte entries of
// feature ID and version. A trailing pair beyond the count appears in captures
// and is ignored.
func DecodeCapabilities(payload []byte) ([]struct{ Feature, Version byte }, error) {
	if len(payload) < 2 {
		return nil, fmt.Errorf("gaia: capability payload of %d bytes is too short", len(payload))
	}

	count := int(binary.BigEndian.Uint16(payload[:2]))
	body := payload[2:]
	if len(body) < count*2 {
		return nil, fmt.Errorf("gaia: capability payload claims %d features but carries %d bytes",
			count, len(body))
	}

	out := make([]struct{ Feature, Version byte }, 0, count)
	for i := 0; i < count; i++ {
		out = append(out, struct{ Feature, Version byte }{
			Feature: body[i*2+1],
			Version: body[i*2],
		})
	}
	return out, nil
}

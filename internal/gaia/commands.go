package gaia

import "fmt"

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
	CmdGetDeviceName   uint16 = 0x1206
	CmdGetFirmware     uint16 = 0x1202
	CmdGetDeviceInfo   uint16 = 0x1201
	CmdGetCapabilities uint16 = 0x0001

	// Equalizer. The app writes the bands with CmdSetEQ, follows with the
	// preamp, then commits.
	CmdSetEQ     uint16 = 0x1002
	CmdGetEQ     uint16 = 0x1003
	CmdCommitEQ  uint16 = 0x1004
	CmdSetPreamp uint16 = 0x1006
)

// Keys inside a sound mode payload, which is a sequence of key/value byte pairs.
const (
	// SoundModeKeyMode selects noise control: see the SoundMode values below.
	SoundModeKeyMode byte = 0x01

	// SoundModeKeyUnknown2 is reported by the device but its meaning has not
	// been identified; it was 0 throughout the capture.
	SoundModeKeyUnknown2 byte = 0x02

	// SoundModeKeyToggle is a second setting the app turns on and off alongside
	// the mode. Which feature it controls is not yet known.
	SoundModeKeyToggle byte = 0x03
)

// Noise control modes, the values of SoundModeKeyMode.
const (
	SoundModeOff      byte = 0x00
	SoundModeOn       byte = 0x01
	SoundModeAdaptive byte = 0x02
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
	CmdGetDeviceInfo:        "GetDeviceInfo",
	CmdGetCapabilities:      "GetCapabilities",
	CmdSetEQ:                "SetEQ",
	CmdGetEQ:                "GetEQ",
	CmdCommitEQ:             "CommitEQ",
	CmdSetPreamp:            "SetPreamp",
}

// IsGetter reports whether cmd follows the device's read-only (odd ID)
// convention. Probing is restricted to these so discovery cannot alter settings.
func IsGetter(cmd uint16) bool { return cmd&1 == 1 }

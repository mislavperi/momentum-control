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
}

// IsGetter reports whether cmd follows the device's read-only (odd ID)
// convention. Probing is restricted to these so discovery cannot alter settings.
func IsGetter(cmd uint16) bool { return cmd&1 == 1 }

package m4

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/mislavperi/momentum-control/internal/gaia"
)

// The bytes below are exactly what a MOMENTUM 4 sent in reply to Sennheiser's
// own app, taken from an HCI snoop capture. Driving the client with them checks
// the whole stack — framing, request matching, payload interpretation — against
// the real device rather than against assumptions about it.
//
// Hardware was not reachable while this was written (macOS would not open the
// control channel), so this replay is the closest thing to an integration test.
// It is not a substitute for one: it proves reads are interpreted correctly, not
// that writes do what they claim.
var capturedReplies = map[uint16]string{
	gaia.CmdGetANC:           "01",
	gaia.CmdGetTransparency:  "64",
	gaia.CmdGetBattery:       "28",
	gaia.CmdGetSoundMode:     "010202000300",
	gaia.CmdGetEQ:            "000501010000011200000122000001320000014200 00",
	gaia.CmdGetDeviceName:    "4d3441454254" + "20" + "426c61636b" + "00",
	gaia.CmdGetFirmware:      "020d2a000000",
	gaia.CmdGetSystemVersion: "0002000d002a",
	gaia.CmdGetCapabilities:  "0009010801 0c0103011001040102010a020b010d0004",
	gaia.CmdGetChargeState:   "0000",
}

// capturedErrors are the commands this firmware rejects.
var capturedErrors = map[uint16]byte{
	gaia.CmdGetBassBoost: 0x01,
}

func replayDevice(t *testing.T) *fakeDevice {
	t.Helper()

	return newFakeDevice(func(f gaia.Frame) []gaia.Frame {
		if status, rejected := capturedErrors[f.Command]; rejected {
			return []gaia.Frame{{
				Version: gaia.Version3,
				Vendor:  gaia.VendorSennheiser,
				Command: f.Command | gaia.MaskError,
				Payload: []byte{status},
			}}
		}

		raw, ok := capturedReplies[f.Command]
		if !ok {
			return []gaia.Frame{ack(f.Command)}
		}

		payload, err := hex.DecodeString(strings.ReplaceAll(raw, " ", ""))
		if err != nil {
			t.Errorf("bad captured reply for %#04x: %v", f.Command, err)
			return nil
		}
		return []gaia.Frame{ack(f.Command, payload...)}
	})
}

func TestReplayCapturedSession(t *testing.T) {
	client := newClient(replayDevice(t))
	defer client.Close()

	state, err := client.State()
	if err != nil {
		t.Fatalf("State: %v", err)
	}

	if !state.ANC {
		t.Error("ANC = off, want on")
	}
	if state.Transparency != 100 {
		t.Errorf("Transparency = %d, want 100", state.Transparency)
	}
	if state.Battery != 40 {
		t.Errorf("Battery = %d%%, want 40%%", state.Battery)
	}
	// The device reported wind-noise reduction at 2, which is automatic.
	if got := state.AncModes.WindNoise(); got != gaia.WindNoiseAutomatic {
		t.Errorf("wind noise = %s, want automatic", WindNoiseName(got))
	}
	if state.AncModes.Enabled(AncComfort) || state.AncModes.Enabled(AncAdaptive) {
		t.Errorf("AncModes = %v, want comfort and adaptive off", state.AncModes)
	}

	// Bass boost is rejected by this firmware and must not be reported.
	if state.Has(gaia.CmdGetBassBoost) {
		t.Error("bass boost reported as supported, but the device rejects it")
	}
	// Everything else the device answered must be.
	for _, cmd := range []uint16{
		gaia.CmdGetANC, gaia.CmdGetTransparency, gaia.CmdGetBattery,
		gaia.CmdGetSoundMode, gaia.CmdGetEQ,
	} {
		if !state.Has(cmd) {
			t.Errorf("%s reported as unsupported, but the device answered it", gaia.CommandName(cmd))
		}
	}
}

func TestReplayDeviceIdentity(t *testing.T) {
	client := newClient(replayDevice(t))
	defer client.Close()

	name, err := client.DeviceName()
	if err != nil {
		t.Fatalf("DeviceName: %v", err)
	}
	if name != "M4AEBT Black" {
		t.Errorf("DeviceName() = %q, want %q", name, "M4AEBT Black")
	}

	firmware, err := client.Firmware()
	if err != nil {
		t.Fatalf("Firmware: %v", err)
	}
	if firmware != "2.13.42" {
		t.Errorf("Firmware() = %q, want %q", firmware, "2.13.42")
	}
}

func TestReplayCapabilities(t *testing.T) {
	client := newClient(replayDevice(t))
	defer client.Close()

	supported, err := client.Supports(gaia.FeatureUserEQ)
	if err != nil {
		t.Fatalf("Supports: %v", err)
	}
	if !supported {
		t.Error("device did not report the equalizer feature, but the capture shows it")
	}

	// Bass boost is not in the capability list, matching its rejection above.
	caps, err := client.Capabilities()
	if err != nil {
		t.Fatalf("Capabilities: %v", err)
	}
	if len(caps) != 9 {
		t.Errorf("got %d capabilities, want 9", len(caps))
	}
}

func TestReplayEQBands(t *testing.T) {
	// The gain query returns only gains, so the bands come back at their
	// captured values with frequencies unset; what matters is that the gains
	// parse and the client does not invent data.
	client := newClient(replayDevice(t))
	defer client.Close()

	eq, err := client.EQ()
	if err != nil {
		t.Fatalf("EQ: %v", err)
	}
	for i, band := range eq.Bands {
		if band.GainCentiDB != 0 {
			t.Errorf("band %d gain = %d, want 0 (the capture was flat)", i+1, band.GainCentiDB)
		}
	}
}

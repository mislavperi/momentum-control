package m4

import (
	"bytes"
	"testing"

	"github.com/mislavperi/momentum-control/internal/gaia"
)

// settingsDevice answers single-byte settings from a store keyed by the get
// command, writes through the matching set command, and records every frame.
func settingsDevice(store map[uint16]byte, setToGet map[uint16]uint16, sent *[]gaia.Frame) *fakeDevice {
	return newFakeDevice(func(f gaia.Frame) []gaia.Frame {
		*sent = append(*sent, f)
		if get, ok := setToGet[f.Command]; ok {
			store[get] = f.Payload[0]
			return []gaia.Frame{ack(f.Command)}
		}
		if v, ok := store[f.Command]; ok {
			return []gaia.Frame{ack(f.Command, v)}
		}
		return []gaia.Frame{ack(f.Command)}
	})
}

func TestEverySettingHasAUniqueNameAndCodec(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range Settings {
		if s.Name == "" || s.Title == "" || s.read == nil || s.write == nil {
			t.Errorf("setting %+v is incomplete", s)
		}
		if seen[s.Name] {
			t.Errorf("setting %q is listed twice", s.Name)
		}
		seen[s.Name] = true
		if s.Kind == KindChoice && len(s.Choices) == 0 {
			t.Errorf("choice setting %q has no choices", s.Name)
		}
	}
}

func TestByteSettingsUseTheirOwnCommands(t *testing.T) {
	// The setter is the lower ID for some settings and the higher for others,
	// so each pair is checked against what m4.json documents.
	cases := []struct {
		name     string
		get, set uint16
		value    int
	}{
		{"on-head-detection", 0x0401, 0x0400, 0},
		{"smart-pause", 0x080D, 0x080C, 0},
		{"auto-answer", 0x080B, 0x080A, 1},
		{"comfort-call", 0x0815, 0x0814, 0},
		{"sidetone", 0x0806, 0x0805, 5},
		{"prompts", 0x0802, 0x0801, 1},
		{"touch-lock", 0x1607, 0x1606, 1},
		{"bluetooth-mode", 0x0406, 0x0405, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var sent []gaia.Frame
			store := map[uint16]byte{tc.get: 9}
			client := newClient(settingsDevice(store, map[uint16]uint16{tc.set: tc.get}, &sent))
			defer client.Close()

			s, ok := LookupSetting(tc.name)
			if !ok {
				t.Fatalf("no setting %q", tc.name)
			}
			if err := s.Write(client, tc.value); err != nil {
				t.Fatalf("Write: %v", err)
			}
			got, err := s.Read(client)
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			if got != tc.value {
				t.Errorf("read back %d, want %d", got, tc.value)
			}
			if sent[0].Command != tc.set || !bytes.Equal(sent[0].Payload, []byte{byte(tc.value)}) {
				t.Errorf("write sent %#04x % x, want %#04x %02x", sent[0].Command, sent[0].Payload, tc.set, tc.value)
			}
		})
	}
}

func TestAutoPowerOffEncodesMinutesAsSeconds(t *testing.T) {
	var sent []gaia.Frame
	device := newFakeDevice(func(f gaia.Frame) []gaia.Frame {
		sent = append(sent, f)
		if f.Command == gaia.CmdGetTimer {
			// Captured from a MOMENTUM 4: timer 0 at 900 s.
			return []gaia.Frame{ack(f.Command, 0x00, 0x03, 0x84)}
		}
		return []gaia.Frame{ack(f.Command)}
	})
	client := newClient(device)
	defer client.Close()

	s, _ := LookupSetting("auto-power-off")
	got, err := s.Read(client)
	if err != nil || got != 15 {
		t.Fatalf("Read = %d, %v; want 15", got, err)
	}
	if err := s.Write(client, 30); err != nil {
		t.Fatalf("Write: %v", err)
	}
	last := sent[len(sent)-1]
	if last.Command != gaia.CmdSetTimer || !bytes.Equal(last.Payload, []byte{0x00, 0x07, 0x08}) {
		t.Errorf("write sent %#04x % x, want 0x0600 00 07 08 (1800 s)", last.Command, last.Payload)
	}
}

func TestSettingRejectsOutOfRangeBeforeSending(t *testing.T) {
	var sent []gaia.Frame
	client := newClient(settingsDevice(map[uint16]byte{}, nil, &sent))
	defer client.Close()

	for name, v := range map[string]int{"sidetone": 6, "prompts": 3, "smart-pause": 2, "auto-power-off": 45} {
		s, _ := LookupSetting(name)
		if err := s.Write(client, v); err == nil {
			t.Errorf("%s accepted %d", name, v)
		}
	}
	if len(sent) != 0 {
		t.Errorf("invalid writes reached the device: %d frames", len(sent))
	}
}

func TestParseAndFormatRoundTrip(t *testing.T) {
	for _, tc := range []struct{ name, text string }{
		{"smart-pause", "on"}, {"smart-pause", "off"}, {"sidetone", "3"},
		{"prompts", "voice"}, {"bluetooth-mode", "audio"}, {"auto-power-off", "60"},
	} {
		s, _ := LookupSetting(tc.name)
		v, err := s.Parse(tc.text)
		if err != nil {
			t.Errorf("%s Parse(%q): %v", tc.name, tc.text, err)
			continue
		}
		if got := s.Format(v); got != tc.text {
			t.Errorf("%s Format(Parse(%q)) = %q", tc.name, tc.text, got)
		}
	}
	s, _ := LookupSetting("prompts")
	if _, err := s.Parse("loud"); err == nil {
		t.Error("prompts accepted an unknown choice")
	}
}

package rfcomm

import "testing"

func TestParseAddrAcceptsBothSeparators(t *testing.T) {
	want := [6]byte{0x80, 0xC3, 0xBA, 0x81, 0xF0, 0xE3}
	for _, in := range []string{"80:C3:BA:81:F0:E3", "80-c3-ba-81-f0-e3", " 80:c3:BA:81:f0:E3 "} {
		got, err := parseAddr(in)
		if err != nil {
			t.Errorf("parseAddr(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("parseAddr(%q) = % x, want % x", in, got, want)
		}
	}
}

func TestParseAddrRejectsMalformed(t *testing.T) {
	for _, in := range []string{"", "80:C3:BA:81:F0", "80:C3:BA:81:F0:E3:00", "zz:C3:BA:81:F0:E3"} {
		if _, err := parseAddr(in); err == nil {
			t.Errorf("parseAddr(%q) accepted a malformed address", in)
		}
	}
}

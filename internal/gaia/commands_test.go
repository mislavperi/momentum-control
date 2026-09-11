package gaia

import "testing"

func TestDecodeCapturedCapabilities(t *testing.T) {
	// Exactly what the device answered in an HCI capture.
	payload := []byte{
		0x00, 0x09,
		0x01, 0x08, 0x01, 0x0c, 0x01, 0x03, 0x01, 0x10, 0x01, 0x04,
		0x01, 0x02, 0x01, 0x0a, 0x02, 0x0b, 0x01, 0x0d,
		0x00, 0x04,
	}

	caps, err := DecodeCapabilities(payload)
	if err != nil {
		t.Fatalf("DecodeCapabilities: %v", err)
	}
	if len(caps) != 9 {
		t.Fatalf("got %d capabilities, want 9", len(caps))
	}

	// The features the app drives must be present.
	want := map[byte]bool{FeatureUserEQ: false, FeatureTransparency: false, FeatureANC: false}
	for _, c := range caps {
		if _, ok := want[c.Feature]; ok {
			want[c.Feature] = true
		}
	}
	for feature, found := range want {
		if !found {
			t.Errorf("capability list is missing feature %#02x", feature)
		}
	}

	// 0x0b was reported at version 2, everything else at version 1.
	for _, c := range caps {
		if c.Feature == 0x0b && c.Version != 2 {
			t.Errorf("feature 0x0b version = %d, want 2", c.Version)
		}
	}
}

func TestDecodeCapabilitiesRejectsTruncated(t *testing.T) {
	if _, err := DecodeCapabilities([]byte{0x00, 0x09, 0x01, 0x08}); err == nil {
		t.Error("DecodeCapabilities accepted a payload shorter than its count")
	}
}

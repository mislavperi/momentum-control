package gaia

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"
)

// captured is the payload the official app sent when every band sat at 0 dB,
// taken from an HCI snoop log. Round-tripping it is the real test: it proves the
// codec matches the device rather than matching itself.
const captured = "0011" +
	"01010000" +
	"0110000b" + "01110168" + "01120000" + "01130b50" +
	"0120000b" + "012104b0" + "01220000" + "01230b50" +
	"0130000b" + "01312328" + "01320000" + "01330b50" +
	"0140000b" + "01414650" + "01420000" + "01430b50" +
	"0000"

func mustDecodeHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		t.Fatalf("bad test data: %v", err)
	}
	return b
}

func TestDecodeCapturedEQ(t *testing.T) {
	eq, err := DecodeEQ(mustDecodeHex(t, captured))
	if err != nil {
		t.Fatalf("DecodeEQ: %v", err)
	}

	wantFreqs := [EQBands]uint16{360, 1200, 9000, 18000}
	for i, want := range wantFreqs {
		if got := eq.Bands[i].Frequency; got != want {
			t.Errorf("band %d frequency = %d, want %d", i+1, got, want)
		}
		if got := eq.Bands[i].Type; got != 0x000B {
			t.Errorf("band %d type = %#04x, want 0x000b", i+1, got)
		}
		if got := eq.Bands[i].Q; got != 0x0B50 {
			t.Errorf("band %d Q = %#04x, want 0x0b50", i+1, got)
		}
		if got := eq.Bands[i].GainCentiDB; got != 0 {
			t.Errorf("band %d gain = %d, want 0", i+1, got)
		}
	}
}

func TestEncodeMatchesCapturedBytes(t *testing.T) {
	eq, err := DecodeEQ(mustDecodeHex(t, captured))
	if err != nil {
		t.Fatalf("DecodeEQ: %v", err)
	}

	got := EncodeEQ(eq)
	want := mustDecodeHex(t, captured)

	if !bytes.Equal(got, want) {
		t.Errorf("EncodeEQ produced bytes the device was never sent:\n got % x\nwant % x", got, want)
	}
}

func TestDecodeNegativeGain(t *testing.T) {
	// From the capture: band 2 pulled down while the app was being driven.
	// 0xfed4 is -300, i.e. -3 dB.
	payload := mustDecodeHex(t, "0002"+"01010102"+"0122fed4")

	eq, err := DecodeEQ(payload)
	if err != nil {
		t.Fatalf("DecodeEQ: %v", err)
	}

	if eq.Bands[1].GainCentiDB != -300 {
		t.Errorf("band 2 gain = %d, want -300", eq.Bands[1].GainCentiDB)
	}
	if got := eq.Bands[1].Gain(); got != -3 {
		t.Errorf("band 2 Gain() = %v dB, want -3", got)
	}
	if eq.GlobalGainCentiDB != 258 {
		t.Errorf("global gain = %d, want 258", eq.GlobalGainCentiDB)
	}
}

func TestGainRoundTripsThroughSignedValues(t *testing.T) {
	var eq EQ
	eq.Bands[0].GainCentiDB = -420
	eq.Bands[3].GainCentiDB = 720

	got, err := DecodeEQ(EncodeEQ(eq))
	if err != nil {
		t.Fatalf("DecodeEQ: %v", err)
	}
	if got.Bands[0].GainCentiDB != -420 || got.Bands[3].GainCentiDB != 720 {
		t.Errorf("gains round-tripped to %d and %d, want -420 and 720",
			got.Bands[0].GainCentiDB, got.Bands[3].GainCentiDB)
	}
}

func TestDecodeRejectsTruncatedPayload(t *testing.T) {
	// Claims four entries, carries one.
	if _, err := DecodeEQ(mustDecodeHex(t, "0004"+"01010000")); err == nil {
		t.Error("DecodeEQ accepted a payload shorter than its own count")
	}
	if _, err := DecodeEQ([]byte{0x00}); err == nil {
		t.Error("DecodeEQ accepted a one-byte payload")
	}
}

func TestEncodeEQQueryAsksForEveryBand(t *testing.T) {
	// The app reads gains with exactly this shape.
	want := mustDecodeHex(t, "0005"+"01010000"+"01120000"+"01220000"+"01320000"+"01420000")

	if got := EncodeEQQuery(EQGainQuery); !bytes.Equal(got, want) {
		t.Errorf("EncodeEQQuery() = % x, want % x", got, want)
	}
}

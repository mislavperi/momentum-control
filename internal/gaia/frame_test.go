package gaia

import (
	"bytes"
	"errors"
	"testing"
)

func TestEncodeGetANC(t *testing.T) {
	got, err := Command(CmdGetANC).Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	want := []byte{0xFF, 0x03, 0x00, 0x00, 0x04, 0x95, 0x1A, 0x05}
	if !bytes.Equal(got, want) {
		t.Errorf("Encode() = % x, want % x", got, want)
	}
}

func TestEncodeWithPayload(t *testing.T) {
	got, err := Command(CmdSetTransparency, 42).Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	want := []byte{0xFF, 0x03, 0x00, 0x01, 0x04, 0x95, 0x1A, 0x02, 42}
	if !bytes.Equal(got, want) {
		t.Errorf("Encode() = % x, want % x", got, want)
	}
}

func TestEncodeRejectsOversizedPayload(t *testing.T) {
	f := Command(CmdSetTransparency, make([]byte, MaxPayload+1)...)
	if _, err := f.Encode(); err == nil {
		t.Fatal("Encode() of oversized payload succeeded, want error")
	}
}

func TestChecksumRoundTrip(t *testing.T) {
	f := Command(CmdSetANC, 1)
	f.Flags = FlagCheck

	raw, err := f.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	got, n, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if n != len(raw) {
		t.Errorf("Decode consumed %d bytes, want %d", n, len(raw))
	}
	if got.Command != CmdSetANC || !bytes.Equal(got.Payload, []byte{1}) {
		t.Errorf("Decode() = %v, want cmd %#04x payload [1]", got, CmdSetANC)
	}

	raw[len(raw)-1] ^= 0xFF
	if _, _, err := Decode(raw); !errors.Is(err, ErrChecksum) {
		t.Errorf("Decode of corrupted checksum = %v, want ErrChecksum", err)
	}
}

func TestDecodeShort(t *testing.T) {
	full := []byte{0xFF, 0x03, 0x00, 0x02, 0x04, 0x95, 0x1A, 0x05, 0x01, 0x02}
	for n := range full {
		if _, _, err := Decode(full[:n]); !errors.Is(err, ErrShort) {
			t.Errorf("Decode of %d bytes = %v, want ErrShort", n, err)
		}
	}
	if _, n, err := Decode(full); err != nil || n != len(full) {
		t.Errorf("Decode(full) = %d, %v; want %d, nil", n, err, len(full))
	}
}

func TestScanSplitsStream(t *testing.T) {
	a, _ := Command(CmdGetANC).Encode()
	b, _ := Command(CmdSetTransparency, 50).Encode()

	stream := append(append([]byte{}, a...), b...)
	stream = append(stream, a[:4]...) // trailing partial frame

	frames, rest := Scan(stream)
	if len(frames) != 2 {
		t.Fatalf("Scan returned %d frames, want 2", len(frames))
	}
	if frames[0].Command != CmdGetANC || frames[1].Command != CmdSetTransparency {
		t.Errorf("Scan returned %v, %v", frames[0], frames[1])
	}
	if !bytes.Equal(rest, a[:4]) {
		t.Errorf("Scan leftover = % x, want % x", rest, a[:4])
	}
}

func TestScanResyncsAfterGarbage(t *testing.T) {
	good, _ := Command(CmdGetANC).Encode()
	stream := append([]byte{0x11, 0x22, 0x33}, good...)

	frames, rest := Scan(stream)
	if len(frames) != 1 || frames[0].Command != CmdGetANC {
		t.Fatalf("Scan after garbage returned %v", frames)
	}
	if len(rest) != 0 {
		t.Errorf("Scan leftover = % x, want empty", rest)
	}
}

func TestIsReplyDistinguishesDeviceInitiatedFrames(t *testing.T) {
	// A device-initiated frame carrying a command ID must not look like a reply.
	if (Frame{Command: CmdGetANC}).IsReply() {
		t.Error("a plain command frame reported itself as a reply")
	}
	if !(Frame{Command: CmdGetANC | MaskACK}).IsReply() {
		t.Error("an ack frame did not report itself as a reply")
	}
	if !(Frame{Command: CmdGetANC | MaskError}).IsReply() {
		t.Error("an error frame did not report itself as a reply")
	}
}

func TestReplyClassification(t *testing.T) {
	ack := Frame{Command: CmdGetANC | MaskACK}
	if !ack.IsACK(CmdGetANC) || ack.IsError(CmdGetANC) {
		t.Error("ack frame misclassified")
	}

	fail := Frame{Command: CmdGetANC | MaskError, Payload: []byte{0x05}}
	if !fail.IsError(CmdGetANC) || fail.IsACK(CmdGetANC) {
		t.Error("error frame misclassified")
	}
	if fail.Status() != 0x05 {
		t.Errorf("Status() = %#x, want 0x05", fail.Status())
	}
	if fail.Base() != CmdGetANC {
		t.Errorf("Base() = %#04x, want %#04x", fail.Base(), CmdGetANC)
	}
}

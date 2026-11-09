package main

import (
	"encoding/binary"
	"testing"

	"github.com/mislavperi/momentum-control/internal/gaia"
)

// buildCapture assembles a minimal btsnoop file containing the given payloads.
func buildCapture(t *testing.T, payloads [][]byte, sent []bool) []byte {
	t.Helper()

	out := append([]byte(snoopMagic), make([]byte, 8)...) // header: magic + version + datalink

	for i, payload := range payloads {
		record := make([]byte, recordHeader)
		binary.BigEndian.PutUint32(record[0:4], uint32(len(payload)))
		binary.BigEndian.PutUint32(record[4:8], uint32(len(payload)))

		var flags uint32 = 1 // received
		if sent[i] {
			flags = 0
		}
		binary.BigEndian.PutUint32(record[8:12], flags)
		binary.BigEndian.PutUint64(record[16:24], epochOffsetMicros)

		out = append(out, record...)
		out = append(out, payload...)
	}
	return out
}

func TestExtractFindsFramesAndDirection(t *testing.T) {
	request, _ := gaia.Command(gaia.CmdGetANC).Encode()
	reply, _ := gaia.Frame{
		Version: gaia.Version3,
		Vendor:  gaia.VendorSennheiser,
		Command: gaia.CmdGetANC | gaia.MaskACK,
		Payload: []byte{1},
	}.Encode()

	// Frames sit behind ACL/L2CAP/RFCOMM headers, so pad the front.
	padded := func(frame []byte) []byte {
		return append([]byte{0x02, 0x40, 0x20, 0x0c, 0x08, 0x00, 0x41, 0x00, 0xff, 0x09}, frame...)
	}

	data := buildCapture(t,
		[][]byte{padded(request), padded(reply)},
		[]bool{true, false})

	captures, err := extract(data[snoopHeaderSize:])
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(captures) != 2 {
		t.Fatalf("extract found %d frames, want 2", len(captures))
	}

	if !captures[0].fromApp || captures[0].frame.Command != gaia.CmdGetANC {
		t.Errorf("first frame = %+v, want GetANC sent by the app", captures[0])
	}
	if captures[1].fromApp || captures[1].frame.Command != gaia.CmdGetANC|gaia.MaskACK {
		t.Errorf("second frame = %+v, want the device's reply", captures[1])
	}
}

func TestExtractIgnoresOtherVendors(t *testing.T) {
	// Channel 14 on these headphones speaks an unrelated protocol that also
	// starts with 0xFF; it must not be mistaken for a control frame.
	airoha := []byte{0xff, 0x55, 0x02, 0x00, 0xee, 0x10}

	other, _ := gaia.Frame{
		Version: gaia.Version3,
		Vendor:  gaia.VendorQualcomm,
		Command: 0x0123,
	}.Encode()

	data := buildCapture(t, [][]byte{airoha, other}, []bool{true, true})

	captures, err := extract(data[snoopHeaderSize:])
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(captures) != 0 {
		t.Errorf("extract found %d frames, want none", len(captures))
	}
}

func TestExtractStopsAtTruncatedRecord(t *testing.T) {
	frame, _ := gaia.Command(gaia.CmdGetANC).Encode()
	data := buildCapture(t, [][]byte{frame}, []bool{true})

	// A capture still being written ends mid-record; that must not be an error.
	truncated := data[:len(data)-3]

	captures, err := extract(truncated[snoopHeaderSize:])
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(captures) != 0 {
		t.Errorf("extract returned %d frames from a truncated record, want none", len(captures))
	}
}

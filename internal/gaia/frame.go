// Package gaia implements the GAIA-style framing used by Sennheiser MOMENTUM 4
// headphones over a Bluetooth Classic RFCOMM channel.
//
// The wire format is:
//
//	 0      1        2       3        4..5       6..7      8..8+n   [+1]
//	+------+--------+-------+--------+----------+---------+--------+------+
//	| 0xFF | VERSION| FLAGS | LENGTH | VENDOR   | COMMAND | PAYLOAD| CHECK|
//	+------+--------+-------+--------+----------+---------+--------+------+
//
// Multi-byte fields are big endian. LENGTH counts payload bytes only. The CHECK
// byte is present only when FLAGS bit 0 is set, and is the XOR of every
// preceding byte in the frame.
package gaia

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	// SOF is the start-of-frame marker.
	SOF byte = 0xFF
	// Version3 is the protocol version the MOMENTUM 4 speaks.
	Version3 byte = 0x03

	// FlagCheck indicates a trailing XOR checksum byte.
	FlagCheck byte = 0x01

	// HeaderLen is the fixed part of a frame, excluding payload and checksum.
	HeaderLen = 8

	// VendorSennheiser is the vendor ID used by MOMENTUM 4 control commands.
	VendorSennheiser uint16 = 0x0495
	// VendorQualcomm is the standard GAIA vendor ID, used by generic commands.
	VendorQualcomm uint16 = 0x000A
)

// Response and error masks. A device reply to command C arrives as C|MaskACK on
// success, or C|MaskError on failure (in which case the first payload byte is a
// status code).
const (
	MaskACK   uint16 = 0x0100
	MaskError uint16 = 0x0180
)

var (
	// ErrShort means the buffer does not yet hold a complete frame.
	ErrShort = errors.New("gaia: short buffer")
	// ErrNoSOF means the buffer does not start at a frame boundary.
	ErrNoSOF = errors.New("gaia: missing start of frame")
	// ErrChecksum means the trailing checksum byte did not verify.
	ErrChecksum = errors.New("gaia: bad checksum")
)

// MaxPayload is the largest payload a single frame can carry, bounded by the
// one-byte LENGTH field.
const MaxPayload = 255

// Frame is one decoded GAIA packet.
type Frame struct {
	Version byte
	Flags   byte
	Vendor  uint16
	Command uint16
	Payload []byte
}

// IsACK reports whether f is a success reply to req.
func (f Frame) IsACK(req uint16) bool { return f.Command == req|MaskACK }

// IsError reports whether f is an error reply to req.
func (f Frame) IsError(req uint16) bool { return f.Command == req|MaskError }

// Status returns the error status byte of an error reply, or 0 if absent.
func (f Frame) Status() byte {
	if len(f.Payload) == 0 {
		return 0
	}
	return f.Payload[0]
}

// Base strips the ACK/error mask, yielding the command the frame responds to.
func (f Frame) Base() uint16 { return f.Command &^ MaskError }

func (f Frame) String() string {
	return fmt.Sprintf("vendor=%#04x cmd=%#04x payload=% x", f.Vendor, f.Command, f.Payload)
}

// Encode serialises f. A checksum byte is appended when FlagCheck is set in
// f.Flags. It returns an error if the payload exceeds MaxPayload.
func (f Frame) Encode() ([]byte, error) {
	if len(f.Payload) > MaxPayload {
		return nil, fmt.Errorf("gaia: payload of %d bytes exceeds maximum of %d", len(f.Payload), MaxPayload)
	}
	version := f.Version
	if version == 0 {
		version = Version3
	}

	out := make([]byte, HeaderLen, HeaderLen+len(f.Payload)+1)
	out[0] = SOF
	out[1] = version
	out[2] = f.Flags
	out[3] = byte(len(f.Payload))
	binary.BigEndian.PutUint16(out[4:6], f.Vendor)
	binary.BigEndian.PutUint16(out[6:8], f.Command)
	out = append(out, f.Payload...)

	if f.Flags&FlagCheck != 0 {
		out = append(out, checksum(out))
	}
	return out, nil
}

// Command builds a Sennheiser-vendor frame for cmd with the given payload.
func Command(cmd uint16, payload ...byte) Frame {
	return Frame{Version: Version3, Vendor: VendorSennheiser, Command: cmd, Payload: payload}
}

// Decode reads the first frame from buf. It returns the frame and the number of
// bytes consumed. If buf holds only part of a frame it returns ErrShort, and the
// caller should read more data and retry with the same buffer.
func Decode(buf []byte) (Frame, int, error) {
	if len(buf) < HeaderLen {
		return Frame{}, 0, ErrShort
	}
	if buf[0] != SOF {
		return Frame{}, 0, ErrNoSOF
	}

	f := Frame{
		Version: buf[1],
		Flags:   buf[2],
		Vendor:  binary.BigEndian.Uint16(buf[4:6]),
		Command: binary.BigEndian.Uint16(buf[6:8]),
	}

	total := HeaderLen + int(buf[3])
	if f.Flags&FlagCheck != 0 {
		total++
	}
	if len(buf) < total {
		return Frame{}, 0, ErrShort
	}
	if f.Flags&FlagCheck != 0 && checksum(buf[:total-1]) != buf[total-1] {
		return Frame{}, 0, ErrChecksum
	}

	if n := int(buf[3]); n > 0 {
		f.Payload = append([]byte(nil), buf[HeaderLen:HeaderLen+n]...)
	}
	return f, total, nil
}

// Scan decodes every complete frame in buf. It returns the frames along with any
// trailing bytes that did not form a complete frame, which the caller should
// carry over to the next read.
//
// Bytes that do not begin at a frame boundary are skipped by resynchronising on
// the next SOF marker, so a corrupt or truncated frame costs at most one packet.
func Scan(buf []byte) ([]Frame, []byte) {
	var frames []Frame
	for len(buf) > 0 {
		f, n, err := Decode(buf)
		switch {
		case err == nil:
			frames = append(frames, f)
			buf = buf[n:]
		case errors.Is(err, ErrShort):
			return frames, buf
		default:
			// Corrupt frame: resynchronise on the next SOF.
			next := indexSOF(buf[1:])
			if next < 0 {
				return frames, nil
			}
			buf = buf[1+next:]
		}
	}
	return frames, nil
}

func indexSOF(buf []byte) int {
	for i, b := range buf {
		if b == SOF {
			return i
		}
	}
	return -1
}

func checksum(b []byte) byte {
	var c byte
	for _, v := range b {
		c ^= v
	}
	return c
}

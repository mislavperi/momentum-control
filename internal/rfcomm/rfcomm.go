// Package rfcomm opens Bluetooth Classic RFCOMM channels.
//
// Each platform reaches RFCOMM differently, so the implementation lives in one
// file per operating system behind the same API:
//
//	macOS    IOBluetooth through cgo and Objective-C (rfcomm_darwin.*)
//	Linux    AF_BLUETOOTH sockets, pure Go          (rfcomm_linux.go)
//	Windows  AF_BTH Winsock sockets, pure Go        (rfcomm_windows.go)
//
// Every platform provides Dial, Conn (Read, Write, Close, ChannelID),
// PairedDevices, OnDeviceConnected and Main. Anything else builds against a
// stub that reports ErrUnsupported.
package rfcomm

import (
	"errors"
	"time"
)

// SennheiserControlUUID is the SDP service UUID of the MOMENTUM 4 control
// channel: A2129FF3-081B-4C45-8AFE-469D9C4842EC.
var SennheiserControlUUID = [16]byte{
	0xA2, 0x12, 0x9F, 0xF3, 0x08, 0x1B, 0x4C, 0x45,
	0x8A, 0xFE, 0x46, 0x9D, 0x9C, 0x48, 0x42, 0xEC,
}

// openTimeout is the default budget for Dial. Opening includes an SDP query and
// the Bluetooth link coming up, so it is generous.
const openTimeout = 15 * time.Second

// Options configures Dial.
type Options struct {
	// UUID is the SDP service UUID to look up. Defaults to SennheiserControlUUID.
	UUID *[16]byte

	// Channel, when non-zero, connects to that RFCOMM channel number directly and
	// skips SDP discovery. Useful when a device does not advertise its service.
	Channel int

	// Timeout bounds the open. Zero means the default, which is generous enough
	// for an SDP query plus the link coming up.
	Timeout time.Duration
}

func (o Options) uuid() [16]byte {
	if o.UUID != nil {
		return *o.UUID
	}
	return SennheiserControlUUID
}

func (o Options) timeout() time.Duration {
	if o.Timeout > 0 {
		return o.Timeout
	}
	return openTimeout
}

// Device is a Bluetooth device paired with this computer.
type Device struct {
	Address string
	Name    string
}

// ErrClosed is returned by Read and Write once the channel has closed, either
// locally or because the headphones went away.
var ErrClosed = errors.New("rfcomm: channel closed")

// ErrNoMainLoop is returned by Dial on macOS when the main dispatch queue is not
// being serviced, since no IOBluetooth callback could be delivered.
var ErrNoMainLoop = errors.New(
	"rfcomm: the main dispatch queue is not being serviced, so no Bluetooth callback can arrive; wrap the program in rfcomm.Main, or dial once the GUI toolkit has started its event loop")

// ErrNoSDP is returned by Dial when no channel was given and this platform
// cannot look the service up. Callers fall back to scanning channels, which
// they do anyway because the device's SDP record goes stale.
var ErrNoSDP = errors.New("rfcomm: service lookup is not supported on this platform; pass a channel")

// ErrUnsupported is returned on platforms with no RFCOMM implementation.
var ErrUnsupported = errors.New("rfcomm: Bluetooth Classic is not supported on this platform")

// parseAddr reads a Bluetooth address written with colons or dashes into its
// six bytes, most significant first.
func parseAddr(addr string) ([6]byte, error) {
	var out [6]byte
	hex := make([]byte, 0, 12)
	for i := 0; i < len(addr); i++ {
		switch c := addr[i]; {
		case c == ':' || c == '-' || c == ' ':
		case '0' <= c && c <= '9', 'a' <= c && c <= 'f', 'A' <= c && c <= 'F':
			hex = append(hex, c)
		default:
			return out, errors.New("rfcomm: malformed Bluetooth address " + addr)
		}
	}
	if len(hex) != 12 {
		return out, errors.New("rfcomm: malformed Bluetooth address " + addr)
	}
	for i := range out {
		out[i] = unhex(hex[2*i])<<4 | unhex(hex[2*i+1])
	}
	return out, nil
}

func unhex(c byte) byte {
	switch {
	case c >= 'a':
		return c - 'a' + 10
	case c >= 'A':
		return c - 'A' + 10
	}
	return c - '0'
}

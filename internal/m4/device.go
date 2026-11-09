package m4

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/mislavperi/momentum-control/internal/gaia"
)

// Battery reports the charge level as a percentage.
func (c *Client) Battery() (int, error) {
	f, err := c.requestTimeout(gaia.CmdGetBattery)
	if err != nil {
		return 0, err
	}
	b, err := payloadByte(f, gaia.CmdGetBattery)
	return int(b), err
}

// DeviceName reports the headphones' own name, e.g. "M4AEBT Black".
func (c *Client) DeviceName() (string, error) {
	f, err := c.requestTimeout(gaia.CmdGetDeviceName)
	if err != nil {
		return "", err
	}
	// The name is NUL-terminated ASCII.
	return string(bytes.TrimRight(f.Payload, "\x00")), nil
}

// Firmware reports the system release version as major.minor.patch.
//
// The device reports it as three 16-bit values, and separately exposes a list of
// per-component firmware versions that this does not use.
func (c *Client) Firmware() (string, error) {
	f, err := c.requestTimeout(gaia.CmdGetSystemVersion)
	if err != nil {
		return "", err
	}
	if len(f.Payload) < 6 {
		return "", fmt.Errorf("m4: version reply carried %d bytes, want at least 6", len(f.Payload))
	}
	return fmt.Sprintf("%d.%d.%d",
		binary.BigEndian.Uint16(f.Payload[0:2]),
		binary.BigEndian.Uint16(f.Payload[2:4]),
		binary.BigEndian.Uint16(f.Payload[4:6])), nil
}

// Codec reports the Bluetooth audio codec in use.
func (c *Client) Codec() (string, error) {
	f, err := c.requestTimeout(gaia.CmdGetCodec)
	if err != nil {
		return "", err
	}
	b, err := payloadByte(f, gaia.CmdGetCodec)
	if err != nil {
		return "", err
	}
	return gaia.CodecName(b), nil
}

// ChargeState reports whether the headphones are charging.
func (c *Client) ChargeState() (string, error) {
	f, err := c.requestTimeout(gaia.CmdGetChargeState)
	if err != nil {
		return "", err
	}
	b, err := payloadByte(f, gaia.CmdGetChargeState)
	if err != nil {
		return "", err
	}
	return gaia.ChargeStateName(b), nil
}

// Capability is a feature the device reports as supported.
type Capability struct {
	Feature byte
	Version byte
}

// Capabilities asks the device which features it implements.
//
// This is better than inferring support from which commands answer: the device
// states it directly, so a UI can be built from the device's own list rather
// than from trial and error.
func (c *Client) Capabilities() ([]Capability, error) {
	f, err := c.requestTimeout(gaia.CmdGetCapabilities)
	if err != nil {
		return nil, err
	}

	raw, err := gaia.DecodeCapabilities(f.Payload)
	if err != nil {
		return nil, err
	}

	caps := make([]Capability, 0, len(raw))
	for _, r := range raw {
		caps = append(caps, Capability{Feature: r.Feature, Version: r.Version})
	}
	return caps, nil
}

// Supports reports whether the device listed the given feature.
func (c *Client) Supports(feature byte) (bool, error) {
	caps, err := c.Capabilities()
	if err != nil {
		return false, err
	}
	for _, capability := range caps {
		if capability.Feature == feature {
			return true, nil
		}
	}
	return false, nil
}

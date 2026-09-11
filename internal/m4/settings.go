package m4

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/mislavperi/momentum4-control/internal/gaia"
)

// State is a snapshot of the settings the device exposes.
//
// Not every MOMENTUM 4 supports every command: firmware varies, and an
// unsupported command is answered with an error rather than silence. The
// Supported set records which settings the device actually answered, so a UI can
// hide what this particular device does not have.
type State struct {
	ANC          bool
	Transparency int // 0..100, higher lets more of the outside world in
	BassBoost    bool
	Battery      int // charge level as a percentage
	NoiseMode    NoiseMode
	EQ           gaia.EQ
	ChannelID    int

	// Supported holds the get-commands the device answered.
	Supported map[uint16]bool
}

// Has reports whether the device answered the given get-command.
func (s State) Has(cmd uint16) bool { return s.Supported[cmd] }

// ANC reports whether active noise cancellation is on.
func (c *Client) ANC() (bool, error) {
	f, err := c.requestTimeout(gaia.CmdGetANC)
	if err != nil {
		return false, err
	}
	return payloadBool(f, gaia.CmdGetANC)
}

// SetANC turns active noise cancellation on or off.
func (c *Client) SetANC(on bool) error {
	_, err := c.requestTimeout(gaia.CmdSetANC, boolByte(on))
	return err
}

// Transparency reports the transparency level, 0..100.
func (c *Client) Transparency() (int, error) {
	f, err := c.requestTimeout(gaia.CmdGetTransparency)
	if err != nil {
		return 0, err
	}
	v, err := payloadByte(f, gaia.CmdGetTransparency)
	return int(v), err
}

// SetTransparency sets the transparency level. Values outside 0..100 are
// clamped rather than rejected, so a UI slider can pass its raw value.
func (c *Client) SetTransparency(level int) error {
	_, err := c.requestTimeout(gaia.CmdSetTransparency, byte(clamp(level, 0, 100)))
	return err
}

// BassBoost reports whether the bass boost EQ preset is on.
func (c *Client) BassBoost() (bool, error) {
	f, err := c.requestTimeout(gaia.CmdGetBassBoost)
	if err != nil {
		return false, err
	}
	return payloadBool(f, gaia.CmdGetBassBoost)
}

// SetBassBoost turns the bass boost EQ preset on or off.
func (c *Client) SetBassBoost(on bool) error {
	_, err := c.requestTimeout(gaia.CmdSetBassBoost, boolByte(on))
	return err
}

// State reads every known setting in one pass.
//
// A setting the device rejects is left at its zero value and omitted from
// Supported; only a transport failure, which means the connection itself is
// gone, is returned as an error. Otherwise one unsupported command would hide
// every setting that does work.
func (c *Client) State() (State, error) {
	s := State{ChannelID: c.ChannelID(), Supported: map[uint16]bool{}}

	read := func(cmd uint16, store func(byte)) error {
		f, err := c.request(readTimeout, cmd)
		if err != nil {
			if isUnsupported(err) {
				return nil // not implemented by this firmware
			}
			return err
		}
		b, err := payloadByte(f, cmd)
		if err != nil {
			return nil // answered, but with nothing usable
		}
		s.Supported[cmd] = true
		store(b)
		return nil
	}

	if err := read(gaia.CmdGetANC, func(b byte) { s.ANC = b != 0 }); err != nil {
		return s, err
	}
	if err := read(gaia.CmdGetTransparency, func(b byte) { s.Transparency = int(b) }); err != nil {
		return s, err
	}
	if err := read(gaia.CmdGetBassBoost, func(b byte) { s.BassBoost = b != 0 }); err != nil {
		return s, err
	}
	if err := read(gaia.CmdGetBattery, func(b byte) { s.Battery = int(b) }); err != nil {
		return s, err
	}

	// The mode and equalizer replies are structured, so they do not fit the
	// single-byte reader above.
	if mode, err := c.noiseMode(readTimeout); err == nil {
		s.NoiseMode = mode
		s.Supported[gaia.CmdGetSoundMode] = true
	} else if !isUnsupported(err) {
		return s, err
	}

	if eq, err := c.eq(readTimeout); err == nil {
		s.EQ = eq
		s.Supported[gaia.CmdGetEQ] = true
	} else if !isUnsupported(err) {
		return s, err
	}

	// Silence from every single command means the connection is gone, not that
	// the device implements nothing.
	if len(s.Supported) == 0 {
		if err := c.lastFatal(); err != nil {
			return s, err
		}
		return s, ErrNotConnected
	}

	return s, nil
}

// readTimeout bounds each read in State. It is shorter than DefaultTimeout
// because a State call makes several reads, and some of them are expected to go
// unanswered. It is a variable so tests need not wait out real timeouts.
var readTimeout = 1500 * time.Millisecond

// isUnsupported reports whether a failure means the device does not implement
// the command, rather than the connection being broken.
//
// The device signals "not implemented" two ways: an error reply, or silence.
// Silence is indistinguishable from a lost connection at this level, so callers
// that need to tell them apart check the client's terminal error.
func isUnsupported(err error) bool {
	var devErr *DeviceError
	return errors.As(err, &devErr) || errors.Is(err, context.DeadlineExceeded)
}

// RegisterNotifications asks the device to push updates when settings change on
// the headphones themselves. Features the device does not accept are skipped, so
// a rejection is not treated as fatal.
func (c *Client) RegisterNotifications(features ...byte) {
	if len(features) == 0 {
		features = []byte{gaia.FeatureUserEQ, gaia.FeatureTransparency, gaia.FeatureANC}
	}
	for _, f := range features {
		_, _ = c.requestTimeout(gaia.CmdRegisterNotification, f)
	}
}

func payloadByte(f gaia.Frame, cmd uint16) (byte, error) {
	if len(f.Payload) == 0 {
		return 0, fmt.Errorf("m4: %s returned an empty payload", gaia.CommandName(cmd))
	}
	return f.Payload[0], nil
}

func payloadBool(f gaia.Frame, cmd uint16) (bool, error) {
	b, err := payloadByte(f, cmd)
	return b != 0, err
}

func boolByte(b bool) byte {
	if b {
		return 1
	}
	return 0
}

func clamp(v, lo, hi int) int {
	switch {
	case v < lo:
		return lo
	case v > hi:
		return hi
	}
	return v
}

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

// Firmware reports the firmware version as major.minor.patch.
func (c *Client) Firmware() (string, error) {
	f, err := c.requestTimeout(gaia.CmdGetFirmware)
	if err != nil {
		return "", err
	}
	if len(f.Payload) < 3 {
		return "", fmt.Errorf("m4: firmware reply carried %d bytes, want at least 3", len(f.Payload))
	}
	return fmt.Sprintf("%d.%d.%d", f.Payload[0], f.Payload[1], f.Payload[2]), nil
}

// NoiseMode is the headphones' noise control setting.
type NoiseMode byte

const (
	NoiseOff      = NoiseMode(gaia.SoundModeOff)
	NoiseOn       = NoiseMode(gaia.SoundModeOn)
	NoiseAdaptive = NoiseMode(gaia.SoundModeAdaptive)
)

func (m NoiseMode) String() string {
	switch m {
	case NoiseOff:
		return "off"
	case NoiseOn:
		return "on"
	case NoiseAdaptive:
		return "adaptive"
	default:
		return fmt.Sprintf("unknown(%d)", byte(m))
	}
}

// NoiseMode reads the current noise control mode.
//
// The device answers with a sequence of key/value pairs rather than a single
// value; only the mode key is interpreted here, since the others have not been
// identified.
func (c *Client) NoiseMode() (NoiseMode, error) {
	return c.noiseMode(DefaultTimeout)
}

func (c *Client) noiseMode(timeout time.Duration) (NoiseMode, error) {
	f, err := c.request(timeout, gaia.CmdGetSoundMode)
	if err != nil {
		return NoiseOff, err
	}
	value, ok := soundModeValue(f.Payload, gaia.SoundModeKeyMode)
	if !ok {
		return NoiseOff, fmt.Errorf("m4: sound mode reply carried no mode key: % x", f.Payload)
	}
	return NoiseMode(value), nil
}

// SetNoiseMode selects noise cancelling, adaptive, or off.
func (c *Client) SetNoiseMode(m NoiseMode) error {
	_, err := c.requestTimeout(gaia.CmdSetSoundMode, gaia.SoundModeKeyMode, byte(m))
	return err
}

// soundModeValue picks one key out of a sound mode payload.
func soundModeValue(payload []byte, key byte) (byte, bool) {
	for i := 0; i+1 < len(payload); i += 2 {
		if payload[i] == key {
			return payload[i+1], true
		}
	}
	return 0, false
}

// EQ reads the equalizer configuration.
func (c *Client) EQ() (gaia.EQ, error) {
	return c.eq(DefaultTimeout)
}

func (c *Client) eq(timeout time.Duration) (gaia.EQ, error) {
	f, err := c.request(timeout, gaia.CmdGetEQ, gaia.EncodeEQQuery(gaia.EQGainQuery)...)
	if err != nil {
		return gaia.EQ{}, err
	}
	return gaia.DecodeEQ(f.Payload)
}

// SetEQ writes the equalizer configuration.
//
// The device needs three commands in order, which is how the official app does
// it: write the bands, set the matching preamp, then commit. Skipping the commit
// leaves the change inaudible.
func (c *Client) SetEQ(eq gaia.EQ) error {
	if _, err := c.requestTimeout(gaia.CmdSetEQ, gaia.EncodeEQ(eq)...); err != nil {
		return err
	}

	// The app sends a preamp byte alongside the bands, scaled from the global
	// gain, to keep boosted bands from clipping.
	preamp := byte(int8(clamp(int(eq.GlobalGainCentiDB/100), -128, 127)))
	if _, err := c.requestTimeout(gaia.CmdSetPreamp, preamp); err != nil {
		return err
	}

	_, err := c.requestTimeout(gaia.CmdCommitEQ, 0x00, 0x01, 0x40, 0x00)
	return err
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

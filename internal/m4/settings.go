package m4

import (
	"bytes"
	"context"
	"encoding/binary"
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
	// TransparentHearing is the switch that actually pipes the outside world in.
	TransparentHearing bool
	// HearingPausesMusic is whether turning Transparent Hearing on pauses playback.
	HearingPausesMusic bool
	BassBoost          bool
	Battery            int // charge level as a percentage
	AncModes           AncModes
	EQ                 gaia.EQ
	ChannelID          int

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

// Transparency reports the level from full noise cancelling to full
// transparency: 0 lets nothing through, 100 lets the outside world in.
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

// TransparentHearing reports whether Transparent Hearing is on.
func (c *Client) TransparentHearing() (bool, error) {
	f, err := c.requestTimeout(gaia.CmdGetTransparentHearing)
	if err != nil {
		return false, err
	}
	return payloadBool(f, gaia.CmdGetTransparentHearing)
}

// SetTransparentHearing turns Transparent Hearing on or off.
func (c *Client) SetTransparentHearing(on bool) error {
	_, err := c.requestTimeout(gaia.CmdSetTransparentHearing, boolByte(on))
	return err
}

// SetHearingPausesMusic chooses whether turning Transparent Hearing on pauses
// playback (true) or lets music keep playing underneath (false).
func (c *Client) SetHearingPausesMusic(pause bool) error {
	_, err := c.requestTimeout(gaia.CmdSetHearingBehaviour, boolByte(pause))
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
	if err := read(gaia.CmdGetTransparentHearing, func(b byte) { s.TransparentHearing = b != 0 }); err != nil {
		return s, err
	}
	if err := read(gaia.CmdGetHearingBehaviour, func(b byte) { s.HearingPausesMusic = b != 0 }); err != nil {
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
	if modes, err := c.ancModes(readTimeout); err == nil {
		s.AncModes = modes
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

// AncMode is one of the headphones' noise cancelling modes. They are not
// mutually exclusive: each is enabled or disabled in its own right, which is why
// the device reports a state per mode rather than a single selection.
type AncMode byte

const (
	AncWindNoise = AncMode(gaia.AncWindNoise)
	AncComfort   = AncMode(gaia.AncComfort)
	AncAdaptive  = AncMode(gaia.AncAdaptive)
)

func (m AncMode) String() string {
	switch m {
	case AncWindNoise:
		return "wind noise reduction"
	case AncComfort:
		return "comfort"
	case AncAdaptive:
		return "adaptive"
	default:
		return fmt.Sprintf("unknown(%d)", byte(m))
	}
}

// AncModes maps each setting to the value the device reports for it.
//
// Most are 0 or 1, but wind-noise reduction has three levels, which is why this
// keeps the raw value rather than a boolean.
type AncModes map[AncMode]byte

// WindNoise reports the wind-noise reduction level.
func (m AncModes) WindNoise() byte { return m[AncWindNoise] }

// WindNoiseName renders the wind-noise reduction level.
func WindNoiseName(level byte) string {
	switch level {
	case gaia.WindNoiseOff:
		return "off"
	case gaia.WindNoiseMaximum:
		return "maximum"
	case gaia.WindNoiseAutomatic:
		return "automatic"
	default:
		return fmt.Sprintf("unknown(%d)", level)
	}
}

// SetWindNoise sets the wind-noise reduction level.
func (c *Client) SetWindNoise(level byte) error {
	_, err := c.requestTimeout(gaia.CmdSetSoundMode, gaia.AncWindNoise, level)
	return err
}

// SetAdaptive turns adaptive noise cancelling on or off.
func (c *Client) SetAdaptive(on bool) error {
	return c.SetAncMode(AncAdaptive, on)
}

// Enabled reports whether a mode is on.
func (m AncModes) Enabled(mode AncMode) bool { return m[mode] != 0 }

// AncModes reads the state of every noise cancelling mode.
func (c *Client) AncModes() (AncModes, error) {
	return c.ancModes(DefaultTimeout)
}

func (c *Client) ancModes(timeout time.Duration) (AncModes, error) {
	f, err := c.request(timeout, gaia.CmdGetSoundMode)
	if err != nil {
		return nil, err
	}

	// The payload is a run of (mode, state) pairs.
	if len(f.Payload)%2 != 0 {
		return nil, fmt.Errorf("m4: sound mode reply has an odd length: % x", f.Payload)
	}

	modes := make(AncModes, len(f.Payload)/2)
	for i := 0; i+1 < len(f.Payload); i += 2 {
		modes[AncMode(f.Payload[i])] = f.Payload[i+1]
	}
	return modes, nil
}

// SetAncMode turns one noise cancelling mode on or off, leaving the others as
// they are.
func (c *Client) SetAncMode(mode AncMode, on bool) error {
	_, err := c.requestTimeout(gaia.CmdSetSoundMode, byte(mode), boolByte(on))
	return err
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

// EQ reads the equalizer configuration.
func (c *Client) EQ() (gaia.EQ, error) {
	return c.eq(DefaultTimeout)
}

func (c *Client) eq(timeout time.Duration) (gaia.EQ, error) {
	f, err := c.request(timeout, gaia.CmdGetEQ, gaia.EncodeEQQuery(gaia.EQGainQuery)...)
	if err != nil {
		return gaia.EQ{}, err
	}
	eq, err := gaia.DecodeEQ(f.Payload)
	if err != nil {
		return eq, err
	}
	// Only gains come back; the rest of each band is the fixed layout.
	return eq.WithDefaults(), nil
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

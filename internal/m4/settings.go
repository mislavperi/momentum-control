package m4

import (
	"errors"
	"fmt"

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
		f, err := c.requestTimeout(cmd)
		if err != nil {
			var devErr *DeviceError
			if errors.As(err, &devErr) {
				return nil // unsupported on this firmware
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
	return s, nil
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

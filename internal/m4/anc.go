package m4

import (
	"fmt"
	"time"

	"github.com/mislavperi/momentum4-control/internal/gaia"
)

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

package m4

import (
	"time"

	"github.com/mislavperi/momentum4-control/internal/gaia"
)

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

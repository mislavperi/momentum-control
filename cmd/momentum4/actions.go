package main

import (
	"fmt"

	"fyne.io/fyne/v2"

	"github.com/mislavperi/momentum4-control/internal/gaia"
	"github.com/mislavperi/momentum4-control/internal/m4"
)

// setMode applies one of the three sound modes.
func (u *ui) setMode(m mode) {
	if u.isApplying() {
		return
	}

	u.mu.Lock()
	hasHearing := u.state.Has(gaia.CmdGetTransparentHearing)
	u.mu.Unlock()

	u.showMode(m) // optimistic; the refresh after the write corrects it

	u.apply(func(c *m4.Client) error {
		switch m {
		case modeAmbient:
			if !hasHearing {
				// Older firmware without the switch: the closest equivalent.
				if err := c.SetANC(true); err != nil {
					return err
				}
				return c.SetTransparency(100)
			}
			return c.SetTransparentHearing(true)
		case modeOff:
			if hasHearing {
				if err := c.SetTransparentHearing(false); err != nil {
					return err
				}
			}
			return c.SetANC(false)
		default:
			if hasHearing {
				if err := c.SetTransparentHearing(false); err != nil {
					return err
				}
			}
			return c.SetANC(true)
		}
	})
}

// setBandGain writes one band, leaving the rest of the configuration as the
// device reported it.
func (u *ui) setBandGain(band int, gainDB float64) {
	u.mu.Lock()
	u.eq.Bands[band].GainCentiDB = int16(gainDB * 100)
	eq := u.eq
	u.mu.Unlock()

	u.apply(func(c *m4.Client) error { return c.SetEQ(eq) })
}

// flattenEQ sets every band to 0 dB.
func (u *ui) flattenEQ() {
	u.mu.Lock()
	for i := range u.eq.Bands {
		u.eq.Bands[i].GainCentiDB = 0
	}
	eq := u.eq
	u.mu.Unlock()

	u.apply(func(c *m4.Client) error { return c.SetEQ(eq) })
}

// apply runs an action against the connected device, reporting failures in the
// status line rather than throwing them away.
func (u *ui) apply(action func(*m4.Client) error) {
	u.mu.Lock()
	client := u.client
	u.mu.Unlock()

	if client == nil {
		u.setStatus("Not connected — the change was not sent.", colorDanger)
		return
	}

	go func() {
		if err := action(client); err != nil {
			u.setStatus(fmt.Sprintf("Failed: %v", err), colorDanger)
		}
		u.refresh()
	}()
}

// isApplying reports whether widgets are being set from device state, in which
// case their change handlers must not write the values back.
func (u *ui) isApplying() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.applying
}

// shutdown closes the connection and stops the retry loop.
func (u *ui) shutdown() {
	close(u.quit)
	u.disconnect()
}

// newUI creates the controller; build lays out its widgets.
func newUI(w fyne.Window) *ui {
	return &ui{win: w, quit: make(chan struct{}), page: pageOverview}
}

// deviceInfo is what is read once per connection rather than on every refresh.
type deviceInfo struct {
	Name, Firmware, Codec, Charge string
}

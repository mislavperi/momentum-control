package main

import (
	"fmt"

	"github.com/mislavperi/momentum-control/internal/gaia"
	"github.com/mislavperi/momentum-control/internal/m4"
)

// Sound modes as the user sees them. The headphones have no single mode
// setting; each mode is a combination of three switches:
//
//	Noise Cancelling  ANC on,  Transparent Hearing off
//	Ambient           Transparent Hearing on
//	Off               ANC off, Transparent Hearing off
//
// Transparent Hearing is the switch that audibly lets the outside world in.
// The transparency level only tunes how strongly ANC works, and on its own
// makes almost no audible difference, which is why an "Ambient" built from the
// level alone appeared not to work.
type mode int

const (
	modeUnknown mode = iota
	modeNoiseCancelling
	modeAmbient
	modeOff
)

func (m mode) String() string {
	switch m {
	case modeNoiseCancelling:
		return "Noise Cancelling"
	case modeAmbient:
		return "Ambient"
	case modeOff:
		return "Off"
	}
	return "Unknown"
}

// modeFor derives the displayed mode from the device's actual settings.
func modeFor(state m4.State) mode {
	if state.Has(gaia.CmdGetTransparentHearing) && state.TransparentHearing {
		return modeAmbient
	}
	if !state.Has(gaia.CmdGetANC) {
		return modeUnknown
	}
	if !state.ANC {
		return modeOff
	}
	if !state.Has(gaia.CmdGetTransparentHearing) && state.Transparency >= 50 {
		return modeAmbient
	}
	return modeNoiseCancelling
}

// modeDetail is the line under the mode name on the overview.
func modeDetail(m mode, s m4.State) string {
	switch m {
	case modeNoiseCancelling:
		detail := fmt.Sprintf("Strength %d%%", 100-s.Transparency)
		if s.Has(gaia.CmdGetSoundMode) {
			if s.AncModes.Enabled(m4.AncAdaptive) {
				detail = "Adaptive"
			}
			if w := s.AncModes.WindNoise(); w != gaia.WindNoiseOff {
				detail += " · wind reduction " + m4.WindNoiseName(w)
			}
		}
		return detail
	case modeAmbient:
		return "Transparent Hearing on"
	case modeOff:
		return "Noise cancelling and Transparent Hearing off"
	}
	return ""
}

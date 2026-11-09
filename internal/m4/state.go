package m4

import (
	"context"
	"errors"
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

package main

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"github.com/mislavperi/momentum4-control/internal/gaia"
	"github.com/mislavperi/momentum4-control/internal/m4"
)

func (u *ui) buildNoise() fyne.CanvasObject {
	u.noiseMode = u.newModePills()
	u.pauseMusic = newToggle(func(on bool) {
		if u.isApplying() {
			return
		}
		u.apply(func(c *m4.Client) error { return c.SetHearingPausesMusic(on) })
	})
	u.pauseRow = container.NewVBox(spacer(8), widget.NewSeparator(), spacer(8),
		row("ambient", "Pause music in Ambient", "Off keeps your music playing while you hear your surroundings", u.pauseMusic))
	modes := titledCard("Sound mode",
		"Ambient switches on Transparent Hearing, so you hear your surroundings through the microphones.",
		container.NewVBox(container.NewHBox(u.noiseMode[0], u.noiseMode[1], u.noiseMode[2]), u.pauseRow))

	u.levelValue = text("—", 13, colorAccentSoft, true)
	u.level = widget.NewSlider(0, 100)
	u.level.Step = 5
	u.level.OnChangeEnded = func(v float64) {
		if u.isApplying() {
			return
		}
		u.levelValue.Text = fmt.Sprintf("%d%%", int(v))
		u.levelValue.Refresh()
		u.apply(func(c *m4.Client) error { return c.SetTransparency(int(v)) })
	}
	ends := container.NewHBox(text("Maximum cancelling", 11, colorTextMuted, false), layout.NewSpacer(),
		text("Let more through", 11, colorTextMuted, false))
	u.levelBox = card(container.NewVBox(
		container.NewBorder(nil, nil,
			container.NewVBox(text("Noise cancelling strength", 15, colorText, true),
				text("Fine-tunes ANC while Noise Cancelling is on.", 12, colorTextMuted, false)),
			container.NewCenter(u.levelValue)),
		spacer(6), u.level, ends,
	))

	u.adaptive = newToggle(func(on bool) {
		if u.isApplying() {
			return
		}
		u.apply(func(c *m4.Client) error { return c.SetAdaptive(on) })
	})
	for i, w := range []struct {
		label string
		level byte
	}{
		{"Off", gaia.WindNoiseOff}, {"Maximum", gaia.WindNoiseMaximum}, {"Auto", gaia.WindNoiseAutomatic},
	} {
		level := w.level
		p := newPill("", w.label, func() {
			if u.isApplying() {
				return
			}
			u.showWind(level)
			u.apply(func(c *m4.Client) error { return c.SetWindNoise(level) })
		})
		p.compact = true
		u.wind[i] = p
	}
	u.ancBox = card(container.NewVBox(
		row("adaptive", "Adaptive ANC", "Adjusts cancelling to your surroundings", u.adaptive),
		spacer(8), widget.NewSeparator(), spacer(8),
		row("wind", "Wind noise reduction", "Cuts wind rumble outdoors", container.NewHBox(u.wind[0], u.wind[1], u.wind[2])),
	))

	return scrollPage(modes, spacer(14), u.levelBox, spacer(14), u.ancBox)
}

func (u *ui) showWind(level byte) {
	for i, l := range []byte{gaia.WindNoiseOff, gaia.WindNoiseMaximum, gaia.WindNoiseAutomatic} {
		u.wind[i].SetSelected(l == level)
	}
}

func (u *ui) showNoiseState(m mode, state m4.State) {
	// Hide what this firmware does not implement rather than showing a dead
	// control: the device answers unsupported commands with an error.
	setVisible(u.levelBox, state.Has(gaia.CmdGetTransparency))
	if state.Has(gaia.CmdGetTransparency) {
		u.level.SetValue(float64(state.Transparency))
		u.levelValue.Text = fmt.Sprintf("%d%%", state.Transparency)
		u.levelValue.Refresh()
	}

	setVisible(u.pauseRow, state.Has(gaia.CmdGetHearingBehaviour))
	u.pauseMusic.SetOn(state.HearingPausesMusic)

	setVisible(u.ancBox, state.Has(gaia.CmdGetSoundMode))
	if state.Has(gaia.CmdGetSoundMode) {
		u.adaptive.SetOn(state.AncModes.Enabled(m4.AncAdaptive))
		u.showWind(state.AncModes.WindNoise())
	}
}

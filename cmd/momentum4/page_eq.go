package main

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"github.com/mislavperi/momentum4-control/internal/gaia"
	"github.com/mislavperi/momentum4-control/internal/m4"
)

// The gain range the EQ sliders offer. The device accepts more, but the
// official app works within roughly this range.
const (
	eqMinDB = -6
	eqMaxDB = 6
)

// eqBand is the column of widgets for one equalizer band.
type eqBand struct {
	freq   *canvas.Text
	value  *canvas.Text
	slider *widget.Slider
}

func (u *ui) buildEQ() fyne.CanvasObject {
	columns := container.NewHBox(layout.NewSpacer())
	for i := range u.eqBands {
		band := i

		value := text("0.0 dB", 12, colorAccentSoft, true)
		value.Alignment = fyne.TextAlignCenter
		freq := text("—", 12, colorTextMuted, false)
		freq.Alignment = fyne.TextAlignCenter

		slider := widget.NewSlider(eqMinDB, eqMaxDB)
		slider.Orientation = widget.Vertical
		slider.Step = 0.5
		slider.OnChangeEnded = func(v float64) {
			if u.isApplying() {
				return
			}
			value.Text = fmt.Sprintf("%+.1f dB", v)
			value.Refresh()
			u.setBandGain(band, v)
		}

		u.eqBands[band] = eqBand{freq: freq, value: value, slider: slider}
		col := container.NewBorder(value, freq, nil, nil, container.NewCenter(fixed(slider, 40, 220)))
		columns.Add(fixed(col, 96, 290))
		columns.Add(layout.NewSpacer())
	}

	flat := newPill("refresh", "Reset to flat", u.flattenEQ)
	flat.compact = true

	u.eqBox = titledCard("Equalizer", fmt.Sprintf("Four bands, %d to %+d dB. Changes apply when you release a slider.", eqMinDB, eqMaxDB),
		container.NewVBox(spacer(8), columns, spacer(8), centered(flat)))
	u.eqAbsent = card(container.NewCenter(text("The equalizer is not available on this firmware.", 13, colorTextMuted, false)))
	u.eqAbsent.Hide()
	return scrollPage(u.eqBox, u.eqAbsent)
}

func (u *ui) showEQState(state m4.State) {
	hasEQ := state.Has(gaia.CmdGetEQ)
	setVisible(u.eqBox, hasEQ)
	setVisible(u.eqAbsent, !hasEQ)
	if hasEQ {
		flat := true
		for i, band := range state.EQ.Bands {
			if band.GainCentiDB != 0 {
				flat = false
			}
			u.eqBands[i].freq.Text = formatHz(band.Frequency) + " Hz"
			u.eqBands[i].freq.Refresh()
			u.eqBands[i].value.Text = fmt.Sprintf("%+.1f dB", band.Gain())
			u.eqBands[i].value.Refresh()
			u.eqBands[i].slider.SetValue(band.Gain())
		}
		if flat {
			u.eqPill.SetLabel("EQ · Flat")
		} else {
			u.eqPill.SetLabel("EQ · Custom")
		}
	} else {
		u.eqPill.SetLabel("EQ · Unavailable")
	}
	// The pill's width follows its label, and only a relayout of its row picks
	// that up.
	u.eqRow.Refresh()
}

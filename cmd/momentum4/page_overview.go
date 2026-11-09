package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"

	"github.com/mislavperi/momentum-control/internal/m4"
)

// modePills is one row of Noise Cancelling / Ambient / Off buttons. The same
// row appears on two pages.
type modePills [3]*pill

func (p modePills) show(m mode) {
	p[0].SetSelected(m == modeNoiseCancelling)
	p[1].SetSelected(m == modeAmbient)
	p[2].SetSelected(m == modeOff)
}

func (u *ui) newModePills() modePills {
	return modePills{
		newPill("shield", "Noise Cancelling", func() { u.setMode(modeNoiseCancelling) }),
		newPill("ambient", "Ambient", func() { u.setMode(modeAmbient) }),
		newPill("power", "Off", func() { u.setMode(modeOff) }),
	}
}

func (u *ui) buildOverview() fyne.CanvasObject {
	u.hero = newHeroArt()
	u.modeTitle = text("Waiting for headphones", 22, colorText, true)
	u.modeTitle.Alignment = fyne.TextAlignCenter
	u.modeSubtitle = text("", 12, colorTextMuted, false)
	u.modeSubtitle.Alignment = fyne.TextAlignCenter

	u.overviewMode = u.newModePills()
	u.eqPill = newPill("eq", "EQ · —", func() { u.goTo(pageEQ) })
	u.eqPill.compact = true
	u.eqRow = centered(u.eqPill).(*fyne.Container)

	bottom := container.NewVBox(
		u.modeTitle, u.modeSubtitle, spacer(18),
		centered(u.overviewMode[0], u.overviewMode[1], u.overviewMode[2]),
		spacer(4),
		u.eqRow,
	)
	return card(container.NewBorder(nil, bottom, nil, nil, u.hero))
}

// showMode highlights the active mode everywhere it is shown.
func (u *ui) showMode(m mode) {
	u.overviewMode.show(m)
	u.noiseMode.show(m)
	u.hero.SetActive(m == modeNoiseCancelling || m == modeAmbient)
	if m == modeUnknown {
		u.chipMode.Set("")
		return
	}
	u.chipMode.Set(m.String())
	u.modeTitle.Text = m.String()
	u.modeTitle.Refresh()
}

func (u *ui) showOverviewState(m mode, state m4.State) {
	u.showMode(m)
	u.modeSubtitle.Text = modeDetail(m, state)
	u.modeSubtitle.Refresh()
}

package main

import (
	"fmt"
	"image/color"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"github.com/mislavperi/momentum4-control/internal/gaia"
	"github.com/mislavperi/momentum4-control/internal/m4"
)

// Pages reachable from the sidebar.
type page int

const (
	pageOverview page = iota
	pageNoise
	pageEQ
	pageDevice
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

// modePills is one row of Noise Cancelling / Ambient / Off buttons. The same
// row appears on two pages.
type modePills [3]*pill

func (p modePills) show(m mode) {
	p[0].SetSelected(m == modeNoiseCancelling)
	p[1].SetSelected(m == modeAmbient)
	p[2].SetSelected(m == modeOff)
}

// ui holds the widgets and the single client they act on.
type ui struct {
	win fyne.Window

	// navigation
	page  page
	nav   map[page]*navItem
	pages map[page]fyne.CanvasObject

	// header and sidebar
	deviceName    *canvas.Text
	sideName      *canvas.Text
	sideStatus    *canvas.Text
	sideDot       *canvas.Circle
	transportText *canvas.Text
	transportDot  *canvas.Circle
	chipCodec     *statChip
	chipBattery   *statChip
	chipMode      *statChip
	status        *canvas.Text

	// overview
	hero         *heroArt
	modeTitle    *canvas.Text
	modeSubtitle *canvas.Text
	overviewMode modePills
	eqPill       *pill
	eqRow        *fyne.Container

	// noise control
	noiseMode  modePills
	level      *widget.Slider
	levelValue *canvas.Text
	levelBox   fyne.CanvasObject
	ancBox     fyne.CanvasObject
	adaptive   *toggle
	pauseMusic *toggle
	pauseRow   fyne.CanvasObject
	wind       [3]*pill

	// equalizer
	eqBands  [gaia.EQBands]eqBand
	eqBox    fyne.CanvasObject
	eqAbsent fyne.CanvasObject

	// device page
	infoName, infoFirmware, infoCodec, infoBattery, infoCharge, infoChannel *canvas.Text

	mu     sync.Mutex
	client *m4.Client
	state  m4.State

	// eq is the configuration last read from the device, so a single band can be
	// changed without inventing values for the others.
	eq gaia.EQ

	// applying suppresses the change handlers while widgets are being set from
	// device state, so refreshing the UI does not write the values back.
	applying bool

	// attempt identifies the current connect loop, so a newer one supersedes it.
	attempt int

	// address of the headphones, once found.
	address string

	// connecting guards against two connect loops running at once.
	connecting bool

	// quit stops the retry loop when the app is shutting down.
	quit chan struct{}
}

func (u *ui) build() fyne.CanvasObject {
	u.nav = map[page]*navItem{}
	u.pages = map[page]fyne.CanvasObject{
		pageOverview: u.buildOverview(),
		pageNoise:    u.buildNoise(),
		pageEQ:       u.buildEQ(),
		pageDevice:   u.buildDevice(),
	}

	pageStack := container.NewStack()
	for _, p := range []page{pageOverview, pageNoise, pageEQ, pageDevice} {
		pageStack.Add(u.pages[p])
	}

	main := container.NewBorder(u.buildHeader(), nil, nil, nil, pageStack)
	backdrop := canvas.NewVerticalGradient(colorBackdrop, colorBackground)
	content := container.NewStack(backdrop, padded(main, 30))

	u.status = text("Starting...", 12, colorTextMuted, false)
	statusBar := container.NewStack(canvas.NewRectangle(colorSidebar), padded(u.status, 8))

	sidebar := u.buildSidebar()
	u.goTo(pageOverview)
	u.showConnected(false, 0)

	return container.NewBorder(nil, statusBar, sidebar, nil, content)
}

func (u *ui) buildSidebar() fyne.CanvasObject {
	logoBg := canvas.NewRectangle(colorAccent)
	logoBg.CornerRadius = 12
	logo := fixed(container.NewStack(logoBg, container.NewCenter(iconImage(headphonesArt, 38))), 46, 46)
	brand := container.NewHBox(logo, spacerW(4), container.NewVBox(
		eyebrow("MOMENTUM"),
		text("Control", 18, colorAccentSoft, true),
	))

	ring := canvas.NewCircle(color.Transparent)
	ring.StrokeColor = colorBorder
	ring.StrokeWidth = 2
	ringIcon := fixed(container.NewStack(ring, container.NewCenter(iconImage(icon("headphones", colorTextMuted), 20))), 42, 42)
	u.sideName = text("MOMENTUM 4", 13, colorText, true)
	u.sideDot = canvas.NewCircle(colorTextFaint)
	u.sideStatus = text("Searching", 11, colorTextMuted, false)
	deviceCard := container.NewStack(
		panel(colorCard, 16),
		padded(container.NewHBox(ringIcon, spacerW(4), container.NewVBox(
			u.sideName,
			container.NewHBox(container.NewCenter(fixed(u.sideDot, 7, 7)), u.sideStatus),
		)), 14),
	)

	navs := container.NewVBox()
	for _, n := range []struct {
		p           page
		icon, label string
	}{
		{pageOverview, "overview", "Overview"},
		{pageNoise, "shield", "Noise Control"},
		{pageEQ, "eq", "Equalizer"},
		{pageDevice, "settings", "Device"},
	} {
		p := n.p
		item := newNavItem(n.icon, n.label, func() { u.goTo(p) })
		u.nav[p] = item
		navs.Add(item)
	}

	u.transportDot = canvas.NewCircle(colorTextFaint)
	u.transportText = text("Not connected", 11, colorTextMuted, false)
	transport := container.NewStack(
		panel(colorCard, 14),
		padded(container.NewBorder(nil, nil,
			container.NewCenter(iconImage(icon("bluetooth", colorAccentSoft), 16)),
			container.NewCenter(fixed(u.transportDot, 8, 8)),
			container.NewVBox(text("GAIA control", 12, colorText, false), u.transportText),
		), 12),
	)

	body := container.NewBorder(
		container.NewVBox(brand, spacer(22), deviceCard, spacer(22), navs),
		transport, nil, nil,
	)

	width := canvas.NewRectangle(colorSidebar)
	width.SetMinSize(fyne.NewSize(256, 0))
	edge := canvas.NewRectangle(colorBorder)
	edge.SetMinSize(fyne.NewSize(1, 0))
	return container.NewBorder(nil, nil, nil, edge, container.NewStack(width, padded(body, 20)))
}

func (u *ui) buildHeader() fyne.CanvasObject {
	u.deviceName = text("MOMENTUM 4", 30, colorText, true)
	u.chipCodec = newStatChip("CODEC")
	u.chipBattery = newStatChip("BATTERY")
	u.chipMode = newStatChip("MODE")

	title := container.NewVBox(eyebrow("CONNECTED DEVICE"), u.deviceName)
	chips := container.NewHBox(u.chipCodec.obj, u.chipBattery.obj, u.chipMode.obj)
	return container.NewVBox(
		container.NewBorder(nil, nil, title, container.NewCenter(chips)),
		spacer(18),
	)
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

func (u *ui) buildDevice() fyne.CanvasObject {
	value := func() *canvas.Text { return text("—", 13, colorText, true) }
	u.infoName, u.infoFirmware, u.infoCodec = value(), value(), value()
	u.infoBattery, u.infoCharge, u.infoChannel = value(), value(), value()

	rows := container.NewVBox()
	for i, r := range []struct {
		label string
		v     *canvas.Text
	}{
		{"Name", u.infoName}, {"Firmware", u.infoFirmware}, {"Audio codec", u.infoCodec},
		{"Battery", u.infoBattery}, {"Charging", u.infoCharge}, {"RFCOMM channel", u.infoChannel},
	} {
		if i > 0 {
			rows.Add(widget.NewSeparator())
		}
		rows.Add(padded(container.NewBorder(nil, nil, text(r.label, 13, colorTextMuted, false), r.v), 4))
	}

	reconnect := newPill("refresh", "Reconnect", func() { go u.connectLoop() })
	reconnect.compact = true
	connection := card(row("bluetooth", "Control connection",
		"GAIA over Bluetooth RFCOMM. The headphones allow one controller at a time.", reconnect))

	return scrollPage(titledCard("About these headphones", "", rows), spacer(14), connection)
}

// scrollPage stacks cards in a scroller, inset by a pixel so the scroller's
// clip does not shave off the cards' hairline borders.
func scrollPage(objs ...fyne.CanvasObject) fyne.CanvasObject {
	return container.NewVScroll(padded(container.NewVBox(objs...), 2))
}

// goTo switches the visible page.
func (u *ui) goTo(p page) {
	u.page = p
	for k, obj := range u.pages {
		setVisible(obj, k == p)
	}
	for k, n := range u.nav {
		n.SetSelected(k == p)
	}
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

func (u *ui) showWind(level byte) {
	for i, l := range []byte{gaia.WindNoiseOff, gaia.WindNoiseMaximum, gaia.WindNoiseAutomatic} {
		u.wind[i].SetSelected(l == level)
	}
}

// showState reflects a fresh read of the device in every widget.
func (u *ui) showState(state m4.State) {
	m := modeFor(state)
	u.showMode(m)
	u.modeSubtitle.Text = modeDetail(m, state)
	u.modeSubtitle.Refresh()

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

	if state.Has(gaia.CmdGetBattery) {
		u.chipBattery.Set(fmt.Sprintf("%d%%", state.Battery))
		u.infoBattery.Text = fmt.Sprintf("%d%%", state.Battery)
	} else {
		u.chipBattery.Set("")
		u.infoBattery.Text = "—"
	}
	u.infoBattery.Refresh()

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

// showConnected updates the connection indicators.
func (u *ui) showConnected(connected bool, channel int) {
	if u.sideDot == nil {
		return // not built yet
	}
	if connected {
		u.sideDot.FillColor = colorSuccess
		u.sideStatus.Text = "Connected"
		u.transportDot.FillColor = colorSuccess
		u.transportText.Text = fmt.Sprintf("RFCOMM · channel %d", channel)
		u.infoChannel.Text = fmt.Sprint(channel)
	} else {
		u.sideDot.FillColor = colorTextFaint
		u.sideStatus.Text = "Not connected"
		u.transportDot.FillColor = colorDanger
		u.transportText.Text = "Not connected"
		u.infoChannel.Text = "—"
		u.modeTitle.Text = "Waiting for headphones"
		u.modeTitle.Refresh()
		u.modeSubtitle.Text = "Settings appear once the control channel opens"
		u.modeSubtitle.Refresh()
		u.overviewMode.show(modeUnknown)
		u.noiseMode.show(modeUnknown)
		u.hero.SetActive(false)
		u.chipMode.Set("")
		u.chipBattery.Set("")
		u.chipCodec.Set("")
	}
	for _, t := range []*canvas.Text{u.sideStatus, u.transportText, u.infoChannel} {
		t.Refresh()
	}
	u.sideDot.Refresh()
	u.transportDot.Refresh()
}

// showInfo fills in the details read once per connection.
func (u *ui) showInfo(info deviceInfo) {
	set := func(t *canvas.Text, s string) {
		if s == "" {
			s = "—"
		}
		t.Text = s
		t.Refresh()
	}
	set(u.infoName, info.Name)
	set(u.infoFirmware, info.Firmware)
	set(u.infoCodec, info.Codec)
	set(u.infoCharge, info.Charge)
	u.chipCodec.Set(info.Codec)
}

func (u *ui) setDeviceName(name string) {
	u.deviceName.Text = name
	u.deviceName.Refresh()
	u.sideName.Text = name
	u.sideName.Refresh()
}

func (u *ui) setStatusNow(s string, col color.Color) {
	u.status.Text = s
	u.status.Color = col
	u.status.Refresh()
}

// formatHz renders a band's centre frequency the way an equalizer labels it.
func formatHz(hz uint16) string {
	if hz >= 1000 {
		return fmt.Sprintf("%.1fk", float64(hz)/1000)
	}
	return fmt.Sprintf("%d", hz)
}

// setVisible shows or hides a widget, since Fyne has no single call for it.
func setVisible(o fyne.CanvasObject, visible bool) {
	if visible {
		o.Show()
		return
	}
	o.Hide()
}

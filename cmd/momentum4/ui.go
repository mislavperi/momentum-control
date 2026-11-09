package main

import (
	"fmt"
	"image/color"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/mislavperi/momentum-control/internal/gaia"
	"github.com/mislavperi/momentum-control/internal/m4"
)

// Pages reachable from the sidebar.
type page int

const (
	pageOverview page = iota
	pageNoise
	pageEQ
	pageSettings
	pageConnections
	pageDevice
)

// pageOrder is the sidebar and stacking order.
var pageOrder = []page{pageOverview, pageNoise, pageEQ, pageSettings, pageConnections, pageDevice}

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

	// settings page
	settingRows   []*settingRow
	settingsEmpty fyne.CanvasObject

	// connections page
	hostList    *fyne.Container
	hostCaption *canvas.Text

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
		pageOverview:    u.buildOverview(),
		pageNoise:       u.buildNoise(),
		pageEQ:          u.buildEQ(),
		pageSettings:    u.buildSettings(),
		pageConnections: u.buildConnections(),
		pageDevice:      u.buildDevice(),
	}

	pageStack := container.NewStack()
	for _, p := range pageOrder {
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

// goTo switches the visible page.
func (u *ui) goTo(p page) {
	u.page = p
	for k, obj := range u.pages {
		setVisible(obj, k == p)
	}
	for k, n := range u.nav {
		n.SetSelected(k == p)
	}

	// These pages are read on demand rather than on every refresh: they are
	// several requests each, and change only when someone changes them.
	u.mu.Lock()
	client := u.client
	u.mu.Unlock()
	if client != nil {
		switch p {
		case pageSettings:
			go u.loadSettings(client)
		case pageConnections:
			go u.loadHosts(client)
		}
	}
}

// scrollPage stacks cards in a scroller, inset by a pixel so the scroller's
// clip does not shave off the cards' hairline borders.
func scrollPage(objs ...fyne.CanvasObject) fyne.CanvasObject {
	return container.NewVScroll(padded(container.NewVBox(objs...), 2))
}

// setVisible shows or hides a widget, since Fyne has no single call for it.
func setVisible(o fyne.CanvasObject, visible bool) {
	if visible {
		o.Show()
		return
	}
	o.Hide()
}

func (u *ui) setStatus(text string, col color.Color) {
	fyne.Do(func() { u.setStatusNow(text, col) })
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

// showState reflects a fresh read of the device in every widget.
func (u *ui) showState(state m4.State) {
	m := modeFor(state)
	u.showOverviewState(m, state)
	u.showNoiseState(m, state)
	u.showDeviceState(state)
	u.showEQState(state)
}

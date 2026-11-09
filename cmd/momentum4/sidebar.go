package main

import (
	"fmt"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
)

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

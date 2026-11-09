package main

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/mislavperi/momentum4-control/internal/gaia"
	"github.com/mislavperi/momentum4-control/internal/m4"
)

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

func (u *ui) showDeviceState(state m4.State) {
	if state.Has(gaia.CmdGetBattery) {
		u.chipBattery.Set(fmt.Sprintf("%d%%", state.Battery))
		u.infoBattery.Text = fmt.Sprintf("%d%%", state.Battery)
	} else {
		u.chipBattery.Set("")
		u.infoBattery.Text = "—"
	}
	u.infoBattery.Refresh()
}

package main

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"github.com/mislavperi/momentum-control/internal/m4"
)

func (u *ui) buildConnections() fyne.CanvasObject {
	u.hostList = container.NewVBox()
	u.hostCaption = text("", 12, colorTextMuted, false)
	reload := newPill("refresh", "Refresh", func() { u.reloadHosts() })
	reload.compact = true

	head := container.NewBorder(nil, nil,
		container.NewVBox(text("Paired devices", 15, colorText, true), u.hostCaption),
		container.NewCenter(reload))
	u.showHosts(nil, 0)
	return scrollPage(card(container.NewVBox(head, spacer(10), u.hostList)))
}

// reloadHosts reads the paired list in the background.
func (u *ui) reloadHosts() {
	u.mu.Lock()
	client := u.client
	u.mu.Unlock()
	if client != nil {
		go u.loadHosts(client)
	}
}

func (u *ui) loadHosts(client *m4.Client) {
	hosts, err := client.PairedHosts()
	max, _ := client.MaxConnections()

	u.mu.Lock()
	current := u.client == client
	u.mu.Unlock()
	if !current {
		return
	}
	fyne.Do(func() {
		if err != nil {
			u.setStatusNow(fmt.Sprintf("Failed to read paired devices: %v", err), colorDanger)
			return
		}
		u.showHosts(hosts, max)
	})
}

// showHosts rebuilds the list. It is small, so rebuilding beats diffing.
func (u *ui) showHosts(hosts []m4.PairedHost, max int) {
	u.hostList.RemoveAll()
	switch {
	case hosts == nil:
		u.hostCaption.Text = "Appears once the headphones are connected."
	case max > 0:
		u.hostCaption.Text = fmt.Sprintf("The headphones remember these devices and stay connected to up to %d at once.", max)
	default:
		u.hostCaption.Text = "The headphones remember these devices."
	}
	u.hostCaption.Refresh()

	for i, h := range hosts {
		if i > 0 {
			u.hostList.Add(widget.NewSeparator())
		}
		u.hostList.Add(padded(u.hostRow(h), 4))
	}
	u.hostList.Refresh()
}

func (u *ui) hostRow(h m4.PairedHost) fyne.CanvasObject {
	status := text("Paired", 11, colorTextMuted, false)
	dotColor := colorTextFaint
	if h.Connected {
		status.Text = "Connected"
		dotColor = colorSuccess
	}
	d := canvas.NewCircle(dotColor)

	title := container.NewHBox(text(h.Name, 13, colorText, true))
	if h.This {
		title.Add(badge("This computer"))
	}
	words := container.NewVBox(title, container.NewHBox(container.NewCenter(fixed(d, 7, 7)), status))
	left := container.NewHBox(container.NewCenter(iconImage(icon("devices", colorAccentSoft), 20)), spacerW(6), words)

	actions := container.NewHBox()
	if !h.Connected {
		actions.Add(u.hostButton("Connect", func(c *m4.Client) error { return c.ConnectHost(h.Index) }))
	}
	// Dropping or forgetting this computer would cut the app's own control
	// connection, with no way back from inside the app.
	if !h.This {
		if h.Connected {
			actions.Add(u.hostButton("Disconnect", func(c *m4.Client) error { return c.DisconnectHost(h.Index) }))
		}
		forget := newPill("", "Forget", func() {
			dialog.ShowConfirm("Forget "+h.Name+"?",
				"The headphones will no longer connect to it automatically. It has to be paired again to use them.",
				func(ok bool) {
					if ok {
						u.hostAction(func(c *m4.Client) error { return c.ForgetHost(h.Index) })
					}
				}, u.win)
		})
		forget.compact = true
		actions.Add(forget)
	}
	return container.NewBorder(nil, nil, left, container.NewCenter(actions), layout.NewSpacer())
}

func (u *ui) hostButton(label string, action func(*m4.Client) error) fyne.CanvasObject {
	p := newPill("", label, func() { u.hostAction(action) })
	p.compact = true
	return p
}

// hostAction runs a multipoint command, then rereads the list: the device
// takes a moment to connect or drop a host, so it is read again after a pause.
func (u *ui) hostAction(action func(*m4.Client) error) {
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
		u.loadHosts(client)
		u.reloadHostsAfter(client)
	}()
}

// badge is a small rounded label.
func badge(s string) fyne.CanvasObject {
	bg := panel(colorSelected, 8)
	bg.StrokeColor = colorAccent
	return container.NewStack(bg, padded(text(s, 10, colorAccentSoft, true), 4))
}

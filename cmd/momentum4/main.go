// Command momentum4 is a desktop controller for Sennheiser MOMENTUM 4
// headphones.
package main

import (
	"fmt"
	"image/color"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/mislavperi/momentum4-control/internal/gaia"
	"github.com/mislavperi/momentum4-control/internal/m4"
	"github.com/mislavperi/momentum4-control/internal/rfcomm"
)

// Sound modes. The MOMENTUM 4 has no single mode setting: it has ANC on or off
// and a separate transparency level, so each mode is a combination of the two.
type mode int

const (
	modeUnknown mode = iota
	modeNoiseCancelling
	modeAmbient
	modeOff
)

func main() {
	// Fyne owns the main thread and pumps the main queue there, which is what
	// IOBluetooth needs to deliver its callbacks. No rfcomm.Main here.
	a := app.NewWithID("com.momentum4.control")
	a.Settings().SetTheme(newPanelTheme())

	w := a.NewWindow("MOMENTUM 4")
	w.Resize(fyne.NewSize(420, 520))

	ui := newUI(w)
	w.SetContent(ui.build())

	// Dial only once Fyne is pumping the main thread: nothing drains the main
	// queue before the event loop starts. The headphones grant one control
	// session at a time, so the connection is then held for the app's lifetime.
	a.Lifecycle().SetOnStarted(func() {
		// Dial the moment the headphones appear: the control service seems to
		// accept a connection only shortly after the link is established, so
		// reacting to the event beats polling for it.
		rfcomm.OnDeviceConnected(func(address string) { go ui.onDeviceConnected(address) })
		go ui.connectLoop()
	})

	w.SetCloseIntercept(func() {
		ui.shutdown()
		w.Close()
	})

	// Closing the channel on the way out matters more than usual here: the
	// headphones grant one control session at a time, and an orphaned session
	// locks out the next connection until the link is reset. A window close is
	// covered above; this covers being terminated.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stop
		ui.shutdown()
		a.Quit()
	}()

	w.ShowAndRun()
}

// ui holds the widgets and the single client they act on.
type ui struct {
	win fyne.Window

	status     *canvas.Text
	deviceName *canvas.Text

	noiseCancelling *modeButton
	ambient         *modeButton
	off             *modeButton

	transparency    *widget.Slider
	transLabel      *canvas.Text
	transparencyBox fyne.CanvasObject
	soundCard       fyne.CanvasObject

	reconnect *widget.Button

	mu     sync.Mutex
	client *m4.Client

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

func newUI(w fyne.Window) *ui {
	return &ui{win: w, quit: make(chan struct{})}
}

// shutdown closes the connection and stops the retry loop.
func (u *ui) shutdown() {
	close(u.quit)
	u.disconnect()
}

func (u *ui) build() fyne.CanvasObject {
	u.deviceName = heading("MOMENTUM 4", 26, colorText, true)
	u.status = heading("Connecting...", 12, colorTextMuted, false)

	u.noiseCancelling = newModeButton("●", "Noise Cancelling", func() {
		u.setMode(modeNoiseCancelling)
	})
	u.ambient = newModeButton("≈", "Ambient Sound", func() {
		u.setMode(modeAmbient)
	})
	u.off = newModeButton("○", "Off", func() {
		u.setMode(modeOff)
	})

	modes := container.NewGridWithColumns(3, u.noiseCancelling, u.ambient, u.off)

	u.transLabel = canvas.NewText("Transparency: -", colorTextMuted)
	u.transLabel.TextSize = 12

	u.transparency = widget.NewSlider(0, 100)
	u.transparency.Step = 5
	u.transparency.OnChangeEnded = func(v float64) {
		u.mu.Lock()
		applying := u.applying
		u.mu.Unlock()
		if applying {
			return
		}
		u.transLabel.Text = fmt.Sprintf("Transparency: %d%%", int(v))
		u.transLabel.Refresh()
		u.apply(func(c *m4.Client) error { return c.SetTransparency(int(v)) })
	}

	u.transparencyBox = container.NewVBox(u.transLabel, u.transparency)

	u.soundCard = card("Ambient Sound Control",
		container.NewVBox(modes, u.transparencyBox))
	u.soundCard.Hide()

	u.reconnect = widget.NewButton("Reconnect", func() { go u.connectLoop() })

	header := container.NewVBox(u.deviceName, u.status)

	return container.NewBorder(
		container.NewPadded(header),
		container.NewPadded(u.reconnect),
		nil, nil,
		container.NewVBox(u.soundCard),
	)
}

// setMode applies one of the three sound modes.
//
// Ambient and Noise Cancelling both leave ANC enabled; they differ in how much
// of the outside world is let through, which is what the transparency level
// controls.
func (u *ui) setMode(m mode) {
	u.mu.Lock()
	applying := u.applying
	u.mu.Unlock()
	if applying {
		return
	}

	u.showMode(m)

	u.apply(func(c *m4.Client) error {
		switch m {
		case modeOff:
			return c.SetANC(false)
		case modeAmbient:
			if err := c.SetANC(true); err != nil {
				return err
			}
			return c.SetTransparency(100)
		default:
			if err := c.SetANC(true); err != nil {
				return err
			}
			return c.SetTransparency(0)
		}
	})
}

// showMode highlights the active mode button.
func (u *ui) showMode(m mode) {
	u.noiseCancelling.SetSelected(m == modeNoiseCancelling)
	u.ambient.SetSelected(m == modeAmbient)
	u.off.SetSelected(m == modeOff)
}

// modeFor derives the displayed mode from the device's actual settings.
func modeFor(state m4.State) mode {
	if !state.Has(gaia.CmdGetANC) {
		return modeUnknown
	}
	if !state.ANC {
		return modeOff
	}
	// The device reports a continuous level, so treat the upper half as ambient.
	if state.Transparency >= 50 {
		return modeAmbient
	}
	return modeNoiseCancelling
}

// apply runs an action against the connected device, reporting failures in the
// status line rather than throwing them away.
func (u *ui) apply(action func(*m4.Client) error) {
	u.mu.Lock()
	client := u.client
	u.mu.Unlock()

	if client == nil {
		return
	}

	go func() {
		if err := action(client); err != nil {
			u.setStatus(fmt.Sprintf("Failed: %v", err), colorDanger)
			return
		}
		u.refresh()
	}()
}

// retryInterval is how long to wait before trying the control channel again.
//
// The headphones do not always accept a control connection: the channel is
// sometimes refused for long stretches, whatever the host does. Rather than
// making that the user's problem, the app keeps trying quietly and connects as
// soon as the device lets it.
const retryInterval = 30 * time.Second

// connectLoop keeps attempting to connect until it succeeds, the window closes,
// or another attempt supersedes this one.
func (u *ui) connectLoop() {
	u.mu.Lock()
	if u.connecting {
		// Another loop is already working through the channels. Dialling in
		// parallel only doubles the failed opens, which this device reacts badly
		// to.
		u.mu.Unlock()
		return
	}
	u.connecting = true
	u.attempt++
	attempt := u.attempt
	u.mu.Unlock()

	defer func() {
		u.mu.Lock()
		u.connecting = false
		u.mu.Unlock()
	}()

	for {
		if u.connect() {
			return
		}

		select {
		case <-time.After(retryInterval):
		case <-u.quit:
			return
		}

		// A newer attempt (the Reconnect button) takes over from this one.
		u.mu.Lock()
		superseded := u.attempt != attempt
		u.mu.Unlock()
		if superseded {
			return
		}
	}
}

// onDeviceConnected dials immediately when our headphones come back.
func (u *ui) onDeviceConnected(address string) {
	u.mu.Lock()
	connected := u.client != nil
	want := u.address
	u.mu.Unlock()

	if connected || !sameAddress(address, want) {
		return
	}
	log.Printf("device %s connected, dialling now", address)
	u.connectLoop()
}

// sameAddress compares Bluetooth addresses, which appear with either separator.
func sameAddress(a, b string) bool {
	norm := func(s string) string {
		return strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(s, ":", ""), "-", ""))
	}
	return b != "" && norm(a) == norm(b)
}

// connect makes one attempt, reporting whether it succeeded.
func (u *ui) connect() bool {
	u.disconnect()
	u.setStatus("Looking for a paired MOMENTUM 4...", colorTextMuted)

	device, err := m4.FindHeadphones()
	if err != nil {
		u.setStatus(err.Error(), colorDanger)
		return false
	}

	u.mu.Lock()
	u.address = device.Address
	u.mu.Unlock()

	fyne.Do(func() {
		u.deviceName.Text = device.Name
		u.deviceName.Refresh()
	})

	client, err := m4.ConnectProgress(device.Address, rfcomm.Options{}, func(s string) {
		log.Print(s)
		u.setStatus(s, colorTextMuted)
	})
	if err != nil {
		log.Printf("connect failed: %v", err)
		u.setStatus(fmt.Sprintf("Headphones are not accepting a control connection. Retrying every %s.",
			retryInterval), colorDanger)
		return false
	}
	log.Printf("connected on RFCOMM channel %d", client.ChannelID())

	u.mu.Lock()
	u.client = client
	u.mu.Unlock()

	// Ask the device to push changes made on the headphones themselves.
	client.RegisterNotifications()
	go u.watch(client)

	fyne.Do(u.soundCard.Show)
	u.refresh()
	return true
}

func (u *ui) disconnect() {
	u.mu.Lock()
	client := u.client
	u.client = nil
	u.mu.Unlock()

	if client != nil {
		client.Close()
	}
	fyne.Do(func() {
		if u.soundCard != nil {
			u.soundCard.Hide()
		}
	})
}

// watch keeps the UI in step with changes made on the headphones, and notices
// when the connection drops.
func (u *ui) watch(client *m4.Client) {
	for range client.Notifications() {
		// The payload layout varies per feature, so re-read rather than guess.
		u.refresh()
	}

	// The stream closes when the channel does.
	u.mu.Lock()
	current := u.client
	u.mu.Unlock()
	if current == client {
		u.disconnect()
		go u.connectLoop()
	}
}

func (u *ui) refresh() {
	u.mu.Lock()
	client := u.client
	u.mu.Unlock()
	if client == nil {
		return
	}

	state, err := client.State()
	if err != nil {
		u.setStatus(fmt.Sprintf("Failed to read settings: %v", err), colorDanger)
		return
	}

	fyne.Do(func() {
		u.mu.Lock()
		u.applying = true
		u.mu.Unlock()
		defer func() {
			u.mu.Lock()
			u.applying = false
			u.mu.Unlock()
		}()

		// Hide what this firmware does not implement rather than showing a dead
		// control: the device answers unsupported commands with an error.
		hasTransparency := state.Has(gaia.CmdGetTransparency)
		setVisible(u.transparencyBox, hasTransparency)

		if hasTransparency {
			u.transparency.SetValue(float64(state.Transparency))
			u.transLabel.Text = fmt.Sprintf("Transparency: %d%%", state.Transparency)
			u.transLabel.Refresh()
		}

		u.showMode(modeFor(state))
		u.setStatusNow(fmt.Sprintf("Connected  ·  RFCOMM channel %d", state.ChannelID), colorTextMuted)
	})
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

func (u *ui) setStatusNow(text string, col color.Color) {
	u.status.Text = text
	u.status.Color = col
	u.status.Refresh()
}

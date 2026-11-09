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
	"syscall"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"

	"github.com/mislavperi/momentum4-control/internal/gaia"
	"github.com/mislavperi/momentum4-control/internal/m4"
	"github.com/mislavperi/momentum4-control/internal/rfcomm"
)

// Sound modes as the user sees them. The headphones have no single mode
// setting; each mode is a combination of three switches:
//
//	Noise Cancelling  ANC on,  Transparent Hearing off
//	Ambient           Transparent Hearing on
//	Off               ANC off, Transparent Hearing off
//
// Transparent Hearing is the switch that audibly lets the outside world in.
// The transparency level only tunes how strongly ANC works, and on its own
// makes almost no audible difference, which is why an "Ambient" built from the
// level alone appeared not to work.
type mode int

const (
	modeUnknown mode = iota
	modeNoiseCancelling
	modeAmbient
	modeOff
)

func (m mode) String() string {
	switch m {
	case modeNoiseCancelling:
		return "Noise Cancelling"
	case modeAmbient:
		return "Ambient"
	case modeOff:
		return "Off"
	}
	return "Unknown"
}

func main() {
	// Fyne owns the main thread and pumps the main queue there, which is what
	// IOBluetooth needs to deliver its callbacks. No rfcomm.Main here.
	a := app.NewWithID("com.momentum4.control")
	a.Settings().SetTheme(newPanelTheme())

	w := a.NewWindow("MOMENTUM 4")
	w.Resize(fyne.NewSize(1080, 720))

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

// deviceInfo is what is read once per connection rather than on every refresh.
type deviceInfo struct {
	Name, Firmware, Codec, Charge string
}

// newUI creates the controller; build lays out its widgets.
func newUI(w fyne.Window) *ui {
	return &ui{win: w, quit: make(chan struct{}), page: pageOverview}
}

// shutdown closes the connection and stops the retry loop.
func (u *ui) shutdown() {
	close(u.quit)
	u.disconnect()
}

// isApplying reports whether widgets are being set from device state, in which
// case their change handlers must not write the values back.
func (u *ui) isApplying() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.applying
}

// setMode applies one of the three sound modes.
func (u *ui) setMode(m mode) {
	if u.isApplying() {
		return
	}

	u.mu.Lock()
	hasHearing := u.state.Has(gaia.CmdGetTransparentHearing)
	u.mu.Unlock()

	u.showMode(m) // optimistic; the refresh after the write corrects it

	u.apply(func(c *m4.Client) error {
		switch m {
		case modeAmbient:
			if !hasHearing {
				// Older firmware without the switch: the closest equivalent.
				if err := c.SetANC(true); err != nil {
					return err
				}
				return c.SetTransparency(100)
			}
			return c.SetTransparentHearing(true)
		case modeOff:
			if hasHearing {
				if err := c.SetTransparentHearing(false); err != nil {
					return err
				}
			}
			return c.SetANC(false)
		default:
			if hasHearing {
				if err := c.SetTransparentHearing(false); err != nil {
					return err
				}
			}
			return c.SetANC(true)
		}
	})
}

// modeFor derives the displayed mode from the device's actual settings.
func modeFor(state m4.State) mode {
	if state.Has(gaia.CmdGetTransparentHearing) && state.TransparentHearing {
		return modeAmbient
	}
	if !state.Has(gaia.CmdGetANC) {
		return modeUnknown
	}
	if !state.ANC {
		return modeOff
	}
	if !state.Has(gaia.CmdGetTransparentHearing) && state.Transparency >= 50 {
		return modeAmbient
	}
	return modeNoiseCancelling
}

// setBandGain writes one band, leaving the rest of the configuration as the
// device reported it.
func (u *ui) setBandGain(band int, gainDB float64) {
	u.mu.Lock()
	u.eq.Bands[band].GainCentiDB = int16(gainDB * 100)
	eq := u.eq
	u.mu.Unlock()

	u.apply(func(c *m4.Client) error { return c.SetEQ(eq) })
}

// flattenEQ sets every band to 0 dB.
func (u *ui) flattenEQ() {
	u.mu.Lock()
	for i := range u.eq.Bands {
		u.eq.Bands[i].GainCentiDB = 0
	}
	eq := u.eq
	u.mu.Unlock()

	u.apply(func(c *m4.Client) error { return c.SetEQ(eq) })
}

// apply runs an action against the connected device, reporting failures in the
// status line rather than throwing them away.
func (u *ui) apply(action func(*m4.Client) error) {
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

	fyne.Do(func() { u.setDeviceName(device.Name) })

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

	fyne.Do(func() { u.showConnected(true, client.ChannelID()) })
	u.refresh()
	go u.loadInfo(client)
	return true
}

// loadInfo reads the details that do not change while connected.
func (u *ui) loadInfo(client *m4.Client) {
	var info deviceInfo
	info.Name, _ = client.DeviceName()
	info.Firmware, _ = client.Firmware()
	info.Codec, _ = client.Codec()
	info.Charge, _ = client.ChargeState()

	u.mu.Lock()
	current := u.client == client
	u.mu.Unlock()
	if current {
		fyne.Do(func() { u.showInfo(info) })
	}
}

func (u *ui) disconnect() {
	u.mu.Lock()
	client := u.client
	u.client = nil
	u.mu.Unlock()

	if client != nil {
		client.Close()
	}
	fyne.Do(func() { u.showConnected(false, 0) })
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

	u.mu.Lock()
	u.state = state
	if state.Has(gaia.CmdGetEQ) {
		u.eq = state.EQ
	}
	u.mu.Unlock()

	fyne.Do(func() {
		u.mu.Lock()
		u.applying = true
		u.mu.Unlock()
		defer func() {
			u.mu.Lock()
			u.applying = false
			u.mu.Unlock()
		}()

		u.showState(state)
		u.setStatusNow("Connected", colorTextMuted)
	})
}

func (u *ui) setStatus(text string, col color.Color) {
	fyne.Do(func() { u.setStatusNow(text, col) })
}

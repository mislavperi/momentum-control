package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	"fyne.io/fyne/v2"

	"github.com/mislavperi/momentum4-control/internal/gaia"
	"github.com/mislavperi/momentum4-control/internal/m4"
	"github.com/mislavperi/momentum4-control/internal/rfcomm"
)

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

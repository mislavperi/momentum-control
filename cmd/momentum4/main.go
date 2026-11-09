// Command momentum4 is a desktop controller for Sennheiser MOMENTUM 4
// headphones.
package main

import (
	"os"
	"os/signal"
	"syscall"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"

	"github.com/mislavperi/momentum4-control/internal/rfcomm"
)

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

//go:build darwin

// On macOS this wraps Apple's IOBluetooth framework, since Darwin has no socket
// family for Classic Bluetooth. A Conn behaves like a stream: bytes arrive from
// IOBluetooth on the main thread and are buffered here for Read.
//
// IOBluetooth requires two things that a plain Go program does not provide, and
// both fail quietly rather than loudly. The process needs a bundle identity and
// an Info.plist declaring NSBluetoothAlwaysUsageDescription, or TCC kills it on
// the first call; and its main thread must run a Cocoa run loop, or callbacks are
// never delivered. See Main and the Makefile.
package rfcomm

/*
#cgo CFLAGS: -x objective-c -fobjc-arc -Wall
#cgo LDFLAGS: -framework Foundation -framework IOBluetooth

#include <stdlib.h>
#include "rfcomm_darwin.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

// maxBuffered caps the inbound buffer. The device sends small status frames, so
// exceeding this means nothing is reading; dropping beats growing without bound.
const maxBuffered = 1 << 20

// writeTimeout bounds an asynchronous IOBluetooth write.
const writeTimeout = 5 * time.Second

// mainQueueProbe bounds the check for a live main queue.
const mainQueueProbe = 2 * time.Second

// Main hands the main thread to a Cocoa run loop and runs fn on a goroutine,
// returning once fn does.
//
// IOBluetooth is backed by CoreBluetooth, which delivers every callback on the
// main queue. Nothing services that queue in a plain Go program, so without this
// a channel opens and writes succeed while no byte is ever delivered back. Call
// it from main, and do the real work inside fn.
//
// A GUI toolkit that already owns the main thread provides the same run loop, so
// programs built on one do not need Main.
func Main(fn func()) {
	runtime.LockOSThread()

	go func() {
		defer C.m4_stop_main_loop()

		// Dial fails fast unless the loop is up, so wait for it to start.
		for C.m4_main_loop_running() == 0 {
			time.Sleep(time.Millisecond)
		}
		fn()
	}()

	C.m4_run_main_loop()
}

// Conn is an open RFCOMM channel.
type Conn struct {
	handle  uint64
	session *C.M4Session

	// IOBluetooth reports open and write completions through delegate callbacks
	// rather than return values, so both are awaited on these channels.
	openCh  chan error
	writeCh chan error

	// writeMu keeps one write in flight at a time, so each completion belongs to
	// the write that is waiting for it.
	writeMu sync.Mutex

	mu       sync.Mutex
	cond     *sync.Cond
	buf      []byte
	closed   bool
	remote   bool // closed by the device rather than by us
	deadline time.Time
}

var (
	registryMu sync.RWMutex
	registry   = map[uint64]*Conn{}
	nextHandle atomic.Uint64
)

// Dial opens an RFCOMM channel to the Bluetooth device at addr, which may be
// written with colons or dashes ("80:C3:BA:81:F0:E3").
func Dial(addr string, opts Options) (*Conn, error) {
	uuid := opts.uuid()

	if C.m4_main_queue_alive(C.int(mainQueueProbe.Milliseconds())) == 0 {
		return nil, ErrNoMainLoop
	}

	c := &Conn{
		handle:  nextHandle.Add(1),
		openCh:  make(chan error, 1),
		writeCh: make(chan error, 1),
	}
	c.cond = sync.NewCond(&c.mu)

	c.session = C.m4_session_new(C.uint64_t(c.handle))
	if c.session == nil {
		return nil, errors.New("rfcomm: out of memory")
	}

	registryMu.Lock()
	registry[c.handle] = c
	registryMu.Unlock()

	cAddr := C.CString(normalizeAddr(addr))
	defer C.free(unsafe.Pointer(cAddr))

	C.m4_session_open(
		c.session,
		cAddr,
		(*C.uint8_t)(unsafe.Pointer(&uuid[0])),
		C.int(opts.Channel),
	)

	budget := opts.timeout()

	select {
	case err := <-c.openCh:
		if err != nil {
			c.Close()
			return nil, fmt.Errorf("rfcomm: dial %s: %w", addr, err)
		}
		return c, nil

	case <-time.After(budget):
		c.Close()
		// An open to a device with no link spends its whole budget paging it, so
		// say so rather than leave a bare timeout.
		if C.m4_device_connected(cAddr) == 0 {
			return nil, fmt.Errorf("rfcomm: dial %s: timed out after %s; the device is not connected to this Mac, so check it is on and connected in Bluetooth settings", addr, budget)
		}
		return nil, fmt.Errorf("rfcomm: dial %s: timed out after %s", addr, budget)
	}
}

// ChannelID reports the RFCOMM channel number in use.
func (c *Conn) ChannelID() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.session == nil {
		return 0
	}
	return int(C.m4_channel_id(c.session))
}

// SetReadDeadline sets an absolute time after which a blocked Read fails with
// os.ErrDeadlineExceeded. A zero time clears the deadline.
func (c *Conn) SetReadDeadline(t time.Time) {
	c.mu.Lock()
	c.deadline = t
	c.mu.Unlock()
	c.cond.Broadcast()

	if !t.IsZero() {
		// Wake any blocked reader when the deadline elapses.
		time.AfterFunc(time.Until(t), c.cond.Broadcast)
	}
}

// Read returns buffered bytes, blocking until at least one is available. It
// returns io.EOF once the device closes the channel and the buffer is drained.
func (c *Conn) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for len(c.buf) == 0 {
		switch {
		case c.remote:
			return 0, io.EOF
		case c.closed:
			return 0, ErrClosed
		case !c.deadline.IsZero() && !time.Now().Before(c.deadline):
			return 0, os.ErrDeadlineExceeded
		}
		c.cond.Wait()
	}

	n := copy(p, c.buf)
	c.buf = c.buf[n:]
	if len(c.buf) == 0 {
		c.buf = nil
	}
	return n, nil
}

// Write sends b to the device, splitting at the channel MTU as needed. It blocks
// until IOBluetooth reports the write complete.
func (c *Conn) Write(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	// Discard any completion left behind by an abandoned write.
	select {
	case <-c.writeCh:
	default:
	}

	// c.mu is held across the call so a concurrent Close cannot free the session
	// under it. m4_session_write only queues the write, so this does not block.
	c.mu.Lock()
	if c.closed || c.session == nil {
		c.mu.Unlock()
		return 0, ErrClosed
	}
	C.m4_session_write(c.session, unsafe.Pointer(&b[0]), C.int(len(b)))
	c.mu.Unlock()

	select {
	case err := <-c.writeCh:
		if err != nil {
			return 0, err
		}
		return len(b), nil

	case <-time.After(writeTimeout):
		return 0, fmt.Errorf("rfcomm: write timed out after %s", writeTimeout)
	}
}

// Close shuts the channel down. It is safe to call more than once.
func (c *Conn) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	session := c.session
	c.session = nil
	c.mu.Unlock()

	c.cond.Broadcast()
	select {
	case c.writeCh <- ErrClosed:
	default:
	}

	registryMu.Lock()
	delete(registry, c.handle)
	registryMu.Unlock()

	if session != nil {
		C.m4_close(session)
	}
	return nil
}

func lookup(handle uint64) *Conn {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return registry[handle]
}

//export m4GoOnOpen
func m4GoOnOpen(handle C.uint64_t, status C.int, errmsg *C.char) {
	c := lookup(uint64(handle))
	if c == nil {
		if errmsg != nil {
			C.free(unsafe.Pointer(errmsg))
		}
		return
	}

	var err error
	if status != 0 {
		err = consumeError(errmsg, "")
	}

	select {
	case c.openCh <- err:
	default: // already reported
	}
}

//export m4GoOnWrite
func m4GoOnWrite(handle C.uint64_t, status C.int) {
	c := lookup(uint64(handle))
	if c == nil {
		return
	}

	var err error
	if status != 0 {
		err = fmt.Errorf("rfcomm: write failed (%#08x)", uint32(status))
	}

	select {
	case c.writeCh <- err:
	default: // nothing waiting
	}
}

//export m4GoOnData
func m4GoOnData(handle C.uint64_t, buf unsafe.Pointer, length C.int) {
	c := lookup(uint64(handle))
	if c == nil || length <= 0 {
		return
	}

	// The IOBluetooth buffer is only valid for the duration of this call.
	chunk := C.GoBytes(buf, length)

	c.mu.Lock()
	if !c.closed && len(c.buf)+len(chunk) <= maxBuffered {
		c.buf = append(c.buf, chunk...)
	}
	c.mu.Unlock()
	c.cond.Broadcast()
}

//export m4GoOnClose
func m4GoOnClose(handle C.uint64_t) {
	c := lookup(uint64(handle))
	if c == nil {
		return
	}

	c.mu.Lock()
	c.remote = true
	c.mu.Unlock()
	c.cond.Broadcast()
}

var (
	connectHookMu sync.Mutex
	connectHook   func(address string)
)

// OnDeviceConnected registers fn to be called whenever a Bluetooth device
// connects, with its address.
//
// The control service appears to accept a connection only shortly after the link
// comes up, so dialling on this event succeeds where polling does not. Passing
// nil stops the callbacks.
func OnDeviceConnected(fn func(address string)) {
	connectHookMu.Lock()
	connectHook = fn
	connectHookMu.Unlock()

	C.m4_watch_connections()
}

//export m4GoOnDeviceConnected
func m4GoOnDeviceConnected(address *C.char) {
	addr := C.GoString(address)
	C.free(unsafe.Pointer(address))

	connectHookMu.Lock()
	fn := connectHook
	connectHookMu.Unlock()

	if fn != nil {
		go fn(addr)
	}
}

// PairedDevices lists the devices macOS has paired, which is where the
// headphone's address comes from without asking the user to type it.
func PairedDevices() []Device {
	raw := C.m4_paired_devices()
	if raw == nil {
		return nil
	}
	text := C.GoString(raw)
	C.free(unsafe.Pointer(raw))

	var devices []Device
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		addr, name, ok := strings.Cut(line, "\t")
		if !ok || addr == "" {
			continue
		}
		devices = append(devices, Device{Address: addr, Name: name})
	}
	return devices
}

// consumeError turns a malloc'd C error string into a Go error and frees it.
func consumeError(msg *C.char, addr string) error {
	text := "unknown IOBluetooth error"
	if msg != nil {
		text = C.GoString(msg)
		C.free(unsafe.Pointer(msg))
	}
	if addr != "" {
		return fmt.Errorf("rfcomm: dial %s: %s", addr, text)
	}
	return fmt.Errorf("rfcomm: %s", text)
}

// normalizeAddr converts a Bluetooth address to the dash-separated form
// IOBluetooth expects.
func normalizeAddr(addr string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(addr), ":", "-"))
}

//go:build linux

package rfcomm

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// On Linux the kernel provides RFCOMM as an ordinary socket family, so a Conn is
// a non-blocking socket wrapped in an *os.File, which gives Read, Write and
// deadlines through the Go runtime's poller with no cgo.
//
// Service lookup (SDP) is not implemented: BlueZ offers no D-Bus call that
// resolves a remote service to a channel, and speaking SDP over L2CAP is a
// protocol of its own. Dial therefore needs a channel, and m4.Connect supplies
// one by scanning, which it does on every platform because the device's SDP
// record goes stale.

// Conn is an open RFCOMM channel.
type Conn struct {
	f       *os.File
	channel int
}

// Dial opens an RFCOMM channel to the Bluetooth device at addr, which may be
// written with colons or dashes ("80:C3:BA:81:F0:E3").
func Dial(addr string, opts Options) (*Conn, error) {
	if opts.Channel <= 0 {
		return nil, ErrNoSDP
	}
	if opts.Channel > 30 {
		return nil, fmt.Errorf("rfcomm: channel %d is outside 1-30", opts.Channel)
	}

	bd, err := parseAddr(addr)
	if err != nil {
		return nil, err
	}

	fd, err := unix.Socket(unix.AF_BLUETOOTH, unix.SOCK_STREAM|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, unix.BTPROTO_RFCOMM)
	if err != nil {
		return nil, fmt.Errorf("rfcomm: socket: %w", err)
	}

	// The kernel stores addresses least significant byte first.
	sa := &unix.SockaddrRFCOMM{Channel: uint8(opts.Channel)}
	for i := range bd {
		sa.Addr[i] = bd[5-i]
	}

	if err := connectTimeout(fd, sa, opts.timeout()); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("rfcomm: dial %s channel %d: %w", addr, opts.Channel, err)
	}

	return &Conn{f: os.NewFile(uintptr(fd), "rfcomm"), channel: opts.Channel}, nil
}

// connectTimeout starts a non-blocking connect and waits for it to finish.
func connectTimeout(fd int, sa unix.Sockaddr, timeout time.Duration) error {
	err := unix.Connect(fd, sa)
	if err == nil {
		return nil
	}
	if !errors.Is(err, unix.EINPROGRESS) && !errors.Is(err, unix.EAGAIN) {
		return err
	}

	deadline := time.Now().Add(timeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return fmt.Errorf("timed out after %s", timeout)
		}
		fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLOUT}}
		n, err := unix.Poll(fds, int(remaining.Milliseconds())+1)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return err
		}
		if n == 0 {
			continue
		}
		soErr, err := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_ERROR)
		if err != nil {
			return err
		}
		if soErr != 0 {
			return unix.Errno(soErr)
		}
		return nil
	}
}

// ChannelID reports the RFCOMM channel number in use.
func (c *Conn) ChannelID() int { return c.channel }

// SetReadDeadline sets an absolute time after which a blocked Read fails with
// os.ErrDeadlineExceeded. A zero time clears the deadline.
func (c *Conn) SetReadDeadline(t time.Time) { _ = c.f.SetReadDeadline(t) }

// Read reads from the channel. It returns io.EOF once the device closes it.
func (c *Conn) Read(p []byte) (int, error) {
	n, err := c.f.Read(p)
	if errors.Is(err, os.ErrClosed) {
		return n, ErrClosed
	}
	return n, err
}

// Write sends b to the device.
func (c *Conn) Write(b []byte) (int, error) {
	n, err := c.f.Write(b)
	if errors.Is(err, os.ErrClosed) {
		return n, ErrClosed
	}
	return n, err
}

// Close shuts the channel down. It is safe to call more than once.
func (c *Conn) Close() error {
	if err := c.f.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		return err
	}
	return nil
}

// PairedDevices lists the devices BlueZ has paired, read from bluetoothctl.
// It returns nil when bluetoothctl is not installed.
func PairedDevices() []Device {
	// BlueZ 5.65 renamed "paired-devices" to "devices Paired"; try both.
	for _, args := range [][]string{{"devices", "Paired"}, {"paired-devices"}} {
		out, err := exec.Command("bluetoothctl", args...).Output()
		if err != nil {
			continue
		}
		if devices := parseBluetoothctl(out); len(devices) > 0 {
			return devices
		}
	}
	return nil
}

// parseBluetoothctl reads "Device AA:BB:CC:DD:EE:FF Name" lines.
func parseBluetoothctl(out []byte) []Device {
	var devices []Device
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		fields := strings.SplitN(strings.TrimSpace(sc.Text()), " ", 3)
		if len(fields) < 2 || fields[0] != "Device" {
			continue
		}
		if _, err := parseAddr(fields[1]); err != nil {
			continue
		}
		d := Device{Address: fields[1]}
		if len(fields) == 3 {
			d.Name = fields[2]
		}
		devices = append(devices, d)
	}
	return devices
}

// OnDeviceConnected is not implemented on Linux; the app's periodic retry
// covers reconnection instead.
func OnDeviceConnected(func(address string)) {}

// Main runs fn. Linux needs no main-thread run loop.
func Main(fn func()) { fn() }

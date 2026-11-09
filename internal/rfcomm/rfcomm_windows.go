//go:build windows

package rfcomm

import (
	"encoding/binary"
	"fmt"
	"io"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// On Windows, RFCOMM is a Winsock socket family (AF_BTH). Unlike Linux, the
// stack resolves a service UUID to its channel itself: connecting with the
// UUID and port 0 performs the SDP lookup. x/sys/windows has no Sockaddr type
// for AF_BTH, so the Winsock calls are made directly.

const (
	afBTH          = 32 // AF_BTH
	bthprotoRFCOMM = 3  // BTHPROTO_RFCOMM
	sockaddrBthLen = 30 // sizeof(SOCKADDR_BTH), declared packed in ws2bth.h
)

var (
	ws2         = windows.NewLazySystemDLL("ws2_32.dll")
	procConnect = ws2.NewProc("connect")
	procRecv    = ws2.NewProc("recv")
	procSend    = ws2.NewProc("send")
	procPeer    = ws2.NewProc("getpeername")

	bthprops      = windows.NewLazySystemDLL("bthprops.cpl")
	procFindFirst = bthprops.NewProc("BluetoothFindFirstDevice")
	procFindNext  = bthprops.NewProc("BluetoothFindNextDevice")
	procFindClose = bthprops.NewProc("BluetoothFindDeviceClose")

	wsaOnce sync.Once
	wsaErr  error
)

// failed reports whether a Winsock call returned SOCKET_ERROR. The functions
// return a 32-bit int, so -1 arrives zero-extended as 0xFFFFFFFF rather than
// as ^uintptr(0); comparing the low 32 bits is what x/sys does too.
func failed(r uintptr) bool { return int32(r) == -1 }

func startup() error {
	wsaOnce.Do(func() {
		var data windows.WSAData
		wsaErr = windows.WSAStartup(0x0202, &data)
	})
	return wsaErr
}

// Conn is an open RFCOMM channel.
type Conn struct {
	s       windows.Handle
	channel int

	mu     sync.Mutex
	closed bool
}

// Dial opens an RFCOMM channel to the Bluetooth device at addr, which may be
// written with colons or dashes ("80:C3:BA:81:F0:E3").
func Dial(addr string, opts Options) (*Conn, error) {
	if err := startup(); err != nil {
		return nil, fmt.Errorf("rfcomm: WSAStartup: %w", err)
	}
	bd, err := parseAddr(addr)
	if err != nil {
		return nil, err
	}

	s, err := windows.Socket(afBTH, windows.SOCK_STREAM, bthprotoRFCOMM)
	if err != nil {
		return nil, fmt.Errorf("rfcomm: socket: %w", err)
	}

	sa := sockaddrBth(bd, opts.uuid(), opts.Channel)

	// connect blocks, so bound it by closing the socket from outside.
	done := make(chan error, 1)
	go func() {
		r, _, e := procConnect.Call(uintptr(s), uintptr(unsafe.Pointer(&sa[0])), sockaddrBthLen)
		if failed(r) {
			done <- e
			return
		}
		done <- nil
	}()

	budget := opts.timeout()
	select {
	case err := <-done:
		if err != nil {
			windows.Closesocket(s)
			return nil, fmt.Errorf("rfcomm: dial %s: %w", addr, err)
		}
	case <-time.After(budget):
		windows.Closesocket(s)
		return nil, fmt.Errorf("rfcomm: dial %s: timed out after %s", addr, budget)
	}

	c := &Conn{s: s, channel: opts.Channel}
	if peer, ok := peerChannel(s); ok {
		c.channel = peer
	}
	return c, nil
}

// sockaddrBth encodes a packed SOCKADDR_BTH: family, 64-bit address, service
// class GUID, port. A zero port asks Windows to resolve the GUID over SDP.
func sockaddrBth(bd [6]byte, uuid [16]byte, channel int) [sockaddrBthLen]byte {
	var b [sockaddrBthLen]byte
	binary.LittleEndian.PutUint16(b[0:], afBTH)

	var btAddr uint64
	for _, x := range bd {
		btAddr = btAddr<<8 | uint64(x)
	}
	binary.LittleEndian.PutUint64(b[2:], btAddr)

	// A GUID stores its first three fields little endian; the UUID is big endian.
	binary.LittleEndian.PutUint32(b[10:], binary.BigEndian.Uint32(uuid[0:4]))
	binary.LittleEndian.PutUint16(b[14:], binary.BigEndian.Uint16(uuid[4:6]))
	binary.LittleEndian.PutUint16(b[16:], binary.BigEndian.Uint16(uuid[6:8]))
	copy(b[18:26], uuid[8:16])

	binary.LittleEndian.PutUint32(b[26:], uint32(channel))
	return b
}

// peerChannel reads the channel Windows connected to.
func peerChannel(s windows.Handle) (int, bool) {
	var sa [sockaddrBthLen]byte
	n := int32(sockaddrBthLen)
	r, _, _ := procPeer.Call(uintptr(s), uintptr(unsafe.Pointer(&sa[0])), uintptr(unsafe.Pointer(&n)))
	if failed(r) {
		return 0, false
	}
	return int(binary.LittleEndian.Uint32(sa[26:])), true
}

// ChannelID reports the RFCOMM channel number in use.
func (c *Conn) ChannelID() int { return c.channel }

func (c *Conn) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// Read reads from the channel. It returns io.EOF once the device closes it.
func (c *Conn) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	// A closed handle value can be reused by the next socket, so never pass it
	// to Winsock again.
	if c.isClosed() {
		return 0, ErrClosed
	}
	r, _, e := procRecv.Call(uintptr(c.s), uintptr(unsafe.Pointer(&p[0])), uintptr(len(p)), 0)
	switch {
	case failed(r):
		if c.isClosed() {
			return 0, ErrClosed
		}
		return 0, fmt.Errorf("rfcomm: recv: %w", e)
	case r == 0:
		return 0, io.EOF
	}
	return int(r), nil
}

// Write sends b to the device.
func (c *Conn) Write(b []byte) (int, error) {
	written := 0
	for written < len(b) {
		if c.isClosed() {
			return written, ErrClosed
		}
		r, _, e := procSend.Call(uintptr(c.s), uintptr(unsafe.Pointer(&b[written])), uintptr(len(b)-written), 0)
		if failed(r) {
			if c.isClosed() {
				return written, ErrClosed
			}
			return written, fmt.Errorf("rfcomm: send: %w", e)
		}
		written += int(r)
	}
	return written, nil
}

// Close shuts the channel down. It is safe to call more than once.
func (c *Conn) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()
	return windows.Closesocket(c.s)
}

// searchParams is BLUETOOTH_DEVICE_SEARCH_PARAMS.
type searchParams struct {
	size                uint32
	returnAuthenticated int32
	returnRemembered    int32
	returnUnknown       int32
	returnConnected     int32
	issueInquiry        int32
	timeoutMultiplier   uint8
	radio               windows.Handle
}

// deviceInfo is BLUETOOTH_DEVICE_INFO.
type deviceInfo struct {
	size          uint32
	address       uint64
	classOfDevice uint32
	connected     int32
	remembered    int32
	authenticated int32
	lastSeen      windows.Systemtime
	lastUsed      windows.Systemtime
	name          [248]uint16
}

// PairedDevices lists the devices Windows remembers, without a radio inquiry.
func PairedDevices() []Device {
	if procFindFirst.Find() != nil {
		return nil
	}
	params := searchParams{returnAuthenticated: 1, returnRemembered: 1}
	params.size = uint32(unsafe.Sizeof(params))
	info := deviceInfo{}
	info.size = uint32(unsafe.Sizeof(info))

	h, _, _ := procFindFirst.Call(uintptr(unsafe.Pointer(&params)), uintptr(unsafe.Pointer(&info)))
	if h == 0 {
		return nil
	}
	defer procFindClose.Call(h)

	var devices []Device
	for {
		devices = append(devices, Device{
			Address: formatAddr(info.address),
			Name:    windows.UTF16ToString(info.name[:]),
		})
		info = deviceInfo{}
		info.size = uint32(unsafe.Sizeof(info))
		if ok, _, _ := procFindNext.Call(h, uintptr(unsafe.Pointer(&info))); ok == 0 {
			return devices
		}
	}
}

func formatAddr(a uint64) string {
	return fmt.Sprintf("%02X:%02X:%02X:%02X:%02X:%02X",
		byte(a>>40), byte(a>>32), byte(a>>24), byte(a>>16), byte(a>>8), byte(a))
}

// OnDeviceConnected is not implemented on Windows; the app's periodic retry
// covers reconnection instead.
func OnDeviceConnected(func(address string)) {}

// Main runs fn. Windows needs no main-thread run loop.
func Main(fn func()) { fn() }

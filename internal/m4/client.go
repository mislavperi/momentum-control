// Package m4 drives a Sennheiser MOMENTUM 4 over its RFCOMM control channel.
package m4

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/mislavperi/momentum-control/internal/gaia"
)

// DefaultTimeout bounds how long a request waits for its reply.
const DefaultTimeout = 3 * time.Second

// ErrNotConnected is returned once the control channel has gone away.
var ErrNotConnected = errors.New("m4: not connected")

// DeviceError is a command rejected by the headphones.
type DeviceError struct {
	Command uint16
	Status  byte
}

func (e *DeviceError) Error() string {
	return fmt.Sprintf("m4: %s rejected by device (status %#02x)", gaia.CommandName(e.Command), e.Status)
}

// Notification is an unsolicited frame pushed by the headphones, for example
// when a setting is changed using the headphone's own controls.
type Notification struct {
	Command uint16
	Payload []byte
}

// transport is the byte stream a Client speaks over. rfcomm.Conn is the real
// implementation; the indirection lets the protocol logic be tested against a
// fake device, without headphones.
type transport interface {
	io.ReadWriteCloser
	ChannelID() int
}

// Client is a connection to the headphones. It is safe for concurrent use;
// requests are serialised because the device answers one command at a time.
type Client struct {
	conn transport

	// reqMu serialises whole request/response exchanges.
	reqMu sync.Mutex

	mu      sync.Mutex
	pending map[uint16]chan gaia.Frame
	err     error

	notifications chan Notification
	done          chan struct{}
	closeOnce     sync.Once
}

// newClient wraps an open channel and starts its reader. Connect is the entry
// point callers want; this exists so connection probing can build a client per
// candidate channel.
func newClient(conn transport) *Client {
	c := &Client{
		conn:          conn,
		pending:       make(map[uint16]chan gaia.Frame),
		notifications: make(chan Notification, 32),
		done:          make(chan struct{}),
	}
	go c.readLoop()
	return c
}

// ChannelID reports the RFCOMM channel the device answered on.
func (c *Client) ChannelID() int { return c.conn.ChannelID() }

// Notifications returns the stream of unsolicited device frames. The channel is
// closed when the client shuts down. Slow consumers miss notifications rather
// than stalling the reader.
func (c *Client) Notifications() <-chan Notification { return c.notifications }

// Close shuts down the client.
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		close(c.done)
		c.conn.Close()
	})
	return nil
}

func (c *Client) readLoop() {
	defer close(c.notifications)

	var buf []byte
	chunk := make([]byte, 4096)

	for {
		n, err := c.conn.Read(chunk)
		if n > 0 {
			buf = append(buf, chunk[:n]...)
			var frames []gaia.Frame
			frames, buf = gaia.Scan(buf)
			for _, f := range frames {
				c.dispatch(f)
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				err = fmt.Errorf("%w: device closed the channel", ErrNotConnected)
			}
			c.fail(err)
			return
		}

		select {
		case <-c.done:
			c.fail(ErrNotConnected)
			return
		default:
		}
	}
}

// dispatch routes a frame to a waiting request, or to the notification stream.
//
// A frame only answers a request when it is actually a reply, from the vendor
// that was asked. Matching on the command ID alone would let a device-initiated
// frame, or a frame from another vendor that happens to share an ID, be taken
// for the answer to a pending request.
func (c *Client) dispatch(f gaia.Frame) {
	if !f.IsReply() || f.Vendor != gaia.VendorSennheiser {
		c.notify(f)
		return
	}

	base := f.Base()

	c.mu.Lock()
	waiter, ok := c.pending[base]
	if ok {
		delete(c.pending, base)
	}
	c.mu.Unlock()

	if ok {
		waiter <- f
		return
	}
	c.notify(f)
}

// notify publishes an unsolicited frame, dropping it rather than stalling the
// reader when nothing is consuming the stream.
func (c *Client) notify(f gaia.Frame) {
	select {
	case c.notifications <- Notification{Command: f.Command, Payload: f.Payload}:
	default:
	}
}

// fail records a terminal error and wakes every waiting request.
func (c *Client) fail(err error) {
	c.mu.Lock()
	if c.err == nil {
		c.err = err
	}
	waiters := c.pending
	c.pending = map[uint16]chan gaia.Frame{}
	c.mu.Unlock()

	for _, ch := range waiters {
		close(ch)
	}
}

// Request sends cmd and waits for the matching reply. An error reply from the
// device is returned as *DeviceError.
func (c *Client) Request(ctx context.Context, cmd uint16, payload ...byte) (gaia.Frame, error) {
	c.reqMu.Lock()
	defer c.reqMu.Unlock()

	frame, err := gaia.Command(cmd, payload...).Encode()
	if err != nil {
		return gaia.Frame{}, err
	}

	// The reply carries cmd|MaskACK or cmd|MaskError; both reduce to cmd.
	reply := make(chan gaia.Frame, 1)

	c.mu.Lock()
	if c.err != nil {
		err := c.err
		c.mu.Unlock()
		return gaia.Frame{}, err
	}
	c.pending[cmd] = reply
	c.mu.Unlock()

	cleanup := func() {
		c.mu.Lock()
		delete(c.pending, cmd)
		c.mu.Unlock()
	}

	if _, err := c.conn.Write(frame); err != nil {
		cleanup()
		return gaia.Frame{}, err
	}

	select {
	case f, ok := <-reply:
		if !ok {
			c.mu.Lock()
			err := c.err
			c.mu.Unlock()
			if err == nil {
				err = ErrNotConnected
			}
			return gaia.Frame{}, err
		}
		if f.IsError(cmd) {
			return f, &DeviceError{Command: cmd, Status: f.Status()}
		}
		return f, nil

	case <-ctx.Done():
		cleanup()
		return gaia.Frame{}, fmt.Errorf("m4: %s: %w", gaia.CommandName(cmd), ctx.Err())

	case <-c.done:
		cleanup()
		return gaia.Frame{}, ErrNotConnected
	}
}

// requestTimeout is Request with DefaultTimeout applied.
func (c *Client) requestTimeout(cmd uint16, payload ...byte) (gaia.Frame, error) {
	return c.request(DefaultTimeout, cmd, payload...)
}

// request is Request with an explicit timeout.
func (c *Client) request(timeout time.Duration, cmd uint16, payload ...byte) (gaia.Frame, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return c.Request(ctx, cmd, payload...)
}

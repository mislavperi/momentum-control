package m4

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/mislavperi/momentum4-control/internal/gaia"
)

// TestMain shortens the per-read timeout: several tests exercise a device that
// stays silent, and the real budget would make the suite crawl.
func TestMain(m *testing.M) {
	readTimeout = 20 * time.Millisecond
	os.Exit(m.Run())
}

// fakeDevice is an in-memory stand-in for the headphones. It decodes the frames
// written to it and replies according to handler, which lets the protocol logic
// be tested without hardware.
type fakeDevice struct {
	handler func(gaia.Frame) []gaia.Frame

	mu      sync.Mutex
	cond    *sync.Cond
	inbound []byte
	sent    []uint16 // commands the client sent, in order
	closed  bool
}

func newFakeDevice(handler func(gaia.Frame) []gaia.Frame) *fakeDevice {
	d := &fakeDevice{handler: handler}
	d.cond = sync.NewCond(&d.mu)
	return d
}

func (d *fakeDevice) Write(b []byte) (int, error) {
	frames, _ := gaia.Scan(b)

	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return 0, errors.New("closed")
	}
	for _, f := range frames {
		d.sent = append(d.sent, f.Command)
		for _, reply := range d.handler(f) {
			encoded, err := reply.Encode()
			if err != nil {
				d.mu.Unlock()
				return 0, err
			}
			d.inbound = append(d.inbound, encoded...)
		}
	}
	d.mu.Unlock()
	d.cond.Broadcast()
	return len(b), nil
}

func (d *fakeDevice) Read(p []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	for len(d.inbound) == 0 {
		if d.closed {
			return 0, io.EOF
		}
		d.cond.Wait()
	}

	n := copy(p, d.inbound)
	d.inbound = d.inbound[n:]
	return n, nil
}

func (d *fakeDevice) Close() error {
	d.mu.Lock()
	d.closed = true
	d.mu.Unlock()
	d.cond.Broadcast()
	return nil
}

func (d *fakeDevice) ChannelID() int { return 1 }

// push delivers an unsolicited frame, as the headphones do when a setting is
// changed on the device itself.
func (d *fakeDevice) push(f gaia.Frame) {
	encoded, _ := f.Encode()
	d.mu.Lock()
	d.inbound = append(d.inbound, encoded...)
	d.mu.Unlock()
	d.cond.Broadcast()
}

func (d *fakeDevice) commandsSent() []uint16 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]uint16(nil), d.sent...)
}

// ack builds the device's success reply to req.
func ack(req uint16, payload ...byte) gaia.Frame {
	return gaia.Frame{
		Version: gaia.Version3,
		Vendor:  gaia.VendorSennheiser,
		Command: req | gaia.MaskACK,
		Payload: payload,
	}
}

// deviceState is a fake that answers the settings commands from stored values.
func deviceState(anc byte, transparency byte, bass byte) func(gaia.Frame) []gaia.Frame {
	return func(f gaia.Frame) []gaia.Frame {
		switch f.Command {
		case gaia.CmdGetANC:
			return []gaia.Frame{ack(f.Command, anc)}
		case gaia.CmdGetTransparency:
			return []gaia.Frame{ack(f.Command, transparency)}
		case gaia.CmdGetBassBoost:
			return []gaia.Frame{ack(f.Command, bass)}
		case gaia.CmdSetANC:
			anc = f.Payload[0]
			return []gaia.Frame{ack(f.Command)}
		case gaia.CmdSetTransparency:
			transparency = f.Payload[0]
			return []gaia.Frame{ack(f.Command)}
		case gaia.CmdSetBassBoost:
			bass = f.Payload[0]
			return []gaia.Frame{ack(f.Command)}
		default:
			// This firmware answers commands it does not implement with an
			// error, as the real device does for bass boost.
			return []gaia.Frame{{
				Version: gaia.Version3,
				Vendor:  gaia.VendorSennheiser,
				Command: f.Command | gaia.MaskError,
				Payload: []byte{0x01},
			}}
		}
	}
}

func TestStateReadsEverySetting(t *testing.T) {
	client := newClient(newFakeDevice(deviceState(1, 40, 0)))
	defer client.Close()

	state, err := client.State()
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if !state.ANC || state.Transparency != 40 || state.BassBoost {
		t.Errorf("State() = %+v, want ANC on, transparency 40, bass off", state)
	}
}

func TestSetThenReadRoundTrip(t *testing.T) {
	client := newClient(newFakeDevice(deviceState(1, 40, 0)))
	defer client.Close()

	if err := client.SetTransparency(75); err != nil {
		t.Fatalf("SetTransparency: %v", err)
	}
	if err := client.SetANC(false); err != nil {
		t.Fatalf("SetANC: %v", err)
	}

	state, err := client.State()
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if state.ANC || state.Transparency != 75 {
		t.Errorf("State() = %+v, want ANC off, transparency 75", state)
	}
}

func TestSetTransparencyClampsOutOfRange(t *testing.T) {
	device := newFakeDevice(deviceState(1, 0, 0))
	client := newClient(device)
	defer client.Close()

	if err := client.SetTransparency(500); err != nil {
		t.Fatalf("SetTransparency: %v", err)
	}

	// 500 truncated to a byte would be 244; it must arrive as 100.
	state, err := client.State()
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if state.Transparency != 100 {
		t.Errorf("transparency = %d, want it clamped to 100", state.Transparency)
	}
}

func TestDeviceErrorIsReported(t *testing.T) {
	device := newFakeDevice(func(f gaia.Frame) []gaia.Frame {
		return []gaia.Frame{{
			Version: gaia.Version3,
			Vendor:  gaia.VendorSennheiser,
			Command: f.Command | gaia.MaskError,
			Payload: []byte{0x07},
		}}
	})
	client := newClient(device)
	defer client.Close()

	_, err := client.ANC()

	var devErr *DeviceError
	if !errors.As(err, &devErr) {
		t.Fatalf("ANC() error = %v, want a *DeviceError", err)
	}
	if devErr.Status != 0x07 || devErr.Command != gaia.CmdGetANC {
		t.Errorf("DeviceError = %+v, want command %#04x status 0x07", devErr, gaia.CmdGetANC)
	}
}

func TestStateSurvivesSilence(t *testing.T) {
	// A device that simply ignores what it does not implement must still yield
	// the settings it does answer, rather than failing the whole read.
	device := newFakeDevice(func(f gaia.Frame) []gaia.Frame {
		if f.Command == gaia.CmdGetANC {
			return []gaia.Frame{ack(f.Command, 1)}
		}
		return nil // silence
	})
	client := newClient(device)
	defer client.Close()

	state, err := client.State()
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if !state.ANC || !state.Has(gaia.CmdGetANC) {
		t.Errorf("State() = %+v, want ANC reported as supported and on", state)
	}
	if state.Has(gaia.CmdGetBattery) {
		t.Error("State() reported battery as supported despite silence")
	}
}

func TestStateFailsWhenNothingAnswers(t *testing.T) {
	// Total silence means the connection is gone, not that the device
	// implements nothing.
	client := newClient(newFakeDevice(func(gaia.Frame) []gaia.Frame { return nil }))
	defer client.Close()

	if _, err := client.State(); err == nil {
		t.Fatal("State() succeeded against a device that answered nothing")
	}
}

func TestAncModesReadsEveryModeState(t *testing.T) {
	device := newFakeDevice(func(f gaia.Frame) []gaia.Frame {
		if f.Command == gaia.CmdGetSoundMode {
			// (mode, state) pairs, as captured from the real device.
			return []gaia.Frame{ack(f.Command, 0x01, 0x02, 0x02, 0x00, 0x03, 0x01)}
		}
		return nil
	})
	client := newClient(device)
	defer client.Close()

	modes, err := client.AncModes()
	if err != nil {
		t.Fatalf("AncModes: %v", err)
	}
	if !modes.Enabled(AncAdaptive) {
		t.Error("adaptive reported off, want on")
	}
	if modes.Enabled(AncComfort) {
		t.Error("comfort reported on, want off")
	}
	if modes.WindNoise() != gaia.WindNoiseMaximum {
		t.Errorf("wind noise = %s, want maximum", WindNoiseName(modes.WindNoise()))
	}
}

func TestSetAncModeSendsModeAndState(t *testing.T) {
	var got []byte
	device := newFakeDevice(func(f gaia.Frame) []gaia.Frame {
		if f.Command == gaia.CmdSetSoundMode {
			got = append([]byte(nil), f.Payload...)
		}
		return []gaia.Frame{ack(f.Command)}
	})
	client := newClient(device)
	defer client.Close()

	if err := client.SetAncMode(AncAdaptive, true); err != nil {
		t.Fatalf("SetAncMode: %v", err)
	}
	if len(got) != 2 || got[0] != byte(AncAdaptive) || got[1] != 1 {
		t.Errorf("SetAncMode sent % x, want 03 01", got)
	}
}

func TestSetEQSendsWritePreampAndCommit(t *testing.T) {
	device := newFakeDevice(func(f gaia.Frame) []gaia.Frame {
		return []gaia.Frame{ack(f.Command)}
	})
	client := newClient(device)
	defer client.Close()

	var eq gaia.EQ
	eq.Bands[0].GainCentiDB = 300

	if err := client.SetEQ(eq); err != nil {
		t.Fatalf("SetEQ: %v", err)
	}

	// The commit is what makes the change audible; missing it is a silent bug.
	want := []uint16{gaia.CmdSetEQ, gaia.CmdSetPreamp, gaia.CmdCommitEQ}
	got := device.commandsSent()
	if len(got) != len(want) {
		t.Fatalf("SetEQ sent %v, want %v", got, want)
	}
	for i, cmd := range want {
		if got[i] != cmd {
			t.Errorf("command %d = %#04x, want %#04x", i, got[i], cmd)
		}
	}
}

func TestRequestIgnoresFramesFromAnotherVendor(t *testing.T) {
	// A frame with the same command ID but a different vendor must not be taken
	// for the answer.
	device := newFakeDevice(func(f gaia.Frame) []gaia.Frame {
		return []gaia.Frame{
			{
				Version: gaia.Version3,
				Vendor:  gaia.VendorQualcomm,
				Command: f.Command | gaia.MaskACK,
				Payload: []byte{0xFF},
			},
			ack(f.Command, 0x01),
		}
	})
	client := newClient(device)
	defer client.Close()

	on, err := client.ANC()
	if err != nil {
		t.Fatalf("ANC: %v", err)
	}
	if !on {
		t.Error("ANC() took the other vendor's frame as the answer")
	}
}

func TestUnsolicitedFrameBecomesNotification(t *testing.T) {
	device := newFakeDevice(deviceState(1, 40, 0))
	client := newClient(device)
	defer client.Close()

	device.push(ack(gaia.CmdGetANC, 0))

	select {
	case n := <-client.Notifications():
		if n.Command != gaia.CmdGetANC|gaia.MaskACK {
			t.Errorf("notification command = %#04x, want %#04x", n.Command, gaia.CmdGetANC|gaia.MaskACK)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for the notification")
	}
}

func TestRequestFailsWhenDeviceGoesAway(t *testing.T) {
	device := newFakeDevice(func(gaia.Frame) []gaia.Frame { return nil }) // never answers
	client := newClient(device)
	defer client.Close()

	device.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if _, err := client.Request(ctx, gaia.CmdGetANC); err == nil {
		t.Fatal("Request against a closed device succeeded, want an error")
	}
}

func TestRequestRespectsContextCancellation(t *testing.T) {
	client := newClient(newFakeDevice(func(gaia.Frame) []gaia.Frame { return nil }))
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := client.Request(ctx, gaia.CmdGetANC)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Request() error = %v, want context.DeadlineExceeded", err)
	}
}

func TestProbeOnlySendsGetters(t *testing.T) {
	device := newFakeDevice(func(f gaia.Frame) []gaia.Frame {
		if f.Command == 0x1005 {
			return []gaia.Frame{ack(f.Command, 0xAB)}
		}
		return nil // silence, as an unsupported command would give
	})
	client := newClient(device)
	defer client.Close()

	var supported []ProbeResult
	for res := range client.Probe(context.Background(), 0x1000, 0x1006, 50*time.Millisecond) {
		if res.Supported() {
			supported = append(supported, res)
		}
	}

	for _, cmd := range device.commandsSent() {
		if cmd&1 == 0 {
			t.Errorf("probe sent %#04x, which is an even (write) command", cmd)
		}
	}

	if len(supported) != 1 {
		t.Fatalf("probe found %d supported commands, want 1", len(supported))
	}
	if supported[0].Command != 0x1005 {
		t.Errorf("probe found %#04x, want 0x1005", supported[0].Command)
	}
	if len(supported[0].Payload) != 1 || supported[0].Payload[0] != 0xAB {
		t.Errorf("probe payload = % x, want ab", supported[0].Payload)
	}
}

func TestVerifyRejectsAnotherVendor(t *testing.T) {
	// Channel 14 on these headphones is an Airoha endpoint that accepts a
	// connection and answers in a protocol of its own; verify must not mistake
	// that for the control service.
	device := newFakeDevice(func(f gaia.Frame) []gaia.Frame {
		return []gaia.Frame{{
			Version: gaia.Version3,
			Vendor:  0x0055,
			Command: f.Command | gaia.MaskACK,
			Payload: []byte{0xEE, 0x10},
		}}
	})
	client := newClient(device)
	defer client.Close()

	if err := client.verify(); err == nil {
		t.Fatal("verify() accepted a reply from the wrong vendor")
	}
}

func TestVerifyAcceptsControlService(t *testing.T) {
	client := newClient(newFakeDevice(deviceState(1, 40, 0)))
	defer client.Close()

	if err := client.verify(); err != nil {
		t.Fatalf("verify() rejected the control service: %v", err)
	}
}

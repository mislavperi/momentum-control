package m4

import (
	"testing"

	"github.com/mislavperi/momentum-control/internal/gaia"
)

// Replies captured from a MOMENTUM 4 with a MacBook and a phone connected.
func multipointDevice() *fakeDevice {
	info := map[byte][]byte{
		0: append([]byte{0x00, 0x00, 0x01}, append([]byte("Mislav’s MacBook Pro"), 0)...),
		1: append([]byte{0x01, 0x01, 0x01}, append([]byte("Mislav's S23+"), 0)...),
		2: append([]byte{0x02, 0x02, 0x00}, append([]byte("Old Phone"), 0)...),
	}
	return newFakeDevice(func(f gaia.Frame) []gaia.Frame {
		switch f.Command {
		case gaia.CmdGetPairedCount:
			return []gaia.Frame{ack(f.Command, 0x00, 0x03)}
		case gaia.CmdGetOwnPairedIndex:
			return []gaia.Frame{ack(f.Command, 0x00)}
		case gaia.CmdGetMaxConnections:
			return []gaia.Frame{ack(f.Command, 0x02)}
		case gaia.CmdGetPairedInfo:
			return []gaia.Frame{ack(f.Command, info[f.Payload[0]]...)}
		}
		return []gaia.Frame{ack(f.Command)}
	})
}

func TestPairedHostsReadsTheDeviceList(t *testing.T) {
	client := newClient(multipointDevice())
	defer client.Close()

	hosts, err := client.PairedHosts()
	if err != nil {
		t.Fatalf("PairedHosts: %v", err)
	}
	want := []PairedHost{
		{Index: 0, Name: "Mislav’s MacBook Pro", Connected: true, Priority: 0, This: true},
		{Index: 1, Name: "Mislav's S23+", Connected: true, Priority: 1},
		{Index: 2, Name: "Old Phone", Connected: false, Priority: 2},
	}
	if len(hosts) != len(want) {
		t.Fatalf("got %d hosts, want %d", len(hosts), len(want))
	}
	for i := range want {
		if hosts[i] != want[i] {
			t.Errorf("host %d = %+v, want %+v", i, hosts[i], want[i])
		}
	}

	if n, err := client.MaxConnections(); err != nil || n != 2 {
		t.Errorf("MaxConnections = %d, %v; want 2", n, err)
	}
}

func TestHostActionsSendTheIndex(t *testing.T) {
	var got []gaia.Frame
	device := newFakeDevice(func(f gaia.Frame) []gaia.Frame {
		got = append(got, f)
		return []gaia.Frame{ack(f.Command)}
	})
	client := newClient(device)
	defer client.Close()

	_ = client.ConnectHost(2)
	_ = client.DisconnectHost(1)
	_ = client.ForgetHost(2)

	want := []struct {
		cmd   uint16
		index byte
	}{{gaia.CmdConnectPaired, 2}, {gaia.CmdDisconnectPaired, 1}, {gaia.CmdForgetPaired, 2}}
	if len(got) != len(want) {
		t.Fatalf("sent %d frames, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].Command != w.cmd || len(got[i].Payload) != 1 || got[i].Payload[0] != w.index {
			t.Errorf("frame %d = %#04x % x, want %#04x %02x", i, got[i].Command, got[i].Payload, w.cmd, w.index)
		}
	}
}

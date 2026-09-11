# momentum4-control

Control a Sennheiser MOMENTUM 4 from macOS: ANC, transparency, EQ, and whatever
else the protocol turns out to expose.

## Status

Working, verified against hardware: RFCOMM transport, GAIA framing, ANC and
transparency control, the desktop app, and the CLI (`status`, `anc`,
`transparency`, `bass`, `watch`, `raw`, `probe`).

Not implemented, because the commands are unknown: EQ bands and presets, battery
level, auto-off, sidetone, touch-control toggle, sound zones. Finding them needs
`m4ctl probe` to run while the control channel is available, see below.

Bass boost (`0x1008`/`0x1009`) is implemented but rejected as unsupported by the
firmware on the device this was developed against, so the app hides it.

## Building

```bash
make          # builds both bin/m4ctl.app and "bin/MOMENTUM 4.app"
open "bin/MOMENTUM 4.app"
./scripts/m4ctl status
```

Set `M4_DEBUG=1` to trace every frame in and out.

## The protocol

The headphones expose an SDP service named **GAIA**
(`A2129FF3-081B-4C45-8AFE-469D9C4842EC`) on Bluetooth Classic RFCOMM, MTU 666.
The channel number moves between connections, see below. Frames are GAIA-style:

```
 0      1        2       3        4..5       6..7      8..8+n   [+1]
+------+--------+-------+--------+----------+---------+--------+------+
| 0xFF | VERSION| FLAGS | LENGTH | VENDOR   | COMMAND | PAYLOAD| CHECK|
+------+--------+-------+--------+----------+---------+--------+------+
```

Big endian, `VERSION` 3, vendor `0x0495` (Sennheiser). `LENGTH` counts payload
bytes. The `CHECK` byte is an XOR of the preceding bytes and is only present when
bit 0 of `FLAGS` is set.

A reply to command `C` arrives as `C | 0x0100`, or `C | 0x0180` if the device
rejected it, in which case the first payload byte is a status code. Even command
IDs set a value and the following odd ID reads it back:

| Command  | Meaning          |
| -------- | ---------------- |
| `0x0007` | Register for notifications |
| `0x0602` / `0x0682` | Get charge state / notification |
| `0x0603` / `0x0683` | Get battery percentage / notification |
| `0x1002` / `0x1003` | Set / get equalizer |
| `0x1004` | Commit equalizer |
| `0x1006` | Set preamp |
| `0x1008` / `0x1009` | Set / get bass boost |
| `0x1201` | Get device info |
| `0x1202` | Get firmware version |
| `0x1206` | Get device name (NUL-terminated ASCII) |
| `0x1A00` / `0x1A01` / `0x1A81` | Set / get / notify sound mode |
| `0x1A02` / `0x1A03` | Set / get transparency (0-100) |
| `0x1A04` / `0x1A05` | Set / get ANC |

Everything below the first three rows was recovered from an HCI capture of
Sennheiser's own app; see `cmd/m4snoop`.

### Sound mode

`0x1A00` carries key/value byte pairs rather than a single value. Key `0x01` is
noise control: `0x00` off, `0x01` on, `0x02` adaptive. Keys `0x02` and `0x03` are
reported by the device but have not been identified. `0x1A81` arrives unsolicited
with every key when the setting changes.

### Equalizer

`0x1002` takes a 16-bit entry count followed by four-byte entries of
`01 <id> <value hi> <value lo>`, and `0x1003` reads them back given a list of the
IDs to fetch. Entry `0x01` is an overall preamp gain; the rest are grouped by
band, with the high nibble selecting band 1-4:

| Entry | Field | Observed |
| ----- | ----- | -------- |
| `0xN0` | filter type | `0x000B` on every band |
| `0xN1` | centre frequency, Hz | 360, 1200, 9000, 18000 |
| `0xN2` | gain, signed, hundredths of a dB | `0xFED4` is -3.00 dB |
| `0xN3` | Q factor | `0x0B50` |

A write is three commands in order: `0x1002` with the bands, `0x1006` with the
matching preamp, then `0x1004` to commit. Without the commit the change is
inaudible.

Verified against hardware — `ff 03 00 00 04 95 1a 05` (get ANC) answers
`ff 03 00 01 04 95 1b 05 01`.

There is no public specification; Qualcomm's GAIA v3 command reference is under
NDA. The command table above comes from
[f3Y0/momentum4-control](https://github.com/f3Y0/momentum4-control), confirmed
here against a real device. Anything beyond it has to be found by probing.

### Learning commands from the official app

The reliable way to learn a command is to watch Sennheiser's own app use it.
Android logs every HCI packet when *Bluetooth HCI snoop log* is enabled in
developer options, and `m4snoop` decodes the control frames out of that capture:

```bash
# on the phone: enable the snoop log, toggle Bluetooth off and on,
# then exercise every setting in Smart Control, pausing between each
adb bugreport bt.zip          # btsnoop_hci.log is inside, under FS/data/misc/bluetooth/logs/
go run ./cmd/m4snoop btsnoop_hci.log
```

It prints each frame with its direction and timing, then a summary of the
commands the app sent. Frames are found by scanning for the start-of-frame
marker and checking the vendor, rather than by reassembling RFCOMM, which is
enough for frames this small and keeps the tool short.

### Probing for unknown commands

`m4ctl probe` sweeps a range of command IDs and reports which the device answers.
It only sends odd IDs, since those read rather than write — but that is an
observed convention in an undocumented protocol, not a guarantee, so it requires
`-i-understand-the-risk`.

## The control channel is only intermittently available

The single most awkward thing about this device: it often refuses to open its
GAIA channel at all, answering every RFCOMM open with `kIOReturnError`
(`0xe00002bc`) for long stretches. During development it accepted a connection
twice in an afternoon and refused the rest of the time.

What did **not** make a difference, each tried repeatedly:

- restarting `bluetoothd` on the Mac
- power-cycling the headphones
- disconnecting the phone, so nothing else held a control session
- waiting quietly, up to ten minutes, with nothing touching the device
- which process asked, or whether it opened the channel via SDP or by scanning
- dialling within a second of the device connecting, in case the service only
  accepted connections just after link establishment. It does not: the app hooks
  IOBluetooth's connect notification, and four such dials were refused

While the control channel is refused, the Airoha/RACE service on another channel
still opens and streams data, so the Bluetooth link itself is healthy. The cause
is not understood. If you find the trigger, it belongs in this section.

The app is built around this rather than against it: it retries in the background
every 30 seconds and connects as soon as the device allows, so the flakiness
stays invisible unless you go looking.

Two things do make it worse, and both are avoided in the code:

- Leaving a channel open when the process dies. The app closes the channel on
  window close and on `SIGTERM`/`SIGINT`; do not `kill -9` it.
- Hammering the device with failed opens. Scanning every channel in a tight loop
  eventually gets even the working ones refused.

## Channel numbers move

The RFCOMM channel of the GAIA service is not stable: it was observed as 1 and as
2 on the same device on the same day, and the SDP record is sometimes stale,
advertising the previous value. HFP moved between 10 and 12 the same way.

So the channel is discovered, not assumed: try SDP first, then a short list of
candidates, and confirm each by sending a read and checking for a well-formed
Sennheiser reply. Confirmation is the part that matters. Channel 14 is an
Airoha/RACE endpoint that accepts the connection and answers in a protocol of its
own (`ff 55 02 00 ee 10`), so "the channel opened" proves nothing. Channel 10-12
is Hands-Free, in use by macOS for audio, and is never probed.

## Two macOS traps

Both of these fail silently, and cost real time to diagnose. They are why the
code is shaped the way it is.

**1. Classic Bluetooth has no socket API on Darwin.** There is no `AF_BLUETOOTH`;
that is Linux only. The only route to RFCOMM is Apple's IOBluetooth framework, so
`internal/rfcomm` is cgo over Objective-C. Pure-Go Bluetooth libraries are all
BLE, which is a different protocol and cannot reach this service.

**2. IOBluetooth needs a bundle identity *and* a running main run loop.**

Without an `Info.plist` declaring `NSBluetoothAlwaysUsageDescription`, and a code
signature to attach the grant to, TCC kills the process the moment it touches
Bluetooth — with `SIGABRT` and no message on stderr. That is why the CLI ships as
`bin/m4ctl.app` and is launched through `scripts/m4ctl`, which runs it via `open`
so LaunchServices gives it its bundle identity.

IOBluetooth is backed by CoreBluetooth and delivers *every* callback on the main
queue, which a Go program never services. Without a main run loop the channel
opens and writes succeed while not a single byte is ever delivered back. Hence
`rfcomm.Main`, which hands the main thread to `CFRunLoopRun` and runs the program
on a goroutine. A GUI toolkit that owns the main thread satisfies this by itself.

Those two combine into a third trap: the blocking `*Sync` IOBluetooth calls
cannot be made *from* the main thread, because the completion they wait for is
queued behind them on the main queue. So the sync calls run on a background
serial queue while the main thread stays free to deliver callbacks.

## Layout

| Path | Purpose |
| ---- | ------- |
| `internal/gaia` | Frame encoding and command IDs. Pure Go, unit tested. |
| `internal/rfcomm` | IOBluetooth RFCOMM transport (cgo + Objective-C). |
| `internal/m4` | Client: request/response matching, settings, probing. |
| `cmd/m4ctl` | Command line interface. |
| `cmd/momentum4` | Desktop app (Fyne). |
| `cmd/m4snoop` | Decodes control frames out of an Android HCI snoop log. |

## Credits

Protocol groundwork from
[f3Y0/momentum4-control](https://github.com/f3Y0/momentum4-control) (MIT).

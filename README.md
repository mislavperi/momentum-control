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
| `0x1008` / `0x1009` | Set / get bass boost |
| `0x1A02` / `0x1A03` | Set / get transparency (0-100) |
| `0x1A04` / `0x1A05` | Set / get ANC |

Verified against hardware — `ff 03 00 00 04 95 1a 05` (get ANC) answers
`ff 03 00 01 04 95 1b 05 01`.

There is no public specification; Qualcomm's GAIA v3 command reference is under
NDA. The command table above comes from
[f3Y0/momentum4-control](https://github.com/f3Y0/momentum4-control), confirmed
here against a real device. Anything beyond it has to be found by probing.

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

## Credits

Protocol groundwork from
[f3Y0/momentum4-control](https://github.com/f3Y0/momentum4-control) (MIT).

// Bluetooth Classic RFCOMM for macOS.
//
// macOS exposes no socket API for Classic Bluetooth (there is no AF_BLUETOOTH on
// Darwin), so the only route to an RFCOMM channel is Apple's IOBluetooth
// framework, which is backed by CoreBluetooth and delivers every callback on the
// main queue.
//
// That shapes this whole interface. The process main thread must run a Cocoa run
// loop (m4_run_main_loop) or no callback is ever delivered. And because the
// callbacks land on the main queue, the framework's *Sync calls cannot be used
// from the main thread: they would wait for a completion queued behind the
// caller. Everything here is therefore asynchronous, reporting results through
// the m4GoOn* callbacks implemented in Go.

#ifndef M4_RFCOMM_DARWIN_H
#define M4_RFCOMM_DARWIN_H

#include <stdint.h>

typedef struct M4Session M4Session;

// m4_run_main_loop hands the calling thread to a Cocoa run loop and returns only
// once m4_stop_main_loop is called. It must be running on the process main
// thread before any channel is opened.
void m4_run_main_loop(void);

// m4_stop_main_loop makes m4_run_main_loop return.
void m4_stop_main_loop(void);

// m4_main_loop_running reports whether the main run loop is currently running.
// This is true for CFRunLoopRun, but false for a GUI toolkit that pumps NSApp
// events itself, so prefer m4_main_queue_alive to decide whether callbacks can
// be delivered.
int m4_main_loop_running(void);

// m4_main_queue_alive reports whether the main dispatch queue is actually being
// drained, by posting a block to it and waiting up to timeout_ms for it to run.
// This is the condition IOBluetooth callbacks depend on, however the main thread
// happens to be driven.
int m4_main_queue_alive(int timeout_ms);

// m4_session_new allocates a session. handle is an opaque Go-side identifier
// echoed back to every callback.
M4Session *m4_session_new(uint64_t handle);

// m4_session_open starts connecting to the device at addr (a
// "xx-xx-xx-xx-xx-xx" Bluetooth address) and returns immediately; completion
// arrives as m4GoOnOpen.
//
// The channel number is discovered by looking up uuid (16 raw bytes of a 128-bit
// SDP service UUID) in the device's service records, running an SDP query first
// if the record is not already cached. Passing forced_channel > 0 skips discovery
// and connects to that channel number directly.
void m4_session_open(M4Session *s, const char *addr, const uint8_t *uuid,
                     int forced_channel);

// m4_session_write queues len bytes for sending and returns immediately;
// completion arrives as m4GoOnWrite. The buffer is copied, so the caller may
// reuse it at once.
void m4_session_write(M4Session *s, const void *buf, int len);

// m4_channel_id reports the RFCOMM channel number in use, or 0 before the
// channel is open.
int m4_channel_id(M4Session *s);

// m4_paired_devices returns the macOS paired-device list as a malloc'd string of
// "address\tname" lines, which the caller must free. Returns NULL if no devices
// are paired.
char *m4_paired_devices(void);

// m4_watch_connections asks IOBluetooth to report when any device connects,
// delivering each event to m4GoOnDeviceConnected. Registration happens on the
// main queue, which must be serviced for notifications to arrive.
//
// This exists because the control service appears to accept a connection only
// shortly after the Bluetooth link is established, so the app needs to dial the
// moment the device appears rather than on a timer.
void m4_watch_connections(void);

// m4_close closes the channel and releases the session. Passing NULL is a no-op.
// No further callbacks fire for the session once it is closed.
void m4_close(M4Session *s);

#endif // M4_RFCOMM_DARWIN_H

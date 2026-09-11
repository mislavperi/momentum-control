#import <Foundation/Foundation.h>
#import <IOBluetooth/IOBluetooth.h>

#include <stdlib.h>
#include <string.h>

#include "rfcomm_darwin.h"

// Implemented in Go (see rfcomm_darwin.go). All four are called on the main
// thread, so they must not block. Any errmsg passed to Go is malloc'd there and
// freed by the Go side.
extern void m4GoOnOpen(uint64_t handle, int status, char *errmsg);
extern void m4GoOnWrite(uint64_t handle, int status);
extern void m4GoOnData(uint64_t handle, void *buf, int len);
extern void m4GoOnClose(uint64_t handle);
extern void m4GoOnDeviceConnected(char *address);

static const NSTimeInterval kSDPTimeout = 10.0;

#pragma mark - Debug tracing

// Setting M4_DEBUG traces the IOBluetooth side of the channel to stderr. The
// protocol is undocumented, so seeing the raw exchange matters.
static BOOL debugEnabled(void) {
    static BOOL enabled;
    static dispatch_once_t once;
    dispatch_once(&once, ^{
        enabled = getenv("M4_DEBUG") != NULL;
    });
    return enabled;
}

static void debugLog(NSString *format, ...) {
    if (!debugEnabled()) {
        return;
    }
    va_list args;
    va_start(args, format);
    NSString *msg = [[NSString alloc] initWithFormat:format arguments:args];
    va_end(args);
    fprintf(stderr, "[rfcomm] %s\n", [msg UTF8String]);
}

#pragma mark - Main run loop

void m4_run_main_loop(void) {
    CFRunLoopRun();
}

void m4_stop_main_loop(void) {
    CFRunLoopStop(CFRunLoopGetMain());
}

int m4_main_loop_running(void) {
    CFStringRef mode = CFRunLoopCopyCurrentMode(CFRunLoopGetMain());
    if (mode == NULL) {
        return 0;
    }
    CFRelease(mode);
    return 1;
}

int m4_main_queue_alive(int timeout_ms) {
    if ([NSThread isMainThread]) {
        return 1;
    }

    // What actually matters is whether the main queue is being drained, not how.
    // CFRunLoopRun drains it, but so does a GUI toolkit pumping NSApp events, and
    // in that case the main run loop reports no current mode. Posting a block and
    // seeing whether it runs tests the thing we depend on rather than a proxy
    // for it.
    dispatch_semaphore_t ran = dispatch_semaphore_create(0);
    dispatch_async(dispatch_get_main_queue(), ^{
        dispatch_semaphore_signal(ran);
    });

    dispatch_time_t deadline = dispatch_time(DISPATCH_TIME_NOW,
                                             (int64_t)timeout_ms * NSEC_PER_MSEC);
    return dispatch_semaphore_wait(ran, deadline) == 0 ? 1 : 0;
}

// IOBluetooth's blocking *Sync calls are the ones that actually work, but they
// cannot run on the main thread: their completion is delivered on the main queue,
// so they would be waiting on themselves. They run here instead, on one serial
// background queue, leaving the main run loop free to deliver callbacks.
static dispatch_queue_t workQueue(void) {
    static dispatch_queue_t queue;
    static dispatch_once_t once;
    dispatch_once(&once, ^{
        queue = dispatch_queue_create("com.momentum4.bluetooth", DISPATCH_QUEUE_SERIAL);
    });
    return queue;
}

static void onWorkQueue(void (^block)(void)) {
    dispatch_async(workQueue(), block);
}

// Registration for IOBluetooth notifications belongs on the main thread, since
// that is where the framework delivers them.
static void onMainQueue(void (^block)(void)) {
    if ([NSThread isMainThread]) {
        block();
    } else {
        dispatch_async(dispatch_get_main_queue(), block);
    }
}

static char *copyError(NSString *msg) {
    if (msg == nil) {
        msg = @"unknown IOBluetooth error";
    }
    return strdup([msg UTF8String]);
}

#pragma mark - Channel session

@interface M4Channel : NSObject <IOBluetoothRFCOMMChannelDelegate>
@property(nonatomic, strong) IOBluetoothRFCOMMChannel *channel;
@property(nonatomic, assign) uint64_t handle;
@property(nonatomic, assign) BluetoothRFCOMMChannelID channelID;
@property(nonatomic, strong) dispatch_semaphore_t sdpSignal;
@property(nonatomic, assign) IOReturn sdpStatus;
@property(nonatomic, assign) BOOL closed;
@property(nonatomic, assign) BOOL openReported;
@end

@implementation M4Channel

// reportOpen delivers the open result to Go exactly once, so a failure part way
// through cannot be followed by a late success from the delegate.
- (void)reportOpen:(IOReturn)status message:(NSString *)msg {
    if (self.openReported) {
        return;
    }
    self.openReported = YES;
    debugLog(@"open complete, status 0x%08x %@", status, msg ?: @"");
    m4GoOnOpen(self.handle, (int)status, status == kIOReturnSuccess ? NULL : copyError(msg));
}

// Resolves the RFCOMM channel number advertised for the given service UUID,
// running an SDP query first if macOS has not cached the record.
- (BOOL)resolveChannelOn:(IOBluetoothDevice *)device
                    uuid:(IOBluetoothSDPUUID *)uuid
                   error:(NSString **)errOut {
    IOBluetoothSDPServiceRecord *record = [device getServiceRecordForUUID:uuid];

    if (record == nil) {
        self.sdpSignal = dispatch_semaphore_create(0);
        IOReturn status = [device performSDPQuery:self];
        if (status != kIOReturnSuccess) {
            *errOut = [NSString stringWithFormat:@"SDP query failed to start (0x%08x)", status];
            return NO;
        }
        // The completion arrives on the main queue; this runs on the work queue,
        // so waiting on a semaphore here does not block its delivery.
        dispatch_time_t deadline = dispatch_time(DISPATCH_TIME_NOW,
                                                 (int64_t)(kSDPTimeout * NSEC_PER_SEC));
        if (dispatch_semaphore_wait(self.sdpSignal, deadline) != 0) {
            *errOut = @"SDP query timed out; is the headset connected?";
            return NO;
        }
        if (self.sdpStatus != kIOReturnSuccess) {
            *errOut = [NSString stringWithFormat:@"SDP query failed (0x%08x)", self.sdpStatus];
            return NO;
        }
        record = [device getServiceRecordForUUID:uuid];
    }

    if (record == nil) {
        *errOut = @"device does not advertise the requested service";
        return NO;
    }

    BluetoothRFCOMMChannelID channelID = 0;
    IOReturn status = [record getRFCOMMChannelID:&channelID];
    if (status != kIOReturnSuccess || channelID == 0) {
        *errOut = @"service record carries no RFCOMM channel ID";
        return NO;
    }
    self.channelID = channelID;
    debugLog(@"resolved service '%@' to channel %d", [record getServiceName], channelID);
    return YES;
}

- (void)openDevice:(NSString *)addr uuid:(IOBluetoothSDPUUID *)uuid forced:(int)forced {
    IOBluetoothDevice *device = [IOBluetoothDevice deviceWithAddressString:addr];
    if (device == nil) {
        [self reportOpen:kIOReturnNoDevice
                 message:[NSString stringWithFormat:@"no Bluetooth device at address %@", addr]];
        return;
    }
    if (!device.isConnected) {
        debugLog(@"device %@ is paired but not currently connected", addr);
    }

    if (forced > 0) {
        self.channelID = (BluetoothRFCOMMChannelID)forced;
    } else {
        NSString *err = nil;
        if (![self resolveChannelOn:device uuid:uuid error:&err]) {
            [self reportOpen:kIOReturnNotFound message:err];
            return;
        }
    }

    IOBluetoothRFCOMMChannel *channel = nil;
    IOReturn status = [device openRFCOMMChannelSync:&channel
                                      withChannelID:self.channelID
                                           delegate:self];
    if (status != kIOReturnSuccess || channel == nil) {
        [self reportOpen:status
                 message:[NSString stringWithFormat:@"could not open RFCOMM channel %d (0x%08x)",
                                                    self.channelID, status]];
        return;
    }
    self.channel = channel;
    debugLog(@"channel %d open, mtu %d", self.channelID, [channel getMTU]);
    [self reportOpen:kIOReturnSuccess message:nil];
}

- (void)writeData:(NSData *)data {
    if (self.closed || self.channel == nil) {
        m4GoOnWrite(self.handle, kIOReturnNotOpen);
        return;
    }

    // Writes larger than the channel MTU are rejected outright, so split them.
    BluetoothRFCOMMMTU mtu = [self.channel getMTU];
    if (mtu == 0) {
        mtu = 255;
    }

    const uint8_t *bytes = (const uint8_t *)data.bytes;
    NSUInteger remaining = data.length;
    while (remaining > 0) {
        UInt16 n = (UInt16)MIN((NSUInteger)mtu, remaining);
        NSData *slice = [NSData dataWithBytes:bytes length:n];

        debugLog(@"send %u bytes: %@", (unsigned)n, slice);

        IOReturn status = [self.channel writeSync:(void *)slice.bytes length:n];
        if (status != kIOReturnSuccess) {
            m4GoOnWrite(self.handle, (int)status);
            return;
        }
        bytes += n;
        remaining -= n;
    }
    m4GoOnWrite(self.handle, kIOReturnSuccess);
}

- (void)shutdown {
    if (self.closed) {
        return;
    }
    self.closed = YES;
    IOBluetoothRFCOMMChannel *channel = self.channel;
    self.channel = nil;
    // Drop the delegate first so no callback reaches Go after close returns.
    [channel setDelegate:nil];
    [channel closeChannel];
}

#pragma mark IOBluetoothDevice SDP delegate

- (void)sdpQueryComplete:(IOBluetoothDevice *)device status:(IOReturn)status {
    self.sdpStatus = status;
    if (self.sdpSignal != nil) {
        dispatch_semaphore_signal(self.sdpSignal);
    }
}

#pragma mark IOBluetoothRFCOMMChannel delegate

- (void)rfcommChannelOpenComplete:(IOBluetoothRFCOMMChannel *)channel status:(IOReturn)status {
    // openRFCOMMChannelSync already reported the result; this only matters if it
    // failed after returning, and reportOpen ignores the duplicate otherwise.
    if (status != kIOReturnSuccess) {
        [self reportOpen:status
                 message:[NSString stringWithFormat:@"RFCOMM channel %d refused the connection (0x%08x)",
                                                    self.channelID, status]];
    }
}

- (void)rfcommChannelData:(IOBluetoothRFCOMMChannel *)channel
                     data:(void *)data
                   length:(size_t)length {
    debugLog(@"recv %zu bytes: %@", length, [NSData dataWithBytes:data length:length]);
    if (self.closed || length == 0) {
        return;
    }
    m4GoOnData(self.handle, data, (int)length);
}

- (void)rfcommChannelClosed:(IOBluetoothRFCOMMChannel *)channel {
    if (self.closed) {
        return;
    }
    self.closed = YES;
    debugLog(@"channel closed by device");
    // An open still in flight must fail rather than hang.
    [self reportOpen:kIOReturnAborted message:@"channel closed before it finished opening"];
    m4GoOnClose(self.handle);
}

@end

#pragma mark - Connection notifications

// M4Watcher reports device connections. IOBluetooth delivers these to a selector
// rather than a block, so it needs an object to hang it on.
@interface M4Watcher : NSObject
@end

@implementation M4Watcher

- (void)connected:(IOBluetoothUserNotification *)note fromDevice:(IOBluetoothDevice *)device {
    debugLog(@"device connected: %@ (%@)", device.addressString, device.name);
    m4GoOnDeviceConnected(strdup([device.addressString UTF8String]));
}

@end

void m4_watch_connections(void) {
    static M4Watcher *watcher;
    static dispatch_once_t once;
    dispatch_once(&once, ^{
        watcher = [[M4Watcher alloc] init];
    });

    onMainQueue(^{
        [IOBluetoothDevice registerForConnectNotifications:watcher
                                                  selector:@selector(connected:fromDevice:)];
    });
}

#pragma mark - C entry points

struct M4Session {
    void *channel; // retained M4Channel
};

M4Session *m4_session_new(uint64_t handle) {
    @autoreleasepool {
        M4Session *session = calloc(1, sizeof(M4Session));
        if (session == NULL) {
            return NULL;
        }
        M4Channel *chan = [[M4Channel alloc] init];
        chan.handle = handle;
        session->channel = (__bridge_retained void *)chan;
        return session;
    }
}

void m4_session_open(M4Session *s, const char *addr, const uint8_t *uuid, int forced_channel) {
    @autoreleasepool {
        if (s == NULL || s->channel == NULL) {
            return;
        }
        M4Channel *chan = (__bridge M4Channel *)s->channel;
        NSString *address = [NSString stringWithUTF8String:addr];

        IOBluetoothSDPUUID *serviceUUID = nil;
        if (uuid != NULL) {
            serviceUUID = [IOBluetoothSDPUUID uuidWithBytes:uuid length:16];
        }

        onWorkQueue(^{
            [chan openDevice:address uuid:serviceUUID forced:forced_channel];
        });
    }
}

void m4_session_write(M4Session *s, const void *buf, int len) {
    @autoreleasepool {
        if (s == NULL || s->channel == NULL) {
            return;
        }
        M4Channel *chan = (__bridge M4Channel *)s->channel;
        NSData *data = [NSData dataWithBytes:buf length:(NSUInteger)len];
        onWorkQueue(^{
            [chan writeData:data];
        });
    }
}

int m4_channel_id(M4Session *s) {
    if (s == NULL || s->channel == NULL) {
        return 0;
    }
    M4Channel *chan = (__bridge M4Channel *)s->channel;
    return (int)chan.channelID;
}

char *m4_paired_devices(void) {
    @autoreleasepool {
        NSArray *devices = [IOBluetoothDevice pairedDevices];
        if (devices.count == 0) {
            return NULL;
        }
        NSMutableString *out = [NSMutableString string];
        for (IOBluetoothDevice *device in devices) {
            NSString *name = device.name ?: @"";
            [out appendFormat:@"%@\t%@\n", device.addressString, name];
        }
        return strdup([out UTF8String]);
    }
}

void m4_close(M4Session *s) {
    @autoreleasepool {
        if (s == NULL || s->channel == NULL) {
            return;
        }
        M4Channel *chan = (__bridge_transfer M4Channel *)s->channel;
        s->channel = NULL;
        onWorkQueue(^{
            [chan shutdown];
        });
        free(s);
    }
}

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

// Every IOBluetooth call is made on the main thread. The framework is not
// thread-safe and ties delegate delivery to the main run loop; driving it from a
// background queue made the headset refuse RFCOMM opens with kIOReturnError
// (0xe00002bc) most of the time while occasionally letting one through. Because
// the main thread also delivers the completions, nothing here may block waiting
// for one: opens are asynchronous and finish in the delegate callbacks.
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
// Held for the life of the session so the device outlives an asynchronous open.
@property(nonatomic, strong) IOBluetoothDevice *device;
@property(nonatomic, assign) uint64_t handle;
@property(nonatomic, assign) BluetoothRFCOMMChannelID channelID;
@property(nonatomic, strong) IOBluetoothSDPUUID *serviceUUID;
@property(nonatomic, assign) BOOL closed;
@property(nonatomic, assign) BOOL openReported;
@property(nonatomic, assign) BOOL opened;
@property(nonatomic, assign) BOOL shutDown;
// Self-reference held while an SDP query is in flight. IOBluetooth does not
// retain the query's target, so without this a close during the query would free
// the object before sdpQueryComplete:status: is sent to it.
@property(nonatomic, strong) M4Channel *pendingQuery;
@end

// deviceForAddress prefers the instance macOS already holds for a paired device
// over building a fresh one from an address string.
static IOBluetoothDevice *deviceForAddress(NSString *addr) {
    NSString *wanted = [[addr stringByReplacingOccurrencesOfString:@":" withString:@"-"] lowercaseString];

    for (IOBluetoothDevice *device in [IOBluetoothDevice pairedDevices]) {
        if ([[device.addressString lowercaseString] isEqualToString:wanted]) {
            return device;
        }
    }
    return [IOBluetoothDevice deviceWithAddressString:addr];
}

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

// openDevice starts the connection. When the service record is already cached
// the channel is opened straight away; otherwise an SDP query runs first and the
// open continues from sdpQueryComplete:.
//
// Runs on the main thread and never blocks: the completions it depends on are
// delivered on that same thread.
- (void)openDevice:(NSString *)addr uuid:(IOBluetoothSDPUUID *)uuid forced:(int)forced {
    if (self.closed) {
        return;
    }
    IOBluetoothDevice *device = deviceForAddress(addr);
    self.device = device;

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
        [self openChannelOn:device];
        return;
    }

    if (uuid == nil) {
        [self reportOpen:kIOReturnBadArgument message:@"no service UUID and no channel given"];
        return;
    }
    self.serviceUUID = uuid;

    if ([device getServiceRecordForUUID:uuid] != nil) {
        [self openResolvedChannelOn:device];
        return;
    }

    self.pendingQuery = self;
    IOReturn status = [device performSDPQuery:self uuids:@[uuid]];
    if (status != kIOReturnSuccess) {
        self.pendingQuery = nil;
        [self reportOpen:status
                 message:[NSString stringWithFormat:@"SDP query failed to start (0x%08x)", status]];
    }
}

// openResolvedChannelOn reads the channel number out of the cached service
// record and opens it.
- (void)openResolvedChannelOn:(IOBluetoothDevice *)device {
    IOBluetoothSDPServiceRecord *record = [device getServiceRecordForUUID:self.serviceUUID];
    if (record == nil) {
        [self reportOpen:kIOReturnNotFound
                 message:@"device does not advertise the requested service"];
        return;
    }

    BluetoothRFCOMMChannelID channelID = 0;
    IOReturn status = [record getRFCOMMChannelID:&channelID];
    if (status != kIOReturnSuccess || channelID == 0) {
        [self reportOpen:kIOReturnNotFound
                 message:@"service record carries no RFCOMM channel ID"];
        return;
    }

    self.channelID = channelID;
    debugLog(@"resolved service '%@' to channel %d", [record getServiceName], channelID);
    [self openChannelOn:device];
}

// openChannelOn starts the RFCOMM open. The result arrives in
// rfcommChannelOpenComplete:status:.
- (void)openChannelOn:(IOBluetoothDevice *)device {
    if (self.closed) {
        return;
    }
    IOBluetoothRFCOMMChannel *channel = nil;
    IOReturn status = [device openRFCOMMChannelAsync:&channel
                                       withChannelID:self.channelID
                                            delegate:self];
    if (status != kIOReturnSuccess) {
        [self reportOpen:status
                 message:[NSString stringWithFormat:@"could not open RFCOMM channel %d (0x%08x)",
                                                    self.channelID, status]];
        return;
    }
    // Kept now so a close during the open can still tear the channel down.
    self.channel = channel;
    debugLog(@"opening channel %d", self.channelID);
}

- (void)writeData:(NSData *)data {
    if (self.closed || self.channel == nil || !self.opened) {
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
    // Not keyed on closed: after the device closes the channel, the delegate
    // still has to be cleared here before this object goes away.
    if (self.shutDown) {
        return;
    }
    self.shutDown = YES;
    self.closed = YES;
    IOBluetoothRFCOMMChannel *channel = self.channel;
    self.channel = nil;
    self.device = nil;
    // Drop the delegate first so no callback reaches Go after close returns.
    [channel setDelegate:nil];
    [channel closeChannel];
}

#pragma mark IOBluetoothDevice SDP delegate

- (void)sdpQueryComplete:(IOBluetoothDevice *)device status:(IOReturn)status {
    // Released at the end of this call, once nothing below still needs self.
    M4Channel *keepAlive = self.pendingQuery;
    self.pendingQuery = nil;
    (void)keepAlive;
    if (self.closed) {
        return;
    }
    if (status != kIOReturnSuccess) {
        [self reportOpen:status
                 message:[NSString stringWithFormat:@"SDP query failed (0x%08x)", status]];
        return;
    }
    [self openResolvedChannelOn:device];
}

#pragma mark IOBluetoothRFCOMMChannel delegate

- (void)rfcommChannelOpenComplete:(IOBluetoothRFCOMMChannel *)channel status:(IOReturn)status {
    if (self.closed) {
        return;
    }
    if (status != kIOReturnSuccess) {
        [self reportOpen:status
                 message:[NSString stringWithFormat:@"RFCOMM channel %d refused the connection (0x%08x)",
                                                    self.channelID, status]];
        return;
    }
    self.channel = channel;
    self.opened = YES;
    debugLog(@"channel %d open, mtu %d", self.channelID, [channel getMTU]);
    [self reportOpen:kIOReturnSuccess message:nil];
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

        onMainQueue(^{
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
        onMainQueue(^{
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

int m4_device_connected(const char *addr) {
    @autoreleasepool {
        if (addr == NULL) {
            return 0;
        }
        IOBluetoothDevice *device = deviceForAddress([NSString stringWithUTF8String:addr]);
        return device != nil && device.isConnected ? 1 : 0;
    }
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
        onMainQueue(^{
            [chan shutdown];
        });
        free(s);
    }
}

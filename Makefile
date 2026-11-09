# macOS will not let a process touch Bluetooth unless it has a bundle identity
# and an Info.plist declaring NSBluetoothAlwaysUsageDescription; a bare binary is
# killed by TCC on first IOBluetooth call ("attempted to access privacy-sensitive
# data without a usage description"). Everything therefore ships as an .app, and
# scripts/m4ctl runs the CLI bundle through LaunchServices.

BIN_DIR   := bin
CLI_APP   := $(BIN_DIR)/m4ctl.app
CLI_BIN   := $(CLI_APP)/Contents/MacOS/m4ctl
CLI_PLIST := build/cli-Info.plist
GUI_APP   := $(BIN_DIR)/MOMENTUM 4.app
GUI_BIN   := $(GUI_APP)/Contents/MacOS/momentum4
GUI_PLIST := build/app-Info.plist

SOURCES := $(shell find cmd internal -name '*.go' -o -name '*.m' -o -name '*.h')

GO ?= go

.PHONY: all cli gui test vet clean

# Only macOS needs app bundles and code signing. Linux and Windows build plain
# binaries: their Bluetooth stacks need no bundle identity or usage string.
ifeq ($(shell uname -s),Darwin)

all: cli gui

cli: $(CLI_BIN)

gui: $(SOURCES) $(GUI_PLIST)
	@mkdir -p "$(GUI_APP)/Contents/MacOS"
	cp $(GUI_PLIST) "$(GUI_APP)/Contents/Info.plist"
	$(GO) build -o "$(GUI_BIN)" ./cmd/momentum4
	codesign --force --sign - "$(GUI_APP)"

$(CLI_BIN): $(SOURCES) $(CLI_PLIST)
	@mkdir -p $(CLI_APP)/Contents/MacOS
	cp $(CLI_PLIST) $(CLI_APP)/Contents/Info.plist
	$(GO) build -o $@ ./cmd/m4ctl
	codesign --force --sign - $(CLI_APP)

else

EXE := $(if $(filter Windows_NT,$(OS)),.exe,)

all: cli gui

cli:
	$(GO) build -o $(BIN_DIR)/m4ctl$(EXE) ./cmd/m4ctl

gui:
	$(GO) build -o $(BIN_DIR)/momentum4$(EXE) ./cmd/momentum4

endif

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

clean:
	rm -rf $(BIN_DIR)

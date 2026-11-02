#!/bin/sh
# Xcode's pre-build step: builds ios/bridge as libglane.a for the SDK being
# built (device or simulator). Go's iOS port needs cgo, hence Xcode's clang.
set -eu

# Xcode runs this with a bare PATH: find Go where mise or Homebrew put it
export PATH="$HOME/.local/bin:/opt/homebrew/bin:/usr/local/bin:/usr/local/go/bin:$PATH"
go() { if command -v mise >/dev/null; then mise exec -- go "$@"; else command go "$@"; fi; }

# shortcut: arm64 only (phones, Apple Silicon simulators); an Intel Mac's simulator needs an amd64 build
target="arm64-apple-ios$IPHONEOS_DEPLOYMENT_TARGET"
[ "$PLATFORM_NAME" = iphonesimulator ] && target="$target-simulator"

cd "$SRCROOT/.."
GOOS=ios GOARCH=arm64 CGO_ENABLED=1 \
	CC="$(xcrun --sdk "$PLATFORM_NAME" -f clang)" \
	CGO_CFLAGS="-isysroot $SDKROOT -target $target" \
	CGO_LDFLAGS="-isysroot $SDKROOT -target $target" \
	go build -trimpath -buildmode=c-archive -ldflags "-s -w" -o "$DERIVED_FILE_DIR/libglane.a" ./ios/bridge

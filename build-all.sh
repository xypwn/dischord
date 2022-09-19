#! /usr/bin/env sh

SRC=cmd/dischord/*.go
BUILDDIR="build"

BUILD_CMDS=""

BUILDS=0
DONE=0

[ "$1" = "clean" ] && echo "Cleaning $BUILDDIR" && rm -rf "$BUILDDIR" && exit 0

do_build() {
	[ "$OS" = "windows" ] && EXT=".exe"
	env GOOS="$OS" GOARCH="$ARCH" CGO_ENABLED=0 go build -ldflags "-s -w" -o "$BUILDDIR/$NAME$EXT" $SRC
}

add() {
	BUILDS=$((BUILDS+1))
	BUILD_CMDS=$(printf "%s\n%s" "$BUILD_CMDS" "$1 $2 $3")
}

build() {
	mkdir -p "$BUILDDIR"
	BUILD_CMDS="$(printf "%s" "$BUILD_CMDS" | grep '.')"
	while IFS= read -r l; do
		OS=$(echo $l|cut -d\  -f1)
		ARCH=$(echo $l|cut -d\  -f2)
		NAME=$(echo $l|cut -d\  -f3)
		echo "Building for $OS on $ARCH"
		printf "%s\r" "Progress: $((DONE*100/BUILDS))%"
		do_build
		DONE=$((DONE+1))
	done << EOF
$BUILD_CMDS
EOF
	echo "Progress: DONE"
}

# see `go tool dist list` for possible configs
add linux   386   dischord-linux-x86
add linux   amd64 dischord-linux-amd64
add linux   arm   dischord-linux-arm32
add linux   arm64 dischord-linux-arm64
add darwin  arm64 dischord-macos-apple-silicon
add darwin  amd64 dischord-macos-intel
add windows 386   dischord-windows-x86
add windows amd64 dischord-windows-amd64
build

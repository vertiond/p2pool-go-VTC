#!/usr/bin/env bash
# Runs inside the Guix container, with no network access: builds the node for
# $HOST from the source archive and the modules fetch.sh downloaded, and writes
# the zip, the bare binary and their SHA256SUMS.part to /outdir. The zip holds
# what the CI build's does: the binary, web/static, LICENSE and README.md.
export LC_ALL=C
export TZ=UTC
set -o errexit -o pipefail -o nounset
umask 0022

: "${HOST:?}" "${NAME:?}" "${GO_VERSION:?}" "${SOURCE_DATE_EPOCH:?}" "${SRC_TAR:?}" "${JOBS:?}"
export SOURCE_DATE_EPOCH

export HOME=/tmp/home
mkdir -p "$HOME"
tar -xf "$SRC_TAR" -C /distsrc
cd /distsrc/src

# Guix's Go points net at its own /etc/services and /etc/protocols and adds
# its tzdata to time's zoneinfo search list, paths that exist only under
# /gnu/store. Undo both on a copy of GOROOT, so the standard library is
# upstream Go's and the binary behaves as CI's does
cp -rL "$(go env GOROOT)" /distsrc/goroot
chmod -R u+w /distsrc/goroot
sed -i 's|/gnu/store/[^/]*-net-base-[^/]*/etc/|/etc/|g' \
    /distsrc/goroot/src/net/lookup_unix.go /distsrc/goroot/src/net/port_unix.go
# Guix adds a blank line and its tzdata line; drop both, or every line after
# them moves and the binary's line table differs from upstream Go's
sed -i '/^var platformZoneSources/{n;/^$/{N;\|^\n"/gnu/store/[^"]*-tzdata-[^"]*/share/zoneinfo",$|d}}' \
    /distsrc/goroot/src/time/zoneinfo_unix.go
if grep -n /gnu/store /distsrc/goroot/src/net/lookup_unix.go \
        /distsrc/goroot/src/net/port_unix.go /distsrc/goroot/src/time/zoneinfo_unix.go; then
    echo "ERR: GOROOT still names /gnu/store" >&2; exit 1
fi
export GOROOT=/distsrc/goroot
export PATH="${GOROOT}/bin:${PATH}"

export GOTOOLCHAIN=local
export GOMODCACHE=/cache/gomod
export GOCACHE=/distsrc/gocache
export GOPROXY=off
export GOSUMDB=off
export GOFLAGS="-mod=readonly -modcacherw -trimpath -buildvcs=false"
export GOMAXPROCS="$JOBS"
export CGO_ENABLED=0
export GOARCH=amd64
go version
go mod verify

case "$HOST" in
    x86_64-linux-gnu)
        export GOOS=linux
        binary=p2pool-go-vtc
        zipname="p2pool-go-vtc-${NAME}-linux-x64.zip"
        ;;
    x86_64-w64-mingw32)
        export GOOS=windows
        binary=p2pool-go-vtc.exe
        zipname="p2pool-go-vtc-${NAME}-windows-x64.zip"
        ;;
    *)
        echo "ERR: unsupported host $HOST" >&2
        exit 1
        ;;
esac

echo "== go build ($HOST, version ${GO_VERSION})"
mkdir -p stage/web
go build -ldflags "-s -w -buildid= -X github.com/CADMonkey21/p2pool-go-VTC/config.Version=${GO_VERSION}" -o "stage/${binary}" .
if grep -a -o '/gnu/store/[^"[:cntrl:] ]*' "stage/${binary}"; then
    echo "ERR: the binary names /gnu/store" >&2; exit 1
fi
cp -r web/static stage/web/
cp LICENSE README.md stage/

# A zip records each file's time and permissions and the order it found them
# in; fix all three
cd stage
chmod 0755 "$binary"
find . -exec touch --no-dereference --date="@${SOURCE_DATE_EPOCH}" {} +
find "$binary" web LICENSE README.md | sort | zip -X -9 -@ "/outdir/${zipname}"
cp "$binary" "/outdir/${binary}"
cd /outdir
sha256sum "$zipname" "$binary" > SHA256SUMS.part
cat SHA256SUMS.part

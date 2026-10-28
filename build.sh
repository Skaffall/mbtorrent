#!/usr/bin/env bash
# Кросс-сборка mbtorrent под Windows из Linux/macOS.
# Требуется только Go — никаких Qt, MSVC и установщиков.
set -euo pipefail
cd "$(dirname "$0")"

VERSION="$(sed -n 's/^var Version = "\(.*\)"/\1/p' internal/buildinfo/buildinfo.go)"
mkdir -p dist

build() {
  local arch="$1"
  echo "→ windows/$arch"
  CGO_ENABLED=0 GOOS=windows GOARCH="$arch" \
    go build -trimpath -buildvcs=false \
    -ldflags "-s -w -H=windowsgui -X mbtorrent/internal/buildinfo.Version=${VERSION}" \
    -o "dist/mbtorrent-${arch}.exe" .
}

build amd64
build arm64

cp -f dist/mbtorrent-amd64.exe dist/mbtorrent.exe
ls -lh dist/*.exe
echo "Готово. Запустите dist/mbtorrent.exe"

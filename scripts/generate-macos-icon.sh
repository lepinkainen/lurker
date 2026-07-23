#!/bin/sh

set -eu

project_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
source_svg="$project_root/web/public/favicon.svg"
iconset="$project_root/macos/Lurker/Assets.xcassets/AppIcon.appiconset"

if ! command -v rsvg-convert >/dev/null 2>&1; then
  printf 'rsvg-convert not found. Install it with: brew install librsvg\n' >&2
  exit 1
fi

mkdir -p "$iconset"

render() {
  size=$1
  filename=$2
  rsvg-convert --width "$size" --height "$size" \
    --output "$iconset/$filename" "$source_svg"
}

render 16 icon_16x16.png
render 32 icon_16x16@2x.png
render 32 icon_32x32.png
render 64 icon_32x32@2x.png
render 128 icon_128x128.png
render 256 icon_128x128@2x.png
render 256 icon_256x256.png
render 512 icon_256x256@2x.png
render 512 icon_512x512.png
render 1024 icon_512x512@2x.png

printf 'Generated macOS AppIcon images in %s from %s\n' "$iconset" "$source_svg"

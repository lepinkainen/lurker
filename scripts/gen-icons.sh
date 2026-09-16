#!/bin/sh
#
# Regenerate raster icons from web/public/favicon.svg, which is the single
# source of truth for every app icon in the repo. Nothing here should be edited
# by hand; change the SVG and rebuild.
#
# Usage: gen-icons.sh [web|desktop|all]
#
# The Apple iconset has its own script (generate-apple-icon.sh) because it is
# macOS-only and already keyed to the same SVG.

set -eu

target=${1:-all}

project_root=$(unset CDPATH; cd -- "$(dirname -- "$0")/.." && pwd)
svg="$project_root/web/public/favicon.svg"

# Backdrop for the icons that must not be transparent: a maskable icon is
# cropped to an arbitrary shape by the launcher, and iOS composites the home
# screen icon onto white. Matches the eye-shape fill in favicon.svg.
bg='#0f1923'

if ! command -v rsvg-convert >/dev/null 2>&1; then
  printf 'rsvg-convert not found. Install librsvg (brew install librsvg, dnf install librsvg2-tools, apt install librsvg2-bin)\n' >&2
  exit 1
fi

if command -v magick >/dev/null 2>&1; then
  magick_cmd=magick
elif command -v convert >/dev/null 2>&1; then
  magick_cmd=convert
else
  printf 'ImageMagick not found. Install it (brew install imagemagick, dnf install ImageMagick, apt install imagemagick)\n' >&2
  exit 1
fi

tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT

# render SIZE OUT -- square render, transparent background.
render() {
  rsvg-convert --width "$1" --height "$1" --output "$2" "$svg"
}

# render_inset CANVAS INNER OUT -- artwork at INNER px, centred on an opaque
# CANVAS px square. INNER < CANVAS leaves the safe-zone padding a maskable icon
# needs so the launcher's crop never clips the artwork.
# POSIX sh has no local variables, so these are prefixed to keep them from
# clobbering the caller's -- an earlier version used a bare `out` and silently
# overwrote the destination directory held by gen_web.
render_inset() {
  ri_canvas=$1
  ri_inner=$2
  ri_out=$3
  render "$ri_inner" "$tmpdir/inner.png"
  "$magick_cmd" -size "${ri_canvas}x${ri_canvas}" "xc:$bg" "$tmpdir/inner.png" \
    -gravity center -composite -alpha remove -alpha off "$ri_out"
}

gen_web() {
  web_dir="$project_root/web/public"
  render 192 "$web_dir/icon-192.png"
  render 512 "$web_dir/icon-512.png"
  # 75% of the canvas: inside the 80% safe zone the maskable spec requires.
  render_inset 512 384 "$web_dir/icon-maskable-512.png"
  render_inset 180 148 "$web_dir/apple-touch-icon.png"
  printf 'web icons     -> %s\n' "${web_dir#"$project_root"/}"
}

gen_desktop() {
  desk_dir="$project_root/desktop/icons"
  render 32 "$desk_dir/32x32.png"
  render 64 "$desk_dir/64x64.png"
  render 128 "$desk_dir/128x128.png"
  render 256 "$desk_dir/128x128@2x.png"
  render 512 "$desk_dir/icon.png"
  # Windows and macOS bundle icons. Tauri only reads these when bundling for
  # those targets, which this repo does not ship -- macOS has the native Swift
  # app -- but they are listed in tauri.conf.json, so keep them in step rather
  # than leaving a stale icon behind a format nobody looks at.
  render 1024 "$tmpdir/master.png"
  "$magick_cmd" "$tmpdir/master.png" \
    -define icon:auto-resize=256,128,64,48,32,16 "$desk_dir/icon.ico"
  "$magick_cmd" "$tmpdir/master.png" "$desk_dir/icon.icns"
  printf 'desktop icons -> %s\n' "${desk_dir#"$project_root"/}"
}

case "$target" in
  web) gen_web ;;
  desktop) gen_desktop ;;
  all) gen_web; gen_desktop ;;
  *)
    printf 'unknown target %s (expected web, desktop or all)\n' "$target" >&2
    exit 1
    ;;
esac

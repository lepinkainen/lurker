#!/bin/sh
#
# Install (or remove) the Tauri desktop shell for the current user.
#
# Everything lands under $HOME, so this needs no root and, on an immutable
# distro like Bazzite or Silverblue, no rpm-ostree layering and no reboot.
#
# Usage: install-linux.sh [--uninstall]

set -eu

app=lurker-desktop

project_root=$(unset CDPATH; cd -- "$(dirname -- "$0")/.." && pwd)
bin_src="$project_root/desktop/target/release/$app"
icon_src="$project_root/desktop/icons"
svg_src="$project_root/web/public/favicon.svg"

data_home=${XDG_DATA_HOME:-$HOME/.local/share}
bin_dir=$HOME/.local/bin
lib_dir=$HOME/.local/lib/lurker
icon_dir=$data_home/icons/hicolor
desktop_dir=$data_home/applications
desktop_file=$desktop_dir/$app.desktop

# hicolor size <- the generated file that matches it. desktop/icons is produced
# from favicon.svg by `task icons-desktop`, so installing those rather than
# re-rendering keeps this in step with the source automatically.
icon_map='32:32x32.png 64:64x64.png 128:128x128.png 256:128x128@2x.png 512:icon.png'

refresh_caches() {
  # Both are best-effort: the entry works without them, they just make it show
  # up without a logout.
  if command -v update-desktop-database >/dev/null 2>&1; then
    update-desktop-database "$desktop_dir" 2>/dev/null || true
  fi
  if command -v gtk-update-icon-cache >/dev/null 2>&1; then
    gtk-update-icon-cache -f -t "$icon_dir" 2>/dev/null || true
  fi
}

uninstall() {
  rm -f "$bin_dir/$app" "$desktop_file"
  rm -rf "$lib_dir"
  for pair in $icon_map; do
    size=${pair%%:*}
    rm -f "$icon_dir/${size}x${size}/apps/$app.png"
  done
  rm -f "$icon_dir/scalable/apps/$app.svg"
  refresh_caches
  printf 'Removed %s from %s\n' "$app" "$HOME/.local"
}

install_app() {
  if [ ! -x "$bin_src" ]; then
    printf 'No binary at %s -- run "task install-linux", which builds it first.\n' "$bin_src" >&2
    exit 1
  fi

  mkdir -p "$lib_dir" "$bin_dir" "$desktop_dir"
  install -m755 "$bin_src" "$lib_dir/$app"

  # A wrapper rather than a bare symlink: WebKitGTK's DMA-BUF renderer commits a
  # buffer with no acquire point under Plasma Wayland + NVIDIA, KWin kills the
  # client for the protocol violation, and the app exits on launch. Setting the
  # variable here means it works from a terminal as well as the launcher, which
  # putting it in the .desktop Exec line alone would not. See
  # ai-docs/desktop.md. An existing value is respected so the workaround can be
  # switched off once WebKitGTK no longer needs it.
  cat > "$bin_dir/$app" <<EOF
#!/bin/sh
WEBKIT_DISABLE_DMABUF_RENDERER="\${WEBKIT_DISABLE_DMABUF_RENDERER:-1}"
export WEBKIT_DISABLE_DMABUF_RENDERER
exec "$lib_dir/$app" "\$@"
EOF
  chmod 755 "$bin_dir/$app"

  for pair in $icon_map; do
    size=${pair%%:*}
    file=${pair#*:}
    mkdir -p "$icon_dir/${size}x${size}/apps"
    cp "$icon_src/$file" "$icon_dir/${size}x${size}/apps/$app.png"
  done
  mkdir -p "$icon_dir/scalable/apps"
  cp "$svg_src" "$icon_dir/scalable/apps/$app.svg"

  # StartupWMClass must match the window's app id or the taskbar shows a generic
  # icon beside the launcher's correct one. `lurker-desktop` is what Tauri puts
  # in its own generated entry for the deb/rpm bundles.
  cat > "$desktop_file" <<EOF
[Desktop Entry]
Type=Application
Name=Lurker
GenericName=IRC Client
Comment=IRC bouncer client (Tauri desktop shell)
Exec=$app
Icon=$app
Terminal=false
Categories=Network;InstantMessaging;IRCClient;
Keywords=irc;chat;bouncer;lurker;
StartupWMClass=$app
StartupNotify=true
EOF

  refresh_caches

  printf 'Installed %s\n' "$app"
  printf '  binary   %s\n' "$lib_dir/$app"
  printf '  launcher %s\n' "$bin_dir/$app"
  printf '  entry    %s\n' "$desktop_file"
  case ":$PATH:" in
    *":$bin_dir:"*) ;;
    *) printf '\nNote: %s is not on PATH, so %s will not run from a shell.\n' "$bin_dir" "$app" ;;
  esac
  printf '\nPin it from the launcher: find Lurker, right-click, Pin to Task Manager.\n'
}

case "${1:-install}" in
  --uninstall|uninstall) uninstall ;;
  install) install_app ;;
  *)
    printf 'unknown argument %s (expected --uninstall)\n' "$1" >&2
    exit 1
    ;;
esac

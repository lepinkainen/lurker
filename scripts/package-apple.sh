#!/bin/sh

set -eu

project_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$project_root"

: "${LURKER_DEVELOPER_ID:?Set LURKER_DEVELOPER_ID to a Developer ID Application identity}"
: "${LURKER_NOTARY_PROFILE:?Set LURKER_NOTARY_PROFILE to an xcrun notarytool keychain profile}"

version=${LURKER_VERSION:-dev}
derived_data="$project_root/build/DerivedData-release"
app_path="$derived_data/Build/Products/Release/Lurker.app"
dmg_path="$project_root/build/Lurker-$version.dmg"
staging=$(mktemp -d "${TMPDIR:-/tmp}/lurker-dmg.XXXXXX")

cleanup() {
  case "$staging" in
    /private/tmp/* | /tmp/* | /var/folders/*) /bin/rm -rf "$staging" ;;
    *) printf 'Refusing to remove unexpected temporary path: %s\n' "$staging" >&2 ;;
  esac
}
trap cleanup EXIT HUP INT TERM

mkdir -p "$project_root/build"

xcodebuild -quiet \
  -project apple/Lurker.xcodeproj \
  -scheme Lurker \
  -configuration Release \
  -destination 'platform=macOS,arch=arm64' \
  -derivedDataPath "$derived_data" \
  CODE_SIGNING_ALLOWED=NO \
  ONLY_ACTIVE_ARCH=YES \
  build

codesign \
  --force \
  --options runtime \
  --timestamp \
  --entitlements apple/Lurker/Lurker.entitlements \
  --sign "$LURKER_DEVELOPER_ID" \
  "$app_path"
codesign --verify --deep --strict --verbose=2 "$app_path"

ditto "$app_path" "$staging/Lurker.app"
ln -s /Applications "$staging/Applications"
/bin/rm -f "$dmg_path"
hdiutil create \
  -volname "Lurker" \
  -srcfolder "$staging" \
  -ov \
  -format UDZO \
  "$dmg_path"

codesign --force --timestamp --sign "$LURKER_DEVELOPER_ID" "$dmg_path"
xcrun notarytool submit "$dmg_path" --keychain-profile "$LURKER_NOTARY_PROFILE" --wait
xcrun stapler staple "$dmg_path"
xcrun stapler validate "$dmg_path"

printf 'Created signed and notarized installer: %s\n' "$dmg_path"

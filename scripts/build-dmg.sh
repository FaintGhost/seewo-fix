#!/bin/sh
# Called by `mise run dmg` after its app dependency has completed.
set -eu
cd "$(dirname "$0")/.."
if [ "$(uname -s)" != Darwin ]; then
    echo 'DMG packaging requires macOS.' >&2
    exit 1
fi
arch=$(go env GOARCH)
app="seewo-fix/build/darwin-$arch/希沃偏移修复.app"
version=$(/usr/libexec/PlistBuddy -c 'Print CFBundleShortVersionString' "$app/Contents/Info.plist")
output="seewo-fix/build/希沃偏移修复-$version-$arch.dmg"
dmg_stage=$(mktemp -d /tmp/seewo-dmg.XXXXXX)
trap 'rm -rf "$dmg_stage"' EXIT HUP INT TERM
ditto "$app" "$dmg_stage/希沃偏移修复.app"
ln -s /Applications "$dmg_stage/Applications"
hdiutil create -volname '希沃偏移修复' -srcfolder "$dmg_stage" -format UDZO -fs HFS+ -ov "$output"
hdiutil verify "$output"
printf '\nDMG: %s\n' "$output"

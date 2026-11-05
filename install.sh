#!/bin/sh
set -eu
umask 077

release_url=https://github.com/windzu/gorex/releases
version=
update_from=

fail() {
    printf 'gorex install: %s\n' "$*" >&2
    exit 1
}

while [ "$#" -gt 0 ]; do
    case $1 in
        --version)
            [ "$#" -ge 2 ] || fail '--version requires a release tag'
            version=$2
            shift 2
            ;;
        --update-from)
            [ "$#" -ge 2 ] || fail 'missing executable path'
            update_from=$2
            shift 2
            ;;
        -h|--help)
            printf '%s\n' 'Usage: sh install.sh [--version vX.Y.Z]'
            exit 0
            ;;
        *) fail "unknown argument: $1" ;;
    esac
done

os=$(uname -s)
case $os:$(uname -m) in
    Darwin:arm64)
        asset_suffix=arm64.dmg
        executable=GoRex.app/Contents/MacOS/GoRex
        ;;
    Linux:x86_64)
        asset_suffix=linux-amd64.tar.gz
        executable=gorex
        ;;
    *) fail "unsupported platform: $os $(uname -m)" ;;
esac

root="${XDG_DATA_HOME:-$HOME/.local/share}/gorex"
# Normalize the parent so links still match when XDG_DATA_HOME is a symlink.
mkdir -p "$root" "$HOME/.local/bin"
root=$(cd "$root" && pwd -P)
command_link="$HOME/.local/bin/gorex"
app_link="$HOME/Applications/GoRex.app"
current="$root/current"
mkdir "$root/.install-lock" 2>/dev/null ||
    fail "another install is in progress (lock: $root/.install-lock)"

work=
mounted=
added_command=
added_app=
cleanup() {
    status=$?
    if [ "$status" -ne 0 ]; then
        if [ -n "$added_command" ]; then rm -f "$command_link"; fi
        if [ -n "$added_app" ]; then rm -f "$app_link"; fi
    fi
    if [ -n "$mounted" ]; then
        hdiutil detach "$mounted" >/dev/null 2>&1 || work=
    fi
    if [ -n "$work" ]; then rm -rf "$work"; fi
    rmdir "$root/.install-lock"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM

managed_link() {
    [ ! -e "$1" ] && [ ! -L "$1" ] || {
        [ -L "$1" ] && [ "$(readlink "$1")" = "$2" ]
    } || fail "refusing to overwrite $1"
}

managed_link "$command_link" "$current/$executable"
if [ "$os" = Darwin ]; then
    managed_link "$app_link" "$current/GoRex.app"
fi
if [ -e "$current" ] || [ -L "$current" ]; then
    [ -L "$current" ] || fail "refusing to overwrite $current"
    case $(readlink "$current") in
        versions/v*) ;;
        *) fail "refusing to overwrite $current" ;;
    esac
fi
if [ -n "$update_from" ]; then
    [ -L "$current" ] && [ -L "$command_link" ] &&
        [ "$update_from" = "$root/$(readlink "$current")/$executable" ] ||
        fail 'gorex update requires the current CLI-managed installation. Install with install.sh first.'
fi

download() {
    curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' \
        --connect-timeout 15 --max-time 300 --retry 2 "$@"
}

if [ -z "$version" ]; then
    latest=$(download -o /dev/null -w '%{url_effective}' "$release_url/latest") ||
        fail 'could not find the latest stable release. Pass --version to select a preview.'
    case $latest in
        "$release_url/tag/"*) version=${latest#"$release_url/tag/"} ;;
        *) fail 'no stable release is published. Pass --version to select a preview.' ;;
    esac
fi
case $version in
    *[!a-zA-Z0-9.-]*) fail "invalid release tag: $version" ;;
esac
printf '%s\n' "$version" | LC_ALL=C grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9]+([.-][a-zA-Z0-9]+)*)?$' ||
    fail "invalid release tag: $version"
if [ -L "$current" ]; then
    installed=$(readlink "$current")
    if [ -n "$update_from" ] && [ "$installed" = "versions/$version" ]; then
        printf 'GoRex %s is already installed.\n' "$version"
        exit 0
    fi
    [ -n "$update_from" ] || [ "$installed" = "versions/$version" ] ||
        fail 'GoRex is already installed. Use gorex update to change versions.'
fi
number=${version#v}
asset="gorex-${number%%-*}-$asset_suffix"
destination="$root/versions/$version"

if [ ! -e "$destination" ]; then
    work=$(mktemp -d "$root/.install.XXXXXX")
    download -o "$work/SHA256SUMS" "$release_url/download/$version/SHA256SUMS"
    expected=$(awk -v asset="$asset" '$2 == asset { print $1 }' "$work/SHA256SUMS")
    [ "${#expected}" -eq 64 ] && printf '%s\n' "$expected" | LC_ALL=C grep -Eq '^[0-9a-f]{64}$' ||
        fail "release $version has no SHA256 for $asset"
    download -o "$work/$asset" "$release_url/download/$version/$asset"
    if [ "$os" = Darwin ]; then
        actual=$(shasum -a 256 "$work/$asset")
    else
        actual=$(sha256sum "$work/$asset")
    fi
    [ "${actual%% *}" = "$expected" ] || fail "SHA256 mismatch for $asset"

    mkdir "$work/payload"
    if [ "$os" = Darwin ]; then
        mkdir "$work/mount"
        hdiutil attach -readonly -nobrowse -mountpoint "$work/mount" "$work/$asset" >/dev/null
        mounted="$work/mount"
        ditto "$mounted/GoRex.app" "$work/payload/GoRex.app"
        hdiutil detach "$mounted" >/dev/null
        mounted=
    else
        tar -xzf "$work/$asset" -C "$work/payload" gorex libghostty-vt.so
    fi
    [ -x "$work/payload/$executable" ] || fail "missing executable in $asset"
    mkdir -p "$root/versions"
    mv "$work/payload" "$destination"
fi
[ -x "$destination/$executable" ] || fail "incomplete install at $destination"

if [ ! -L "$command_link" ]; then
    ln -s "$current/$executable" "$command_link"
    added_command=yes
fi
if [ "$os" = Darwin ] && [ ! -L "$app_link" ]; then
    mkdir -p "$HOME/Applications"
    ln -s "$current/GoRex.app" "$app_link"
    added_app=yes
fi

if [ -z "$work" ]; then work=$(mktemp -d "$root/.install.XXXXXX"); fi
ln -s "versions/$version" "$work/current"
if [ -n "$update_from" ]; then
    "$destination/$executable" server upgrade --if-running ||
        fail 'the current installation is still selected. Resolve the server error and retry.'
fi
# Replace the pointer itself, even when it points to a directory.
if [ "$os" = Darwin ]; then
    mv -fh "$work/current" "$current"
else
    mv -fT "$work/current" "$current"
fi
added_app=
added_command=
printf 'GoRex %s installed at %s\n' "$version" "$command_link"
case ":$PATH:" in
    *":$HOME/.local/bin:"*) ;;
    *) printf 'Add %s to PATH to run gorex from any terminal.\n' "$HOME/.local/bin" ;;
esac

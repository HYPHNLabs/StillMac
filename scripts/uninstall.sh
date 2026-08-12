#!/bin/sh
set -eu
. "$(CDPATH= cd -- "$(dirname "$0")" && pwd)/lib.sh"
fail(){ printf '%s\n' "StillMac uninstaller: $*" >&2; exit 1; }
[ "$#" -eq 0 ] || fail 'unknown option'
case "$HOME" in /*) ;; *) fail 'HOME must be absolute';; esac
uid=$(id -u); secure_component "$HOME" "$uid" || fail 'HOME is unsafe'
local="$HOME/.local"; bin="$local/bin"
secure_component "$local" "$uid" || { [ ! -e "$local" ] && exit 0 || fail 'destination path component is unsafe'; }
secure_component "$bin" "$uid" || { [ ! -e "$bin" ] && exit 0 || fail 'destination path component is unsafe'; }
binpath="$bin/stillmac"; [ ! -L "$binpath" ] || fail 'refusing symlink binary'; [ ! -e "$binpath" ] || [ -f "$binpath" ] || fail 'binary is not a regular file'
home_id=$(path_identity "$HOME"); local_id=$(path_identity "$local"); bin_id=$(path_identity "$bin")
[ "$(path_identity "$HOME")" = "$home_id" ] && [ "$(path_identity "$local")" = "$local_id" ] && [ "$(path_identity "$bin")" = "$bin_id" ] || fail 'destination path changed during uninstall'
[ ! -e "$binpath" ] || rm -f -- "$binpath" || fail 'could not remove binary'
printf '%s\n' 'binary removed; data retained'

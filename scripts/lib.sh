#!/bin/sh
# Shared POSIX validation for public distribution scripts.
validate_version() {
    v=$1
    case "$v" in v*) ;; *) return 1 ;; esac
    rest=${v#v}
    oldifs=$IFS; IFS=.; set -- $rest; IFS=$oldifs
    [ "$#" -eq 3 ] || return 1
    for part in "$@"; do
        [ -n "$part" ] || return 1
        case "$part" in *[!0-9]*) return 1 ;; esac
        case "$part" in 0|[1-9]*) ;; *) return 1 ;; esac
    done
}
absolute_real_dir() {
    [ -d "$1" ] && [ ! -L "$1" ]
}
# Darwin is the supported runtime. The fallback keeps fixture tests runnable on
# non-Darwin hosts; production validation always uses the Darwin format.
path_identity() {
    if [ "$(uname -s)" = Darwin ]; then
        stat -f '%d:%i:%u:%Lp' -- "$1"
    else
        stat -c '%d:%i:%u:%a' -- "$1"
    fi
}
secure_component() {
    p=$1 expected_uid=$2
    [ -d "$p" ] && [ ! -L "$p" ] || return 1
    identity=$(path_identity "$p") || return 1
    oldifs=$IFS; IFS=:; set -- $identity; IFS=$oldifs
    [ "$#" -eq 4 ] || return 1
    [ "$3" = "$expected_uid" ] || return 1
    mode=$4
    case "$mode" in *[!0-9]*|'') return 1 ;; esac
    [ "$((mode % 100))" -eq 0 ] || return 1
}

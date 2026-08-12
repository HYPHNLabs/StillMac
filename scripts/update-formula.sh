#!/bin/sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
. "$ROOT/scripts/lib.sh"
VERSION=${1:?usage: update-formula.sh VERSION DIST [OUTPUT]}; DIST=${2:?usage: update-formula.sh VERSION DIST [OUTPUT]}; OUTPUT=${3:-$ROOT/Formula/stillmac.rb}
validate_version "$VERSION" || { echo 'invalid semantic version' >&2; exit 2; }
[ -d "$DIST" ] && [ ! -L "$DIST" ] || { echo 'distribution is not a real directory' >&2; exit 1; }
manifest="$DIST/SHA256SUMS"; [ -f "$manifest" ] || { echo 'manifest missing' >&2; exit 1; }
arm_name="stillmac-$VERSION-darwin-arm64.tar.gz"; amd_name="stillmac-$VERSION-darwin-amd64.tar.gz"
values=$(awk -v a="$arm_name" -v b="$amd_name" 'BEGIN{bad=0} {if(NF!=2||$1!~/^[0-9A-Fa-f][0-9A-Fa-f]*$/||length($1)!=64){bad=1}; if($2==a){ca++;ha=$1};if($2==b){cb++;hb=$1};if($2!=a&&$2!=b){bad=1};n++} END{if(bad||n!=2||ca!=1||cb!=1)exit 1;print ha,hb}' "$manifest") || { echo 'manifest must contain exactly two valid entries' >&2; exit 1; }
arm=$(printf '%s\n' "$values" | awk '{print $1}'); amd=$(printf '%s\n' "$values" | awk '{print $2}')
for pair in "$arm_name:$arm" "$amd_name:$amd"; do name=${pair%%:*}; hash=${pair#*:}; [ -f "$DIST/$name" ] || { echo 'artifact missing' >&2; exit 1; }; actual=$(shasum -a 256 "$DIST/$name"|awk '{print $1}'); [ "$actual" = "$hash" ] || { echo 'artifact checksum mismatch' >&2; exit 1; }; done
[ ! -e "$OUTPUT" ] || [ -f "$OUTPUT" ] && [ ! -L "$OUTPUT" ] || { echo 'output is unsafe' >&2; exit 1; }
tmp=$(mktemp "${OUTPUT}.XXXXXXXX")
trap 'rm -f "$tmp"' EXIT
sed -e "s/@VERSION@/${VERSION#v}/g" -e "s/@ARM64_SHA256@/$arm/g" -e "s/@AMD64_SHA256@/$amd/g" "$ROOT/Formula/stillmac.rb.tmpl" > "$tmp"
mv "$tmp" "$OUTPUT"
trap - EXIT

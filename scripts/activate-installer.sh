#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
. "$ROOT/scripts/lib.sh"

VERSION=${1:?usage: activate-installer.sh VERSION DIST [OUTPUT]}
DIST=${2:?usage: activate-installer.sh VERSION DIST [OUTPUT]}
validate_version "$VERSION" || { echo 'version must be canonical vMAJOR.MINOR.PATCH' >&2; exit 2; }

[ -d "$DIST" ] && [ ! -L "$DIST" ] || { echo 'distribution directory must be a real directory' >&2; exit 2; }
DIST=$(CDPATH= cd -- "$DIST" && pwd)
OUTPUT=${3:-$DIST/stillmac-install-$VERSION.sh}
case "$OUTPUT" in /*) ;; *) echo 'output must be an absolute path' >&2; exit 2;; esac
parent=$(dirname "$OUTPUT")
basename=$(basename "$OUTPUT")

arm="stillmac-$VERSION-darwin-arm64.tar.gz"
manifest="$DIST/SHA256SUMS"
[ -f "$manifest" ] && [ ! -L "$manifest" ] || { echo 'manifest is missing or unsafe' >&2; exit 1; }
for name in "$arm"; do
  [ -f "$DIST/$name" ] && [ ! -L "$DIST/$name" ] || { echo "artifact is missing or unsafe: $name" >&2; exit 1; }
done

expected_arm=$(awk -v a="$arm" 'BEGIN{n=0;seen=0;bad=0} NF!=2{bad=1;next} {h=$1;f=$2;if(length(h)!=64 || h !~ /^[0-9A-Fa-f][0-9A-Fa-f]*$/ || f !~ /^\*?[A-Za-z0-9._-]+$/)bad=1;n++;if(f==a){seen++;ha=h} else bad=1} END{if(bad||n!=1||seen!=1)exit 1;print ha}' "$manifest") || { echo 'manifest must contain exactly the Apple Silicon release artifact' >&2; exit 1; }
[ "$(shasum -a 256 "$DIST/$arm" | awk '{print $1}')" = "$expected_arm" ] || { echo "checksum mismatch: $arm" >&2; exit 1; }
manifest_digest=$(shasum -a 256 "$manifest" | awk '{print $1}')
[ "$(grep -o '@TRUSTED_MANIFEST_SHA256@' "$ROOT/scripts/install.sh.tmpl" | wc -l | tr -d ' ')" = 1 ] || { echo 'installer template must contain exactly one trust placeholder' >&2; exit 1; }

python3 "$ROOT/scripts/publish-installer.py" "$ROOT/scripts/install.sh.tmpl" "$parent" "$basename" "$manifest_digest" || exit 1
printf 'activated installer: %s\n' "$OUTPUT"

#!/bin/sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
. "$ROOT/scripts/lib.sh"
VERSION=${1:?usage: package.sh VERSION [OUT]}; OUT=${2:-$ROOT/dist}
validate_version "$VERSION" || { echo 'version must be canonical vMAJOR.MINOR.PATCH' >&2; exit 2; }
case "$OUT" in /*) ;; *) echo 'output directory must be absolute' >&2; exit 2;; esac
case "$OUT" in /|/tmp|/var|/Users|/Users/*/Library) echo 'refusing unsafe output directory' >&2; exit 2;; esac
if [ -e "$OUT" ] || [ -L "$OUT" ]; then absolute_real_dir "$OUT" || { echo 'output is not a real directory' >&2; exit 2; }; else mkdir -p "$OUT"; fi
WORK=$(mktemp -d "${TMPDIR:-/tmp}/stillmac-package.XXXXXXXX") || exit 1
case "$WORK" in /*) ;; *) exit 1;; esac
cleanup(){ rm -rf -- "$WORK"; }
trap cleanup EXIT INT TERM
for arch in arm64 amd64; do
  bin="$WORK/stillmac-$arch"
  (cd "$ROOT" && GOOS=darwin GOARCH=$arch CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags='-s -w' -o "$bin" ./cmd/stillmac)
  chmod 755 "$bin"
  archive="$OUT/stillmac-$VERSION-darwin-$arch.tar.gz"
  # Python is a build-time tool: normalize every tar field, including uid/gid/time.
  python3 - "$bin" "$archive" <<'PY'
import gzip, os, sys, tarfile
src, dst = sys.argv[1:]
with open(dst, 'wb') as raw:
  with gzip.GzipFile(fileobj=raw, mode='wb', filename='', mtime=0) as gz:
    with tarfile.open(fileobj=gz, mode='w', format=tarfile.USTAR_FORMAT) as t:
      i=tarfile.TarInfo('stillmac'); i.size=os.path.getsize(src); i.mode=0o755; i.uid=i.gid=0; i.uname=i.gname=''; i.mtime=0
      with open(src,'rb') as f: t.addfile(i,f)
PY
done
(cd "$OUT" && shasum -a 256 "stillmac-$VERSION-darwin-arm64.tar.gz" "stillmac-$VERSION-darwin-amd64.tar.gz" > SHA256SUMS)

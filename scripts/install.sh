#!/bin/sh
set -eu
printf '%s\n' 'StillMac installer: fail-closed source candidate is not release-activated; use a release-generated installer.' >&2
exit 1

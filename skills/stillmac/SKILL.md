---
name: stillmac
description: Use StillMac for explicit, local, read-only macOS baseline observations.
metadata:
  hermes:
    tags: [macos, diagnostics, privacy]
---

# StillMac

## When to use
Use the installed `stillmac` CLI when the user explicitly wants a local, read-only macOS process/memory baseline. Use the repository's `scripts/install.sh` only after inspect-first review.

## Do not use
Do not use for scheduling, remediation, process termination, cache/port inspection, networking, telemetry, causation claims, or automatic collection. Never invent collector logic or host-specific absolute paths.

## Prerequisites and consent
StillMac v0.1 targets macOS 14+ and requires a supported release binary or Go 1.23+ source build. Before `sample`, explain the allowlisted fields and obtain explicit consent for each collection run. Collection writes only the selected local data directory.

## Install (inspect first)
Review `docs/DISTRIBUTION-CONTRACT.md`, `INSTALL.md`, and the fail-closed `scripts/install.sh`. The remote release, npx skill activation, and Homebrew tap are unavailable until a versioned release and tap exist. When activated, use the release-generated installer: it verifies its embedded trusted manifest digest before validating the matching archive checksum and contents, runs staged `doctor`, then atomically installs without sudo.

## Doctor and use
```sh
stillmac doctor --data-dir "$DIR"
# after explicit consent:
stillmac sample --data-dir "$DIR"
stillmac status --data-dir "$DIR"
stillmac report --format markdown --data-dir "$DIR"
stillmac report --format json --data-dir "$DIR"
```

## Update
Inspect the new release manifest and run the same installer. Updates are checksum-verified, doctor-gated, and preserve the prior binary if staging fails.

## Uninstall
Run `scripts/uninstall.sh` or the documented installed equivalent. Uninstall removes only the regular binary and keeps data. Beta never automates recursive data purge because safe descriptor-relative deletion is not implemented; inspect the exact path first and manually remove only reviewed data, refusing symlinks and non-regular objects.

## Safety and verification
Verify paths, checksums, archive members, exit status, and that real user data and scheduler state were untouched. StillMac is read-only/no-network/no-scheduler. Seven-day status gates are elapsed span, distinct UTC dates, and 84 intervals; they are not an approval or causation claim.

<p align="center">
  <img src="assets/brand/stillmac-logo-matte-dark.png" alt="StillMac" width="760">
</p>

# StillMac

StillMac learns what is normal on your Mac so future cleanup decisions can be evidence-led, explicit, and safe.

> **Current beta:** a local, deterministic, read-only process and memory baseline. It runs locally, sends no telemetry, uses no LLM, and cannot terminate processes, clean files, or change system configuration.

## How StillMac works

```text
+============================================================================+
|                       CURRENT — READ-ONLY                                  |
+============================================================================+
|  +-------------+    +-------------+    +----------------+                  |
|  | Mac signals | -> |   Explicit  | -> | Private history|                  |
|  | process +   |    |    sample   |    | validated +    |                  |
|  | memory      |    | no scheduler|    | bounded locally|                  |
|  +-------------+    +-------------+    +--------+-------+                  |
|                                                  |                         |
|                                      +-----------+-----------+             |
|                                      v                       v             |
|                              +---------------+       +---------------+     |
|                              |    Status     |       |    Report     |     |
|                              | coverage view |       | evidence view |     |
|                              +---------------+       +---------------+     |
+============================================================================+
|          SAFETY GATE — CLEANUP IS NOT ENABLED IN THE CURRENT BETA          |
+============================================================================+
|                    COMING SOON — APPROVAL-GATED                            |
+============================================================================+
| +----------+  +-------------+  +-----------+  +---------+  +-------------+ |
| |  Learn   |  |  Classify   |  | Explain + |  |  User   |  |  Protect +  | |
| | patterns |->| candidates  |->|  dry run  |->| approves|->|    clean    | |
| +----------+  +------+------+  +-----------+  +---------+  +------+------+ |
|                   |                                              |         |
|           caches · stale worktrees                    quarantine · rollback|
|           other reviewed files                         verify · action log |
+============================================================================+
```

StillMac's objective is not blind cleanup. It is to learn recurring patterns first, distinguish active resources from credible stale candidates, and make any future deletion inspectable and user-approved. Cache inspection, worktree classification, recommendations, quarantine, and deletion are **not implemented in the current beta**.

## Release state

This repository is a **pre-release beta candidate**. The implemented surface is intentionally narrow:

- `doctor` validates the host, native probes, and local data directory;
- `sample` records one validated process-and-memory observation;
- `status` reports progress toward a seven-day coverage threshold;
- `report` renders the latest validated sample as JSON or Markdown.

StillMac has no scheduler, network access, telemetry, cache inspection, port inspection, recommendations, or active remediation. The repository now contains inspect-first installer/update/uninstaller scripts and an Agent Skill; remote release, npx activation, and Homebrew installation remain unavailable until separately activated by a real release workflow. A short release-candidate soak does not count as a completed seven-day observation period.

## One-line installation routes

These are the intended copy-and-paste routes, but **neither is active yet** because no GitHub Release, Homebrew tap, or public Agent Skill source exists. Do not run them until the first release is activated and this notice is removed.

**Homebrew — StillMac CLI**

```bash
brew install HYPHNLabs/tap/stillmac
```

**Agent Skill — cross-agent integration**

```bash
npx skills add HYPHNLabs/StillMac -g
```

The Homebrew command installs the CLI. The `npx skills add` command installs the thin Agent Skill that invokes the same CLI; it is not a second implementation of StillMac.

## Quick start

After installing or [building from source](#build-from-source), run:

```bash
stillmac doctor
stillmac sample
stillmac status
stillmac report --format markdown
```

`sample` records one explicit observation. Run it again only when you intentionally want another observation; StillMac does not schedule collection automatically. There is no `stillmac learn` command—the bounded history created by repeated `sample` calls is the learning record used by `status`.

## Command reference

| Command | Purpose |
|---|---|
| `stillmac doctor` | Validate macOS probes and the selected local data directory. |
| `stillmac sample` | Collect and store one explicit process-and-memory observation. |
| `stillmac status` | Report progress toward the seven-day coverage threshold. |
| `stillmac report` | Render the latest observation as Markdown (the default). |
| `stillmac report --format json` | Render the latest observation as JSON. |
| `stillmac report --format markdown` | Render the latest observation as Markdown explicitly. |
| `stillmac help` | Show CLI usage. |

Every operational command accepts `--data-dir PATH` or `--data-dir=PATH`. Without it, StillMac uses `$HOME/Library/Application Support/StillMac`.

## Compatibility

- **Executed locally:** Apple Silicon, macOS 26.5
- **Target:** macOS 14 or later
- **Cross-build target:** Darwin `arm64` and `amd64`
- **Not yet claimed:** runtime compatibility on Intel Macs or macOS 14

Compatibility claims will expand only after execution on those environments.

## Build from source

Requirements: macOS and Go 1.23 or later.

```bash
mkdir -p ./bin
go build -trimpath -o ./bin/stillmac ./cmd/stillmac
```

No public binary release exists yet. The local distribution package and installer template are verified, but remote GitHub Release assets do not exist; do not claim `brew install` or npx activation works until those assets are published. `scripts/install.sh` intentionally fails closed in this source candidate. After a release exists and provenance is reviewed, use the release-generated installer with its embedded trusted manifest digest; it verifies that digest before archive checks, installs per-user without sudo, and runs doctor only. See [INSTALL.md](INSTALL.md) and [UNINSTALL.md](UNINSTALL.md).

## Use an isolated data directory

```bash
STILLMAC_DATA_DIR="$(mktemp -d)/StillMac"

./bin/stillmac doctor --data-dir "$STILLMAC_DATA_DIR"
./bin/stillmac sample --data-dir "$STILLMAC_DATA_DIR"
./bin/stillmac status --data-dir "$STILLMAC_DATA_DIR"
./bin/stillmac report --format json --data-dir "$STILLMAC_DATA_DIR"
./bin/stillmac report --format markdown --data-dir "$STILLMAC_DATA_DIR"
```

Without `--data-dir`, StillMac uses:

```text
$HOME/Library/Application Support/StillMac
```

`sample` requires access to macOS `/bin/ps` and `/usr/sbin/sysctl`. It stores only the allowlisted fields documented in [PRIVACY.md](PRIVACY.md).

## What `status` means

Coverage eligibility requires all three gates:

- at least seven days of elapsed observation span;
- observations on at least seven distinct UTC dates;
- at least 84 distinct 30-minute coverage intervals.

`coverage_ready` means only that these coverage gates passed. It does not establish causation, enable recommendations, or authorise actions.

## Data and removal

StillMac writes only its selected data directory. Directories use mode `0700` and state/history files use mode `0600` where practical. History is bounded by count, age, per-file size, and total size.

`scripts/uninstall.sh` removes the regular installed binary and keeps data. It never recursively deletes data: safe descriptor-relative deletion is not implemented in beta. To remove observations, inspect the exact data path first, refuse symlinks/non-regular objects, and manually delete only the reviewed path. StillMac never deletes unrelated files.

See [docs/DATA-LOCATIONS.md](docs/DATA-LOCATIONS.md) and [PRIVACY.md](PRIVACY.md).

## Development and verification

```bash
gofmt -w .
go test -count=1 -race ./...
go vet ./...
go build -trimpath -o ./bin/stillmac ./cmd/stillmac
```

The test suite includes intentionally hostile **synthetic** paths, usernames, and credential-shaped strings to prove they do not leak into state, reports, or user-visible errors. They are not real credentials or user data.

See:

- [docs/V0.1-TRACER-CONTRACT.md](docs/V0.1-TRACER-CONTRACT.md) — exact command, schema, storage, and exit-code contract
- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) — component boundaries
- [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md) — local engineering workflow
- [THREAT-MODEL.md](THREAT-MODEL.md) — security assumptions and mitigations
- [CONTRIBUTING.md](CONTRIBUTING.md) — contribution rules

## Licence

StillMac source code and documentation are licensed under the [Apache License 2.0](LICENSE). The StillMac name and brand assets are not granted for use as trademarks by that licence; Apache-2.0 permits only reasonable and customary use to describe the work's origin.

The approved matte [app icon](assets/brand/stillmac-app-icon-matte-1024.png) and [horizontal logo](assets/brand/stillmac-logo-matte-dark.png) are included for consistent StillMac identification.

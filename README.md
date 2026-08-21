<p align="center">
  <img src="assets/brand/stillmac-logo-matte-dark.png" alt="StillMac" width="760">
</p>

# StillMac

StillMac helps Mac developers record point-in-time process and memory measurements and review developer caches that may be using disk space.

It runs locally and can clean one thing in this beta: the exact Go build cache, after showing a short-lived plan and asking for explicit approval. It is not a general-purpose Mac cleaner.

## Who StillMac is for

StillMac is for Mac developers who use tools such as Go, Homebrew, Git worktrees, or Codex and want evidence before cleanup.

You can use it to:

- take an explicit process and memory snapshot;
- measure a fixed set of developer-cache roots;
- review path-minimised Git worktree inventory for a project;
- clean an eligible Go build cache after approval;
- keep a private local receipt of the measured result.

Homebrew caches, Codex runtimes, and Git worktrees remain inventory-only. StillMac never cleans them.

## Public beta limits

StillMac's first public beta release is `v0.1.1`. It supports **Apple Silicon Macs only** and has been executed on macOS 26.5. Other macOS versions are not yet verified runtime claims.

Signing and Apple notarisation are not claimed for this beta. Homebrew and Agent Skill distribution are also inactive.

## Install

### 1. Install StillMac

```bash
curl -fsSL https://github.com/HYPHNLabs/StillMac/releases/download/v0.1.1/stillmac-install-v0.1.1.sh | sh
```

The installer verifies the pinned `v0.1.1` manifest and archive, installs without `sudo`, and runs `doctor` before replacing an existing binary. It installs StillMac at `$HOME/.local/bin/stillmac`.

### 2. Add StillMac to PATH

StillMac does not edit your shell configuration. Run this once for the default macOS zsh shell:

```bash
printf '\nexport PATH="$HOME/.local/bin:$PATH"\n' >> "$HOME/.zprofile"
exec zsh -l
```

If you already manage PATH in `$HOME/.zshrc`, use that file instead. Do not add the same line to both files.

Troubleshooting fallback: if `stillmac` is not found, open a new Terminal window or run `$HOME/.local/bin/stillmac doctor` directly.

Prefer to inspect the installer first? See [INSTALL.md](INSTALL.md) for the inspect-first route, source build, and uninstall instructions.

## Quick start

Check the installation, then run a read-only scan:

```bash
stillmac doctor
stillmac scan --format text
```

`doctor` checks whether StillMac can run safely. `scan` measures and classifies the fixed cache roots. Neither command cleans anything.

To review the interactive cleanup flow:

```bash
stillmac clean all
```

`clean all` shows every candidate and exclusion, creates a 15-minute plan containing only eligible `SAFE` items, and asks you to type the exact plan approval before it acts. If the state changes or safety cannot be proved, StillMac stops. In this beta, only the exact Go build cache can be eligible.

## Command reference

| Command | What it does |
| --- | --- |
| `stillmac doctor` | Checks runtime, permissions, and local safety requirements. |
| `stillmac scan --format text` | Inventories the fixed developer-cache roots without changing them. |
| `stillmac clean all` | Guides you through review, planning, approval, and eligible cleanup. |
| `stillmac explain <candidate-id> --format text` | Explains one candidate from the latest scan. |
| `stillmac protect <candidate-id>` | Protects a candidate from future cleanup plans. |
| `stillmac plan all-safe --format text` | Creates a short-lived plan for every eligible candidate. |
| `stillmac apply <plan-id> --format json` | Revalidates and applies one explicitly approved plan. |
| `stillmac history --format text` | Shows local cleanup receipts. |
| `stillmac sample` | Records one process and memory snapshot. |
| `stillmac status` | Shows sampling coverage and data quality. |
| `stillmac report --format markdown` | Renders the latest stored snapshot. |
| `stillmac help` | Lists commands and options. |

Run `stillmac help` to print the built-in usage summary.

## Example scan

Example only. Your sizes and IDs will differ.

```text
1. sm-1111111111111111 REVIEW Homebrew download cache 734003200 bytes: exact allowlisted cache root; this release has no bounded owner-native Homebrew action
2. sm-2222222222222222 SAFE Go build cache 536870912 bytes: exact allowlisted Go cache root; owner-native Go executable and GOCACHE verified
3. sm-3333333333333333 BLOCKED_ACTIVE Codex runtime cache 0 bytes: Codex inactivity was not proven
```

- `SAFE` means the item is eligible for a plan. It is not cleaned automatically.
- `REVIEW` means StillMac can show the inventory but has no cleanup action for it.
- `BLOCKED_*` means StillMac could not prove that an action would be safe.

## What StillMac can change

The only active cleanup action is the verified owner-native command `go clean -cache` for the exact Go build cache. StillMac performs revalidation immediately before invoking it, then writes a receipt with the measured bytes before and after the action.

Cleaning the Go build cache can free disk space, but later Go builds may take longer while cache entries are rebuilt.

StillMac does not delete Homebrew caches, Codex runtimes, or Git worktrees. It does not scan arbitrary locations, terminate processes, elevate privileges, run in the background, or provide generic path deletion.

## Process and memory snapshots

`sample` saves one point-in-time process and memory snapshot. Repeated manual samples build the coverage shown by `status`; `report` renders the latest stored sample. StillMac never samples automatically.

```bash
stillmac sample
stillmac status
stillmac report --format markdown
```

Reports describe measurements observed during the same collection cycle. They do not claim that a process caused memory pressure, and one sample cannot establish a trend.

## Advanced and automated use

Human users should normally start with `clean all`. For automation or agent workflows, keep planning and approval separate and use the stable IDs from a fresh scan:

```bash
# Explain one current candidate
stillmac explain sm-0123456789abcdef --format text

# Preview every currently eligible candidate
stillmac plan all-safe --format text

# Apply only after reviewing that exact plan
stillmac apply plan-0123456789abcdef --format json

# Review local receipts
stillmac history --format text
```

Candidate and plan IDs above are shape examples only. Never invent an ID or reuse one from a different scan. Plans expire after 15 minutes.

To add path-free Git worktree inventory for one project:

```bash
stillmac scan --scope /path/to/project --format text
```

Providing a project scope adds inventory only. StillMac never performs a Git cleanup action.

## Upgrade

StillMac does not update itself. For `v0.1.1`, rerun the pinned installer command from the [Install](#install) section. The installer verifies the downloaded release and stages `doctor` before replacing the existing binary.

For a later release, use only the exact version-pinned command published on that release page. Do not change the version number in an old command and assume the assets are equivalent.

## Uninstall

Download and inspect the versioned uninstall script before running it:

```bash
curl -fsSLo /tmp/stillmac-uninstall.sh \
  https://raw.githubusercontent.com/HYPHNLabs/StillMac/v0.1.1/scripts/uninstall.sh
less /tmp/stillmac-uninstall.sh
sh /tmp/stillmac-uninstall.sh
```

The script removes only `$HOME/.local/bin/stillmac`. It retains private StillMac data so you can inspect it. The PATH entry is harmless after uninstall; you may remove the StillMac export line from the profile file you chose during setup.

## Build from source

Requirements: Apple Silicon macOS and Go 1.23 or later.

```bash
git clone https://github.com/HYPHNLabs/StillMac.git
cd StillMac
mkdir -p ./bin
go build -buildvcs=false -trimpath -o ./bin/stillmac ./cmd/stillmac
./bin/stillmac doctor
./bin/stillmac scan --format text
```

The `./bin/stillmac` path applies only to this source-build route.

## Privacy

Local means local:

- No telemetry, analytics, cloud service, network client, or LLM dependency in the Go binary.
- Process and memory samples exclude command arguments, environment variables, full executable paths, workspace names, usernames, file contents, conversations, tokens, and credentials.
- Cleanup private state retains the exact action target, host binding, protection and plan records, receipts, and verified Go executable identity required to revalidate an approved plan.
- Public output excludes the private target paths and executable identity.
- Private state uses restrictive local permissions under `$HOME/Library/Application Support/StillMac` by default.
- Sampling and cleanup occur only when explicitly invoked.

Read [PRIVACY.md](PRIVACY.md) and [SECURITY.md](SECURITY.md) for the complete boundaries.

## Detailed documentation

- [Installation and uninstall](INSTALL.md)
- [Developer cleanup contract](docs/DEVELOPER-CLEANUP-CONTRACT.md)
- [v0.1 tracer contract](docs/V0.1-TRACER-CONTRACT.md)
- [Architecture](docs/ARCHITECTURE.md)
- [Distribution contract](docs/DISTRIBUTION-CONTRACT.md)
- [Threat model](THREAT-MODEL.md)
- [Data locations](docs/DATA-LOCATIONS.md)
- [Development and verification](docs/DEVELOPMENT.md)
- [Roadmap](ROADMAP.md)

StillMac is intentionally narrow. Boring safety beats clever deletion.

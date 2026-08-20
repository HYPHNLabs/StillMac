# Install

StillMac's first public beta release is `v0.1.1`. It supports Apple Silicon Macs only and has been executed on macOS 26.5.

## Direct installer

```sh
curl -fsSL https://github.com/HYPHNLabs/StillMac/releases/download/v0.1.1/stillmac-install-v0.1.1.sh | sh
```

The installer embeds the reviewed SHA-256 digest of the exact `v0.1.1` manifest. It verifies the manifest and Apple Silicon archive, rejects unsafe archive members, stages `doctor` in temporary data, installs per-user at `$HOME/.local/bin/stillmac` without `sudo`, and preserves an existing binary if verification fails. This route is usable only while the exact versioned GitHub Release assets are available; a missing or mismatched asset fails closed.

## Inspect first

```sh
curl -fsSLo /tmp/stillmac-install-v0.1.1.sh \
  https://github.com/HYPHNLabs/StillMac/releases/download/v0.1.1/stillmac-install-v0.1.1.sh
less /tmp/stillmac-install-v0.1.1.sh
sh /tmp/stillmac-install-v0.1.1.sh
```

## Homebrew, INACTIVE

```sh
brew install HYPHNLabs/tap/stillmac
```

There is no activated tap formula. Do not use this command yet.

## Agent Skill, INACTIVE

```sh
npx skills add HYPHNLabs/StillMac -g
```

The repository contains a thin Agent Skill, but no npx-compatible distribution route has been activated. The skill invokes the same CLI and does not implement separate cleanup logic.

## Build from source

Requirements: Apple Silicon macOS and Go 1.23 or later.

```sh
git clone https://github.com/HYPHNLabs/StillMac.git
cd StillMac
mkdir -p ./bin
go build -buildvcs=false -trimpath -o ./bin/stillmac ./cmd/stillmac
./bin/stillmac help
./bin/stillmac scan --format text
```

The last command is read-only. Run `plan` or `apply` only after reviewing [docs/DEVELOPER-CLEANUP-CONTRACT.md](docs/DEVELOPER-CLEANUP-CONTRACT.md).

## Uninstall

```sh
curl -fsSLo /tmp/stillmac-uninstall.sh \
  https://raw.githubusercontent.com/HYPHNLabs/StillMac/v0.1.1/scripts/uninstall.sh
less /tmp/stillmac-uninstall.sh
sh /tmp/stillmac-uninstall.sh
```

Uninstall removes only `$HOME/.local/bin/stillmac`. Private StillMac data is retained for user-controlled inspection or manual removal.

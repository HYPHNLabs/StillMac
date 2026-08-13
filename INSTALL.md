# Install

No public installation route is active. `scripts/install.sh` intentionally exits with failure. The installer template and packaging tests are local distribution evidence, not a release.

## Direct installer, INACTIVE

After the immutable `v0.1.0` release and provenance review exist, the concise intended route is:

```sh
curl -fsSL https://github.com/HYPHNLabs/StillMac/releases/download/v0.1.0/stillmac-install-v0.1.0.sh | sh
```

The safer inspect-first route downloads the same pinned release asset before execution:

```sh
curl -fsSL -o stillmac-install-v0.1.0.sh https://github.com/HYPHNLabs/StillMac/releases/download/v0.1.0/stillmac-install-v0.1.0.sh
less stillmac-install-v0.1.0.sh
sh stillmac-install-v0.1.0.sh
```

Do not run either command yet. The concrete `v0.1.0` asset does not exist. A release-generated installer must embed the reviewed manifest digest, verify it before archive hashes, reject unsafe archive members, stage `doctor` in temporary data, install per-user without sudo, and preserve the old binary on failure.

## Homebrew, INACTIVE

```sh
brew install HYPHNLabs/tap/stillmac
```

There is no activated tap formula.

## Agent Skill, INACTIVE

```sh
npx skills add HYPHNLabs/StillMac -g
```

There is no published npx-compatible source. The skill is a thin approval interface to the same CLI, not an independent cleanup implementation.

## Build from source

The only current working route is a reviewed local source build:

```sh
mkdir -p ./bin
go build -buildvcs=false -trimpath -o ./bin/stillmac ./cmd/stillmac
./bin/stillmac help
./bin/stillmac scan --format text
```

The last command is read-only. Run `plan` or `apply` only after reviewing [docs/DEVELOPER-CLEANUP-CONTRACT.md](docs/DEVELOPER-CLEANUP-CONTRACT.md).

# Install

No public installation route is active. `scripts/install.sh` intentionally exits with failure. The installer template and packaging tests are local distribution evidence, not a release.

## Direct installer, INACTIVE

After an immutable versioned release and provenance review exist, the intended inspect-first route is:

```sh
curl --fail --location --output ./stillmac-install-vX.Y.Z.sh https://github.com/HYPHNLabs/StillMac/releases/download/vX.Y.Z/stillmac-install-vX.Y.Z.sh
less ./stillmac-install-vX.Y.Z.sh
STILLMAC_VERSION=vX.Y.Z sh ./stillmac-install-vX.Y.Z.sh
```

Do not run that placeholder now. There is no asset to trust. A release-generated installer must embed the reviewed manifest digest, verify it before archive hashes, reject unsafe archive members, stage `doctor` in temporary data, install per-user without sudo, and preserve the old binary on failure.

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
go build -trimpath -o ./bin/stillmac ./cmd/stillmac
./bin/stillmac help
./bin/stillmac scan --format text
```

The last command is read-only. Run `plan` or `apply` only after reviewing [docs/DEVELOPER-CLEANUP-CONTRACT.md](docs/DEVELOPER-CLEANUP-CONTRACT.md).

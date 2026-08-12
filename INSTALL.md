# Install

Current status: local package and scripts are testable; no remote GitHub release exists, so direct remote commands are placeholders until a versioned release is activated. Do not run `brew install` or `npx skills add` yet.

Inspect the fail-closed source candidate and the release-activation contract first:

```sh
less docs/DISTRIBUTION-CONTRACT.md scripts/install.sh
```

For a future activated release, download the **release-generated installer** whose trusted manifest digest has been embedded after provenance review. Inspect it and its pinned version before running it:

```sh
STILLMAC_VERSION=vX.Y.Z sh ./stillmac-install-vX.Y.Z.sh
```

The script supports macOS arm64 and amd64, requires `curl`, `shasum`, `tar`, `mktemp`, and standard POSIX tools, never requires Python, refuses root, validates the exact archive checksum and member set, stages `doctor` in isolated data, and atomically updates `~/.local/bin/stillmac`. Add `~/.local/bin` to `PATH` if needed. It does not mutate scheduler state or real user data.

After installation:

```sh
stillmac doctor
stillmac sample --data-dir "$(mktemp -d)/StillMac" # only after explicit collection consent
stillmac status
stillmac report --format markdown
```

Seven-day status gates are elapsed span, seven UTC dates, and 84 half-hour intervals. Installation is not a claim of macOS 14/Intel runtime evidence. StillMac is licensed under Apache-2.0.

Current status: locally verified packaging and installer template; no GitHub release exists yet. `scripts/install.sh` intentionally refuses installation. After a versioned GitHub Release and provenance review exist, download/review the release-generated pinned installer, then run it locally. It verifies the pinned manifest digest before archive checks, rejects traversal, installs without sudo, and runs doctor only. Homebrew and npx Agent Skill activation require a future published release/package and are not currently claimed to work.

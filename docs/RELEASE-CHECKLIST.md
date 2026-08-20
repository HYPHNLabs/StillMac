# Release Checklist

## Decision boundary

StillMac is approved as HYPHN Labs' first public beta. Public scope is Apple Silicon only. The superseded private `v0.1.0` rehearsal release has been returned to draft and is not a public installation route. `v0.1.1` is the approved publication candidate.

## Evidence already completed

- [x] Bounded 12-hour beta soak completed: 25/25 samples over 12 hours 1 minute 55 seconds.
- [x] Seven-day observe-only learning completed: 336/336 scheduled runs and 337 recorded samples; review completed.
- [x] Local process/memory and cleanup contracts independently reviewed during development.
- [x] Repository owner and name established as `HYPHNLabs/StillMac`.
- [x] Apache License 2.0 approved and added.
- [x] HYPHN Labs confirmed as code and brand rights holder.
- [x] Provenance reviewed: one repository contributor, no copied prototype source, Go standard library only, no third-party runtime dependencies.
- [x] Security fallback route established at `contact@hyphnlabs.com`; GitHub Private Vulnerability Reporting must be enabled immediately after public visibility permits it.
- [x] Public compatibility narrowed to Apple Silicon only; executed on macOS 26.5. Intel is not claimed or distributed.
- [x] Superseded `v0.1.0` prerelease returned to draft before visibility change.
- [x] Local installer/uninstaller fixtures, manifest pinning, unsafe archive rejection, rollback, and keep-data behavior tested.

## Exact `v0.1.1` candidate gate

- [x] Full race tests, vet, formatting, and trimpath build pass after the final source change.
- [x] Real-host `doctor`, `sample`, `status`, scan, plan preview, and report formats pass without performing cleanup.
- [x] Source, tracked history, generated state/reports, and final binary pass privacy/residue scans.
- [x] Apple Silicon archive builds twice byte-for-byte identically.
- [x] Final `SHA256SUMS`, installer, and `PROVENANCE.json` are generated from the committed `main` revision.
- [x] Fresh temporary-HOME install, repeat install, read-only smoke, and uninstall pass using the deterministic candidate assets; repeat against downloaded release assets before visibility change.
- [x] Independent final specification and security review passes against the exact diff and artifacts.
- [x] Remote CI passes on the final committed revision.
- [x] Final release assets are downloaded back and match local SHA-256 values.
- [x] README and INSTALL commands match the published `v0.1.1` assets.

## Public visibility gate

- [x] Repository visibility changed to public only after the exact candidate gate passes.
- [x] Anonymous repository, release, archive, manifest, installer, and source links return successfully.
- [x] GitHub Private Vulnerability Reporting enabled and verified.
- [x] Public branch protection and security settings verified.
- [x] Public pinned installer command succeeds under a fresh temporary HOME.

## Deliberately inactive routes

- Homebrew tap: inactive.
- Agent Skill/npx distribution: inactive.
- Intel binaries: not distributed.
- Signing/notarisation: not claimed.

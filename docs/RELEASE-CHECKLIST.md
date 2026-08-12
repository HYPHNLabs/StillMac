# Release Checklist

## Current decision boundary

This repository may be prepared and committed locally. No remote, push, tag, release, or publication is authorised by this checklist.

## Private repository gate

- [ ] Bounded 12-hour beta soak completed without failure.
- [ ] Full race tests, vet, and trimpath build passed after the final source change.
- [ ] Darwin `arm64` and `amd64` cross-builds passed.
- [ ] Real-host `doctor`, `sample`, `status`, and both report formats passed.
- [ ] Source, tracked-file list, generated state/reports, and binary passed privacy/residue scans.
- [ ] Private PRDs, local build output, and runtime state are absent from Git history.
- [ ] Independent specification review passed.
- [ ] Independent security/code-quality review passed.
- [ ] Repository owner, name, and private visibility approved.
- [ ] Commit identity and recovery controls approved.

## Public beta gate

- [ ] Legal rights holder confirmed.
- [x] Apache License 2.0 approved and added.
- [ ] Exact inspiration/third-party attribution confirmed.
- [ ] Security disclosure route is operational.
- [ ] Claimed macOS/architecture matrix executed in CI or on real hosts.
- [ ] Tagged artifacts, SHA-256 manifest, and provenance generated and verified.
- [x] Local clean-account installer/uninstaller fixtures and rollback behavior are tested; remote release activation remains pending.
- [ ] README claims match executed evidence.
- [ ] Explicit public publication approval recorded.

## Promotion gate

- [ ] Public artifact installation re-verified.
- [ ] Issue and security routes verified.
- [ ] Launch copy reviewed for unsupported performance, safety, or compatibility claims.
- [ ] Explicit promotion approval recorded.

## Current known blockers

- Apache-2.0 is selected; legal confirmation of code and brand ownership remains a public-release gate.
- No published release, Homebrew tap, or npx activation. Local installer/uninstaller scripts are present and separately verified.
- No Intel runtime evidence.
- macOS 14 is a target, not an executed support claim.
- The seven-day product observation window has not been completed by the release-candidate soak.

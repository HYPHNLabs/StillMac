# StillMac development rules

## Product boundary

StillMac v0.1 is a local, deterministic process and memory baseline plus an approval-gated developer-cache cleanup slice for macOS.

The current beta candidate contains only:

- one safe process collector;
- macOS memory-pressure and swap inputs;
- allowlisted private JSON history;
- deterministic coverage status;
- preliminary JSON and Markdown reports;
- explicit data-quality and confidence output.
- exact-root Homebrew and Go cache inventory, immutable plans, protection, and receipts;
- path-free Git worktree inventory with no Git cleanup action;
- non-executable Codex runtime inventory.

Do not broaden cleanup beyond `docs/DEVELOPER-CLEANUP-CONTRACT.md`. Do not add a scheduler, runtime installer/updater, client adapter, standalone port analysis, process termination, arbitrary deletion, cache-root rename, telemetry, cloud service, networking, or LLM dependency without a separately approved contract.

## Privacy and safety

- Collect only explicitly allowlisted fields.
- Never collect command arguments, environment variables, full executable paths, workspace paths/names, usernames, unrelated filenames, file contents, clipboard, browser data, agent conversations, tokens, or credentials.
- Report temporal association only. Never claim a process caused memory pressure.
- The only active action is approval-gated invocation of a verified owner-native Go tool as absolute `go clean -cache` for the exact Go build cache.
- Store data locally with restrictive permissions and fail closed on unsafe state.

## Engineering

- Use Go and the standard library unless a dependency is explicitly justified.
- Follow strict TDD: run a failing behavioural test before production code, then make it pass.
- Keep CLI exit codes and JSON schemas stable and documented.
- macOS 14+ is a target, not a support claim until executed there.
- Darwin `amd64` cross-build success is not Intel runtime evidence.
- Do not copy code from any internal prototype. Treat prototypes only as evidence and requirements.
- Do not add licence or copyright-holder claims until ownership is confirmed.

## Repository boundary

- Local Git commits are allowed for the clean candidate package.
- Keep private planning PRDs, runtime state, build artifacts, editor settings, and environment files out of Git.
- Do not add a remote, push, tag, release, package, or publish without explicit approval.

## Verification

Run at minimum:

```bash
gofmt -w .
go test -count=1 -race ./...
go vet ./...
go build -trimpath -o ./bin/stillmac ./cmd/stillmac
```

Inspect the tracked-file list, source, generated state/reports, and built binary for prohibited real strings. Synthetic hostile fixtures are expected and must remain clearly artificial. The binary must not contain the current workspace or home path.

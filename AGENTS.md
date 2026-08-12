# StillMac development rules

## Product boundary

StillMac v0.1 is a local, deterministic, read-only process and memory baseline for macOS.

The current beta candidate contains only:

- one safe process collector;
- macOS memory-pressure and swap inputs;
- allowlisted private JSON history;
- deterministic coverage status;
- preliminary JSON and Markdown reports;
- explicit data-quality and confidence output.

Do not add a scheduler, installer, updater, uninstaller, Agent Skill, client adapter, cache collection, standalone port analysis, process termination, cleanup, telemetry, cloud service, networking, or LLM dependency without a separately approved contract.

## Privacy and safety

- Collect only explicitly allowlisted fields.
- Never collect command arguments, environment variables, full executable paths, workspace paths/names, usernames, unrelated filenames, file contents, clipboard, browser data, agent conversations, tokens, or credentials.
- Report temporal association only. Never claim a process caused memory pressure.
- The binary must have no active-action capability in v0.1.
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

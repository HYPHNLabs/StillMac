# Contributing

StillMac is a narrow baseline and approval-gated developer-cache beta candidate. Contributions must preserve its privacy and safety boundaries.

## Before proposing a change

- Read [docs/V0.1-TRACER-CONTRACT.md](docs/V0.1-TRACER-CONTRACT.md).
- Read [docs/DEVELOPER-CLEANUP-CONTRACT.md](docs/DEVELOPER-CLEANUP-CONTRACT.md) for cleanup work.
- Keep changes inside one of those current contracts.
- Do not add network access, telemetry, scheduling, runtime installation, recursive deletion, new cleanup roots, process control, port collection, or LLM dependencies without a separately approved contract.
- Do not add licence or copyright-holder claims until ownership is formally decided.

## Engineering rules

- Use Go and the standard library unless a dependency is explicitly justified.
- Follow strict test-driven development: add and run a failing behavioural test before production code.
- Keep wall-clock, host probes, and thresholds injectable where deterministic testing requires it.
- Return stable, generic, path-free errors.
- Never persist command arguments, environments, full paths, usernames, secrets, file contents, or native command output.
- Treat every existing state file and directory entry as untrusted.

## Required verification

```bash
gofmt -w .
go test -count=1 -race ./...
go vet ./...
go build -trimpath -o ./bin/stillmac ./cmd/stillmac
```

Inspect source, generated state/reports, and the binary for private paths, credentials, and unsupported claims. Synthetic hostile fixtures are expected; real secrets and personal data are prohibited.

## Changes and review

Keep changes small and explain:

- behavioural contract affected;
- privacy/security impact;
- RED/GREEN test evidence;
- compatibility impact;
- rollback or migration implications.

Security reports must follow [SECURITY.md](SECURITY.md), not public issue discussion.

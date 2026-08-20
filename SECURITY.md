# Security policy

## Supported versions

Security fixes are provided for the latest public beta. The current supported beta is `v0.1.1` for Apple Silicon Macs.

## Reporting

Do not put a vulnerability or real private data in a public issue. Use GitHub Private Vulnerability Reporting from the repository Security tab. If that route is unavailable, email `contact@hyphnlabs.com` with the subject `StillMac security report`. Include the affected command and revision, preconditions, impact, and a synthetic reproduction.

## Runtime boundary

The binary reads fixed process and memory probes, reads exact cache metadata and Git worktree state, writes private StillMac state, and can perform one active action: approval-gated invocation of a verified absolute Go executable as `go clean -cache` for the exact allowlisted Go build cache.

It has no direct recursive-deletion or cache-root-rename primitive. It cannot execute arbitrary plan paths, force Git operations, act on Homebrew, Codex, or Git inventory, terminate processes, schedule itself, elevate privileges, send telemetry, or use a network client. The verified Go tool owns deletion inside its exact configured build cache.

Security invariants include strict path-free public schemas, immutable hash-addressed 15-minute plans, a private integrity-bound target registry, actual host binding, protection enforcement, exact root device and inode identity, complete cache and executable fingerprint revalidation, sanitized Go configuration, fixed owner-native arguments, private atomic receipts, and fail-closed history reads. These checks reduce accidental or stale changes but are not atomic with `execve` or Go pathname resolution.

StillMac has no cache filesystem mutation primitive. Homebrew, Codex, and Git actions are `none`. Successful Go cleanup reports only the non-negative byte reduction measured by the exact scanner.

See [THREAT-MODEL.md](THREAT-MODEL.md) and [docs/DEVELOPER-CLEANUP-CONTRACT.md](docs/DEVELOPER-CLEANUP-CONTRACT.md).

Same-UID malicious concurrent replacement of the Go executable or logical GOCACHE pathname is explicitly out of scope. `SAFE` means a bounded owner-native action under ordinary operation, not immunity to equivalent-user malware. Approval authorizes `go clean -cache` against the logical exact GOCACHE pathname; private paths are never exposed publicly.

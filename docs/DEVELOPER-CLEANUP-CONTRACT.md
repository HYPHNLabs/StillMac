# Developer cleanup contract

## Product boundary

Cleanup is local, deterministic, narrow, and approval-gated. `scan` discovers bounded candidates, `explain` expands one current row, `plan` snapshots selected SAFE IDs, and `apply` revalidates one owner-native Go cache action. `clean` is an interactive wrapper around the same flow. StillMac performs no direct cache filesystem mutation.

`SAFE` means that StillMac established a bounded owner-native action under ordinary operation; it does not mean immunity to equivalent-user malware. Malicious concurrent same-UID replacement of the Go executable or logical GOCACHE pathname is explicitly out of scope. Userspace checks reduce accidental or stale changes but cannot atomically bind either pathname to `execve` or Go's pathname resolution.

The complete decisions are `SAFE`, `REVIEW`, `PROTECTED`, `BLOCKED_ACTIVE`, `BLOCKED_DIRTY`, `BLOCKED_UNMERGED`, `BLOCKED_UNKNOWN`, and `BLOCKED_CHANGED`. Generic `BLOCKED` is invalid.

## Commands

```text
scan [--scope PATH] [--format text|json]
explain ID [--scope PATH] [--format text|json]
plan ID... | plan all-safe [--scope PATH] [--data-dir PATH] [--format text|json]
apply PLAN_ID [--data-dir PATH] [--format text|json]
clean [IDs...|all] [--scope PATH] [--data-dir PATH]
protect ID [--scope PATH] [--data-dir PATH]
history [--data-dir PATH] [--format text|json]
```

Options accept split or `--name=value` forms. Text scans number the current rows `1..N` while also showing each stable ID. Interactive `clean` may accept those fresh display numbers (for example `clean 1 3`) and maps them to IDs from the same scan; numbers are never persisted. `plan` accepts stable IDs or `all-safe` only; numeric selections are rejected. Unknown selections fail, and combining `all`/`all-safe` with explicit selections is rejected. `all-safe` includes every and only `SAFE` candidate and returns every non-SAFE row in `excluded`.

`clean` requires a TTY, prints the full scan and excluded rows, creates the exact plan, and requires the exact line `apply PLAN_ID`. A wrong confirmation performs no action. Non-TTY use refuses and points to `plan` then `apply`.

## Scan rules

Default roots are exactly `$HOME/Library/Caches/Homebrew`, `$HOME/Library/Caches/go-build`, and `$HOME/.cache/codex-runtimes`. Homebrew is `REVIEW` with action `none`. It cannot be selected by `all-safe`. Go alone may be `SAFE`, and only when both the exact-root rule and the owner-native tool binding below pass.

Production resolution does not trust inherited `PATH`, `GOENV`, or `GOCACHE`. It checks a fixed set of absolute Go locations, resolves symbolic links to a regular executable, verifies executable and path ownership and permissions, captures device, inode, SHA-256 fingerprint and version, and runs the absolute executable without a shell. The environment is exactly `HOME` set to the verified home, `GOCACHE` set to the exact allowlisted target, `GOENV=off`, `GOTOOLCHAIN=local`, `GOPROXY=off`, `GOSUMDB=off`, and `PATH=/usr/bin:/bin`. Classification requires `go env GOCACHE` to return that exact target. Missing, unsafe, changed, or differently configured tools yield `BLOCKED_UNKNOWN`.

Codex runtime inventory never has an action. With no injected proof of inactivity it is `BLOCKED_ACTIVE`. With proof it is `REVIEW`. Whole mixed agent roots (`.codex`, `.claude`, `.hermes`), conversations, credentials, memories, skills, config, and Application Support are never traversed or emitted; their exclusion is the protection boundary.

`--scope` adds project and worktree state. It never derives scope from the data directory and never treats `scope/go-build` as a cache. Git uses actual `git worktree list --porcelain`, then fixed per-worktree `status --porcelain` and reachability checks. Current or main and locked worktrees are `BLOCKED_ACTIVE`; dirty is `BLOCKED_DIRTY`; not proven merged is `BLOCKED_UNMERGED`; clean merged inactive is `REVIEW`; unavailable or prunable state is `BLOCKED_UNKNOWN`. Git actions are always `none`.

Candidate IDs are a truncated SHA-256 over family and a private stable key. JSON labels are generic. JSON and persisted public state exclude raw absolute HOME and scope paths, usernames, command arguments, and unrelated filenames.

## Candidate JSON

`stillmac.cleanup.v1` candidates contain exactly:

```text
id, family, rule_version, bytes, decision, reasons, action, reversible,
captured_at, label, fingerprint, current_state, root_kind
```

`fingerprint` is a SHA-256 identity over relative tree structure, file sizes, types, and modes. It does not expose the underlying names.

## Plans and target registry

A public plan contains:

```text
schema_version, plan_id, plan_hash, expires_at, host_binding, rule_set,
target_registry_hash, candidates, excluded
```

Plans expire after 15 minutes according to the injected clock. `plan_id` is derived from the canonical complete plan hash. The host binding is an opaque hash. The actual injectable per-installation host ID and absolute action targets exist only in the private target registry.

The target registry contains schema, plan ID, actual host ID, registry hash, and one target per selected candidate. Each target binds candidate ID, family, rule version, root kind, exact private path, fingerprint, decision, device, inode, and the trusted Go executable identity and configuration. Its canonical hash is bound into the public plan. Executable and cache paths remain private. Plan ID traversal, tampering, expiry, host mismatch, malformed registry, changed ordering, and target substitution fail closed.

## Protection

`protect` accepts only an ID discovered by a scan using the same current scope and rules. Private protection state records schema, stable ID, and family. Future scan returns `PROTECTED`; plan rejects it; apply rechecks protection and fails.

## Apply

Apply accepts only a plan ID and performs no user-supplied path action. It never invokes `rm`, recursive removal, a cache-root rename, Git force, or a shell. For each row it revalidates:

1. plan and target registry schema, canonical hashes, host, and 15-minute expiry;
2. protection and current supported rule version;
3. exact allowlisted target reconstruction from HOME;
4. parent components, real-directory type, ownership, device, and inode;
5. current decision and complete cache fingerprint;
6. absolute Go executable path, resolved regular-file identity, ownership, device, inode, executable fingerprint, version, sanitized environment, and exact `GOCACHE` immediately before action. This is defense-in-depth, not an atomic same-UID race guarantee.

Any difference is `BLOCKED_CHANGED` in the structured failure row and fails closed. The only successful action is equivalent to `<verified-absolute-go> clean -cache` with the fixed environment above. No shell, arbitrary argument, target path argument, or other Go subcommand is accepted.

Approval authorizes `go clean -cache` against the logical exact GOCACHE pathname. Concurrent same-account hostile mutation is not defended; private target paths remain in private state only.

The exact scanner measures before and after. `moved_bytes` is always `0`. `removed_bytes` and `reclaimed_bytes` are both `max(before_bytes-after_bytes, 0)`. Go build cache cleaning therefore reclaims measured disk space, at the cost of rebuilding cache entries during later builds.

## Receipts and history

Every attempted candidate after plan validation receives a private atomic receipt, including success and partial failure. Fields are:

```text
schema_version, candidate_id, rule_version, decision, plan_hash, before_bytes,
after_bytes, moved_bytes, removed_bytes, reclaimed_bytes, method, result,
timestamp
```

Success uses result `cleaned` and method `owner-native-go-clean-cache`. Revalidation failure uses result `blocked_changed` and method `none`. A Go command failure uses result `owner_action_failed`, method `owner-native-go-clean-cache`, and zero moved, removed, and reclaimed bytes even if the tool may have partially changed its cache. Apply returns the same rows as structured text or JSON. Writer failure returns exit code 6; state or action failure returns exit code 5 after emitting available structured rows.

History rejects malformed schemas, symbolic links, non-regular objects, wrong permissions, unknown entries, negative byte values, and inconsistent result/method/decision/byte combinations.

## State safety

Cleanup directories are `0700`; regular JSON is `0600`. Writes use a private temporary file, sync, close, and atomic rename. Unsafe selected directories, symlinked nearest parents, symlink/non-directory destinations, unknown state entries, and unsafe permissions fail closed.

## Non-goals and limitations

- No Homebrew action until a separate narrowly bounded owner-native operation is designed.
- No cache content interpretation.
- No Git mutation.
- No Codex action.
- No scheduler or end-session action. Future end-session automation may be scan-only and must never auto-clean.
- No release, Homebrew, npx, or curl installation claim.

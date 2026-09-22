# Data locations

## Selected data directory

The default is `$HOME/Library/Application Support/StillMac`. Commands with `--data-dir PATH` use that exact selected state location. `--scope` is independent and never changes the data directory.

```text
StillMac/
├── current-sample.json       optional legacy baseline state
├── samples/                  bounded immutable baseline samples
└── cleanup/
    ├── plans/                path-free public plan JSON
    ├── targets/              private host IDs and exact action targets
    ├── protected/            stable protected candidate records
    └── receipts/             success and failure action receipts
```

Selected data and cleanup directories use `0700`; regular JSON uses `0600`. State readers reject links, non-regular files, unsafe permissions, malformed schemas, and unknown entries. Baseline reports are not retained. Explicit source-candidate residue snapshots are retained as described below.

## Scanned roots

Default scan inspects metadata for exactly:

```text
$HOME/Library/Caches/Homebrew
$HOME/Library/Caches/go-build
$HOME/.cache/codex-runtimes
```

It does not inspect whole agent configuration roots, conversations, credentials, memories, skills, config, or arbitrary caches. `--scope PATH` adds Git worktree inventory and does not add `PATH/go-build` or any other invented cache.

## Owner-native Go cache action

The exact Go cache remains at `$HOME/Library/Caches/go-build`. After full plan and binding revalidation, StillMac invokes the verified absolute Go executable as `go clean -cache` with an exact sanitized environment. Go owns the cache mutation. StillMac stores only its private plan, target binding, protection, and receipt state.

## Removal

The uninstaller keeps the complete data directory. Removing retained state is outside the cleanup contract and requires a separately reviewed process.

## Source candidate additions

`residue/snapshots/` stores path-free explicit scan snapshots with a bounded
history. The [residue contract](RESIDUE-CONTRACT.md) specifies count and byte
limits and when comparisons are valid. Inspection output alone is not stored.

`retire/` holds private registrations, exact target bindings, plans, approvals,
protection state and receipts. These are separate from cache plans; IDs cannot
be substituted between action namespaces. The
[retirement contract](WORKTREE-RETIREMENT-CONTRACT.md) documents the exact schema
and retained fields. The uninstaller also retains this state.

Residue `--scope` explicitly selects supported project artifact inventory. It
does not authorise deletion of those artifacts. Retirement `--target` selects
one user-managed linked worktree for registration and evidence gathering;
mutation still requires the exact approved plan and revalidation.

Cache protection management retains disabled tombstones in `cleanup/protected/`
and a private `cleanup/protection-generation.json` counter. These invalidate
older plans. A source build and v0.1.1 should use separate `--data-dir` locations:
the old binary intentionally rejects unfamiliar private state.

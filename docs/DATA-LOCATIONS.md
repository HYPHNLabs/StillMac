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

Selected data and cleanup directories use `0700`; regular JSON uses `0600`. State readers reject links, non-regular files, unsafe permissions, malformed schemas, and unknown entries. Reports are not retained.

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

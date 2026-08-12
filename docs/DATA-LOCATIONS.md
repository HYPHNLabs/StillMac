# Data Locations

## Default

StillMac uses this per-user directory when `--data-dir` is omitted:

```text
$HOME/Library/Application Support/StillMac
```

## Custom directory

Every command accepts a custom location:

```bash
./bin/stillmac sample --data-dir /path/chosen/by/the/user
```

StillMac does not print the selected path in errors.

## Contents

```text
StillMac/
├── current-sample.json   # optional legacy compatibility state
└── samples/
    └── sample-*.json     # validated immutable observations
```

Current sampling writes only immutable entries under `samples/`. Reports are written to standard output and are not retained by StillMac.

## Permissions

Where supported:

- selected data directory: `0700`;
- `samples/`: `0700`;
- state/history files: `0600`.

Unsafe selected directories, state/history paths, and history-directory entries fail closed when they are symlinks, non-regular objects, malformed, exposed where the contract requires private modes, or outside documented bounds. Unrelated entries in the selected root are not treated as StillMac history and are never pruned.

## Retention

Retention is enforced during accepted storage operations. StillMac keeps no more than the newest 672 history samples, 14 days relative to the newest valid sample, 2 MiB per sample, and 128 MiB total encoded history.

There is no background expiry job and no general cleanup capability.

## Removal

The beta uninstaller removes only the regular installed binary and retains data. Removing a locally built binary does not remove data. There is no automated recursive purge path; inspect the exact selected directory before manually deleting it and never use a broad cache or Application Support deletion command.

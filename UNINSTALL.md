# Uninstall

Inspect the script and paths first:

```sh
less scripts/uninstall.sh
sh scripts/uninstall.sh
```

The script removes only a regular installed `~/.local/bin/stillmac` binary. It refuses unsafe options and symlinks. It keeps all baseline state, cleanup plans, receipts, and protections.

Removing retained state is outside the cleanup contract. If it must be removed, inspect the exact selected data directory and use a separately reviewed process. Do not use a broad wildcard, recursive command copied from an agent, or a path you did not resolve yourself.

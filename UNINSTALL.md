# Uninstall

Inspect the script and paths first:

```sh
curl -fsSLo /tmp/stillmac-uninstall.sh \
  https://raw.githubusercontent.com/HYPHNLabs/StillMac/v0.1.1/scripts/uninstall.sh
less /tmp/stillmac-uninstall.sh
sh /tmp/stillmac-uninstall.sh
```

The script removes only a regular installed `~/.local/bin/stillmac` binary. It refuses unsafe options and symlinks. It keeps all baseline state, cleanup plans, receipts, and protections.

The PATH entry is harmless after uninstall. If you want to remove it, delete only the StillMac export line from the profile file you chose during setup, either `$HOME/.zprofile` or `$HOME/.zshrc`.

Removing retained state is outside the cleanup contract. If it must be removed, inspect the exact selected data directory and use a separately reviewed process. Do not use a broad wildcard, recursive command copied from an agent, or a path you did not resolve yourself.

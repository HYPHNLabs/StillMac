# Uninstall

Inspect the paths before acting. The beta operation removes only a regular `stillmac` binary and keeps all data:

```sh
sh scripts/uninstall.sh
```

The beta script deliberately has no automated recursive data purge: safe descriptor-relative deletion is not implemented, so a check-then-remove race would be unsafe. To remove data, inspect first, then delete only the reviewed path with your normal filesystem tools; do not follow symlinks and do not use a broad wildcard. Custom data paths require the same inspect-first review. No scheduler, unrelated file, or user data outside the selected path is touched.

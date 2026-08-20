# Privacy

## Summary

The StillMac Go binary is local and has no network client, telemetry, analytics, cloud service, or LLM dependency. The direct `v0.1.1` release installer is a separate networked distribution surface that downloads the pinned manifest and Apple Silicon archive from GitHub Releases. Cleanup inventory and state are path-minimised and private.

## Process and memory observations

The baseline may store sanitised process basename, PID, PPID, CPU percentage, memory percentage, elapsed seconds, UTC capture time, macOS memory-pressure category, swap used bytes, and fixed data-quality fields. It never stores command lines, arguments, environment variables, full executable paths, workspace names, usernames, unrelated filenames, contents, browser data, agent conversations, tokens, or credentials.

## Cleanup public output

Candidate and plan JSON may contain stable opaque IDs, family, versioned rule, measured bytes, decision, fixed reasons, action, reversibility, UTC capture time, generic label, tree fingerprint, current state, and root kind. Git worktree labels are numbered and path-free.

Public JSON and receipts exclude absolute HOME and project paths, usernames, command arguments, cache filenames, and Git filenames. A tree fingerprint binds private structure without disclosing it.

## Cleanup private state

The private target registry contains the actual host ID, exact allowlisted Go cache path, and trusted Go executable identity and configuration because apply cannot safely act without them. Protection records, plans, target registries, and receipts remain under the selected StillMac data directory. Directories use `0700`; regular JSON uses `0600`. Unsafe links, objects, permissions, and unknown entries fail closed.

StillMac does not store a copy of cache contents. The verified Go tool owns cache cleaning. Public output reports only measured aggregate bytes and never cache filenames or contents.

## Storage and sharing

The default directory is `$HOME/Library/Application Support/StillMac`; `--data-dir` selects another. Reports go to standard output. StillMac transmits nothing. Callers control later sharing.

Baseline sample retention remains bounded to 672 samples, 14 days relative to the newest valid sample, 2 MiB per sample, and 128 MiB total. Cleanup plans expire after 15 minutes. There is no scheduler.

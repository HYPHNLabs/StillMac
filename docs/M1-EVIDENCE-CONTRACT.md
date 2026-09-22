# M1 evidence contract

This document defines the evidence rules for the M1 cleanup and legacy CLI
slice. It supplements `DEVELOPER-CLEANUP-CONTRACT.md`; it does not add a
cleanup action or change the `stillmac.cleanup.v1` JSON schema.

## Worktree evidence

Git worktree inventory is read from local `git worktree list --porcelain`
facts. The primary repository worktree is the first worktree record reported by
Git. The current worktree is the canonicalised path supplied as `--scope`.
Branch names such as `main` and `master` are not enough to identify either
role. A linked worktree may legitimately use one of those names.

For a non-current, non-primary worktree, StillMac checks clean status and
ancestry. The integration base is resolved deterministically from a supplied
local `--base REF`, or from one local default. The default candidates are local
`main`, local `master`, and a configured local `origin/HEAD` symbolic reference
when no local `main` or `master` exists. Resolution uses local Git commands
only. StillMac never fetches.

Missing or ambiguous base evidence produces `BLOCKED_UNKNOWN`. A failed
ancestor check produces `BLOCKED_UNMERGED` with `merge-unproven` state. That
state means only that ancestry was not proven. It does not claim that a change
is inactive, and it does not infer that a squash merge happened or did not
happen. Git worktree rows remain inventory-only.

## Size evidence and v1 compatibility

The public candidate shape remains exactly:

```text
id, family, rule_version, bytes, decision, reasons, action, reversible,
captured_at, label, fingerprint, current_state, root_kind
```

The legacy `bytes` field has no separate status field. A value of zero in a v1
JSON document cannot prove whether the measured size was exactly zero or the
measurement was unavailable. The scanner keeps that distinction internally:

- `exact` means the allowlisted tree was fully measured;
- `partial` means a lower-bound measurement was obtained before an unsafe or
  unreadable entry stopped traversal;
- `unknown` means no safe size measurement was established.

Human-readable scans show binary human units, the status, and a totals line.
The exact total includes exact measurements only. Partial lower bounds are
shown separately and are not included in that total. Unknown sizes are omitted
from all totals. JSON consumers must treat `bytes` as a legacy measurement
whose basis is limited by the rules above.

## Explain output

Text `explain` output expands the scan row with four explicit sections:

1. evidence gathered and current state;
2. action availability;
3. checks that remain missing or unsatisfied;
4. rebuild trade-off.

JSON `explain` output remains the v1 candidate object. For the Go build cache,
the rebuild trade-off is that later Go builds may recreate entries after a
successful clean.

## Interactive clean

`clean` always prints the current scan first. Bare `clean` then asks for fresh
candidate numbers or IDs, or `all` for all SAFE candidates. `clean all` and a
fresh `all` selection both print the exact plan and require the exact
`apply PLAN_ID` confirmation. A wrong confirmation performs no action.

When there are no SAFE candidates, the command reports that there is nothing to
clean and exits without creating a plan. Explicit selections of non-SAFE rows
remain errors. `plan` and `apply` continue to provide the non-interactive
workflow.

## Logical reduction versus filesystem space

Receipt decoding and the `stillmac.cleanup.v1` receipt fields remain unchanged.
For a successful Go action, `before_bytes` and `after_bytes` are measurements of
the exact logical cache tree, while `removed_bytes` and `reclaimed_bytes` are
the non-negative logical reduction between those measurements. They are not a
measurement or guarantee of operating-system filesystem free space. Filesystem
allocation, delayed reclamation, snapshots, and other storage behaviour remain
outside this contract. The only active action remains the verified owner-native
`go clean -cache`, and its cache entries may need to be rebuilt later.

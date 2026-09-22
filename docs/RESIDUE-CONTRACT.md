# Residue contract

This document defines the bounded, inventory-only residue slice in StillMac M1 and M2. It covers explicit project and Git worktree inspection, comparison of explicitly requested snapshots, and a user-invoked session report. It does not add deletion, automatic approval, scheduling, end-session hooks, a client adapter, or traversal of agent state.

## Boundary

The residue backend has two input classes:

1. The existing scope-free cleanup scan supplies decisions for the three exact StillMac cache roots: Homebrew downloads, the Go build cache, and the Codex runtime cache. The residue backend never changes those decisions and never performs cleanup.
2. A project or worktree root supplied explicitly by the user may be inspected for metadata-only sizes of exactly these artifact families: `node_modules`, `.next`, `dist`, `target`, and `.build`.

No project root is discovered from HOME, the data directory, Git configuration, editor state, or a broad workspace walk. An explicit project root may cause Git linked-worktree metadata to be read with a parent-injected fixed metadata-only runner for `git -C ROOT worktree list --porcelain`. If no runner is injected, the backend fails closed with `git_metadata_unavailable` and does not execute an inherited-PATH Git process. Branch names, Git output, command arguments, environment variables, and file contents are never placed in a report.

Known mixed agent roots are protected before traversal. This includes `.codex`, `.claude`, `.hermes`, `.agents`, `.cursor`, `.continue`, `.aider`, `.cline`, `.roo`, `.windsurf`, `.gemini`, `.goose`, `.amazon-q`, and `.kiro`. A Codex-owned linked worktree is inventory-only and owner-managed at most. Its artifact contents are not measured. A symlink or canonical alias into a protected root is also not traversed.

The only supported actions in this slice are `inventory-only`, `owner-managed`, and the existing cleanup package's approval-gated candidate decision. There is no residue removal action.

## Public backend API

The package is `stillmac/internal/residue` and is deliberately CLI-neutral.

```go
engine, err := residue.New(residue.Config{
    Home:         home,
    DataDir:      dataDir,
    Now:          clock,
    GitRunner:    gitRunner,
    GoCleaner:    goCleaner,
    Limits:       residue.Limits{MaxItems: 4096, MaxDepth: 16, MaxBytes: maxBytes, MaxDuration: timeout},
    HistoryLimit: 32,
})

report, err := engine.Inspect(scopes)                         // read-only
snapshot, err := engine.Snapshot(scopes)                      // explicit private write
changes, err := engine.Changes(residue.ChangeOptions{})       // latest pair
session, err := engine.SessionReport(residue.SessionReportOptions{
    Scopes: scopes, QuietUnchanged: quiet, ThresholdBytes: threshold,
})
```

`Scope.Path` is required. An empty `Scope.Kind` means `project`; `repository` and `worktree` are also accepted. `Scope.Alias` and `Config.Aliases` are bounded human-facing labels only. They are not report fields. A CLI that wants to render one may call `residue.ScopeID(scope)` and `engine.AliasForScope(scope)` while keeping the label in text output outside the machine report.

The concrete methods are:

```text
New(Config) (*Engine, error)
(*Engine).Inspect([]Scope) (Report, error)
(*Engine).Snapshot([]Scope) (Report, error)
(*Engine).Changes(ChangeOptions) (Report, error)
(*Engine).SessionReport(SessionReportOptions) (Report, error)
ScopeID(Scope) (string, error)
(*Engine).AliasForScope(Scope) (string, bool)
```

`Inspect` never creates or updates the residue state directory. `Snapshot` appends only because the caller explicitly requested it. `SessionReport` also appends one snapshot because it is an explicit user invocation. It never runs in the background.

## Report envelope

Every machine report uses `stillmac.residue.report.v1` and contains stable opaque IDs, not raw paths:

```text
schema_version, report_id, snapshot_id, report_kind, status, captured_at,
rule_set, scope_bindings, evidence, actions, next_step, entries,
from_snapshot, to_snapshot, changes, quiet, suppressed
```

`report_kind` is `inspect`, `snapshot`, `changes`, or `session-report`. `rule_set` is `residue-rules.v1`. The report status is one of `complete`, `partial`, `unknown`, or `not-present`.

Every `size` and `evidence.total_size` object contains:

```json
{"status":"complete|partial|unknown|not-present", "bytes":null}
```

Complete measurements always carry a non-negative integer `bytes`. Partial measurements may carry a measured lower bound. Unknown and not-present measurements always carry `bytes: null`. A missing root is therefore not represented as zero, and an unreadable root is never treated as a missing root.

An entry contains its opaque ID, scope ID, fixed family and rule version, size, state, opaque root identity, optional metadata fingerprint, action availability, cleanup candidate decision when one exists, and fixed evidence codes. Relative names used to make a fingerprint are hashed and never emitted.

The evidence object also contains `limits_applied`. Its production value makes the boundary explicit: the item, depth, byte, and duration limits apply to explicit project artifact trees and linked-worktree metadata. The exact-root cleanup decision scan is delegated to `internal/cleanup` and is not governed by those residue traversal limits. This is deliberate because that package owns the existing cache contract. The CLI must not claim that a residue duration limit bounds the whole exact-cache decision scan.

## Measurement rules

The explicit-root scanner is metadata-only. It uses `Lstat`, directory metadata, file sizes, file modes, and device/inode identity. It never opens an artifact file, follows a symlink, interprets a filename, or reads a command environment.

The scanner is bounded by:

- `MaxItems`, counting visited metadata entries and linked-worktree rows;
- `MaxDepth`, measured from each artifact root;
- `MaxBytes`, a logical accounting ceiling for measured regular-file sizes; and
- `MaxDuration`, checked during metadata traversal.

The default logical byte ceiling is `math.MaxInt64`; the practical resource bounds are the item, depth, and duration limits. A bound hit produces `partial` evidence with a warning code such as `item_bound_reached`, `depth_bound_reached`, `byte_bound_reached`, or `time_bound_reached`. Permission failures, symlinks, special files, missing identities, and unreadable directories produce explicit partial or unknown evidence.

Regular files are deduplicated by device and inode within each artifact row. The report's aggregate total deduplicates those identities again across overlapping explicit scopes and linked worktrees. Symlinks and special files are skipped, not followed or counted. A hardlink is counted once. A replacement of an artifact root changes its opaque identity and cannot become a size delta.

Git metadata is bounded before parsing at one mebibyte. A failed or non-zero Git metadata command is reported as `git_metadata_unavailable` and marks the binding partial. It is not silently presented as an empty repository. No status, merge-base, filter, fsmonitor, or other Git command is run by the residue backend.

## Snapshots and changes

Snapshots are private JSON files at:

```text
DATA-DIR/residue/snapshots/snapshot-<opaque-id>.json
```

The residue directory and snapshots directory use mode `0700`. Snapshot files use mode `0600`, are written through a synced temporary file and atomic rename, and are bounded by count and total bytes. Existing files, symlinks, wrong permissions, unknown entries, malformed JSON, unknown JSON fields, invalid statuses, and invalid IDs fail closed. Residue history is separate from `DATA-DIR/cleanup`; the two schemas are never mixed.

`changes` compares the latest two snapshots unless both `--from` and `--to` are supplied. It never searches older history for a silently compatible pair. The pair must have the same rule set, the same ordered scope bindings and root identities, and the same entry IDs and rule versions. A mismatch is visible as `unknown` with `scope_binding_mismatch`, `rule_set_mismatch`, or `entry_set_mismatch` evidence.

Complete entries with stable identities may report `unchanged`, `growth`, `shrink`, or `replaced`. Missing, partial, unknown, not-present-to-present, permission-denied, identity-unavailable, and scope-changed measurements never become growth, shrink, or reclaimed bytes. The backend never infers reclaimed space from a shrink. It marks `regrowth` only when all of the following are present:

1. the entry is the exact Go build-cache candidate;
2. a successful `owner-native-go-clean-cache` receipt with positive removed bytes exists;
3. an earlier complete comparable snapshot proves the cache was larger before that receipt;
4. the immediately compared complete snapshot proves the post-receipt cache was smaller; and
5. the current complete snapshot proves growth from that reduced observation.

Without that linked receipt and comparable reduction evidence, a later increase is ordinary `growth`, never inferred `regrowth`.

## Session reports

`session-report` is a user-invoked scan, report, and explicit snapshot append. It never schedules itself and has no client or end-session hook. The first invocation is always visible and says that it established the first explicit snapshot. Later invocations compare the new snapshot with the immediately previous one.

When `QuietUnchanged` is true, the report sets both `quiet` and `suppressed` only if the comparison is complete or known not-present, every row is comparable, and every growth or shrink delta is at or below `ThresholdBytes`. The CLI may emit no text when `suppressed` is true. It must still emit an incomplete, unknown, replaced, scope-mismatched, or first report. A quiet result is still stored as a snapshot.

## CLI integration recipe

The existing CLI should add four commands without changing v1 cleanup schemas or exit codes:

```text
inspect [--scope PATH ...] [--format text|json] [limits]
snapshot [--scope PATH ...] [--format text|json] [limits]
changes [--from ID --to ID] [--format text|json]
session-report [--scope PATH ...] [--threshold-bytes N] [--quiet] [--format text|json] [limits]
```

The adapter should construct `residue.Config` from the selected HOME, data directory, the parent-owned fixed and bounded Git metadata runner, existing Go cleaner, and parsed limits. It should pass scopes exactly as supplied and keep aliases out of JSON. JSON output is the `Report` value. Text output may add aliases and human-readable labels, but must not add absolute paths, usernames, branch names, file names, command arguments, environment values, or file contents.

Recommended command mapping:

| Command | Backend call | State write |
|---|---|---|
| `inspect` | `Inspect(scopes)` | none |
| `snapshot` | `Snapshot(scopes)` | one snapshot |
| `changes` | `Changes(ChangeOptions{...})` | none |
| `session-report` | `SessionReport(options)` | one snapshot |

`ErrInvalidScope` and invalid option values are usage failures. `ErrDataDir` and `ErrHistory` are local-state failures. Scanner evidence such as partial or unknown is a successful structured report, not permission to invent a zero or claim a reclaim.

## Non-goals

This contract does not add arbitrary deletion, force removal, `sudo`, process termination, cache-root renaming, cache-content interpretation, whole-home discovery, agent conversation or memory access, network collection, telemetry, an LLM dependency, a scheduler, a runtime installer or updater, or automatic approval. Codex-owned worktrees remain inventory or owner-managed. Existing cleanup protections and action management remain in `internal/cleanup`.

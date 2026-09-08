# M1 and M2 candidate acceptance

This document separates source implementation from release and user validation.
The published v0.1.1 binary is unchanged by this development branch. New commands
require a build from the reviewed source until a separately approved release.

## M1: a useful first scan

The intended outcome is an informed storage decision within five minutes of
installation. This is a pilot target, not a measured claim about this candidate.

- Show complete, partial and unavailable measurements distinctly.
- Explain ownership, missing evidence, available action and rebuild tradeoffs.
- Inventory only explicitly selected project scopes and allowlisted artifact
  families, without persisting their names or contents in public reports.
- Keep the terminal and Agent Skill on the same deterministic command boundary.
- Preserve explicit selection, exact-plan approval and current-state validation.
- Keep Homebrew inventory-only unless its native tool can enforce the separately
  reviewed exact-target contract. A broad cleanup command is not a substitute.

## M2: account for what remains after a coding session

- Preserve comparable explicit scan snapshots and report growth without treating
  missing, partial or changed coverage as a storage reduction.
- Allow inspection and deliberate removal of user protection records. Removing
  protection is not approval to clean anything.
- Provide a separate, narrowly contracted retirement flow for explicitly
  registered user-managed Git linked worktrees.
- Separate user attestation of a completed session from machine-observed Git
  state. Clean, merged or old does not establish inactivity.
- Keep owner-managed agent worktrees outside the native retirement action.
- Permit explicitly invoked session reports, with no installed scheduler,
  client hook, automatic plan, automatic approval or automatic cleanup.

## Engineering evidence

Production behaviour requires a failing behavioural test before implementation.
Each integrated candidate must pass Go race tests, vet, formatting, trimpath
builds, distribution checks and privacy inspection. New native Git mutation is
tested only in disposable repositories, including blocked and changed-state
cases. A passing fixture test does not establish every real-machine scenario.

Measurement labels distinguish logical file-size reduction from independently
observed filesystem free-space change. Reconstruction from a preserved Git
commit is not recovery of untracked or ignored local data.

## Field and release gates still required

| Gate | Required evidence | State |
| --- | --- | --- |
| First-use comprehension | At least 8 of 10 pilots distinguish actionable, blocked and unmeasured rows without coaching | Not measured |
| Useful first result | At least 5 pilots identify a useful cleanup or evidence-backed keep decision | Not measured |
| Repeat use | At least 5 pilots return independently during the following month when another relevant development session occurs | Not measured |
| Live agent compatibility | A recorded version-qualified Codex interaction covering selection, refusal, exact approval and receipt | Not verified by a simulated harness |
| Platform coverage | Runtime evidence on each macOS version claimed in a release | Existing v0.1.1 claims only |
| Distribution | Separately reviewed release assets and installation routes | No new release authorised here |

Pilot feedback should record only opt-in aggregate outcomes: installation
completed, time to first decision, useful or not useful, missing resource family,
repeat use, and voluntarily supplied comments. No automatic telemetry, raw
workspace paths, private reports or conversations are needed.

## Source integration evidence, 8 September 2026

M1 terminal evidence, explicit residue inventory/history, source capability
negotiation, local Skill preparation, protection management and guarded
user-managed worktree retirement are integrated in the candidate branch.

Local checks passed: formatting, the full Go race-test suite, vet, a trimpath
build, 74 distribution/Skill/privacy tests, shell syntax, and inspection of
tracked files and the binary for real home/workspace strings. Native retirement
and reconstruction were exercised only in disposable fixture repositories.

Review fixes include stale-plan invalidation after unprotect, descriptor-bound
single-link protection tombstones, guarded Git configuration and execution,
Skill destination races, malformed/overfull history handling, and CLI disclosure
of blocked retirement evidence. This records source verification, not a new
release, independent security certification, live agent integration or pilot
traction. The field and release gates above remain outstanding.

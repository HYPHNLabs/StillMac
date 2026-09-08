# StillMac Agent Workflow

This document describes the local Agent Skill preparation route and the
released StillMac workflow. It does not activate a provider distribution
route, change Codex configuration, or publish a package.

## Scope and release status

| Surface | Status | Contract |
| --- | --- | --- |
| StillMac binary `v0.1.1` | Released | The current CLI and cleanup contracts |
| `skills/stillmac/SKILL.md` | Released skill instructions | Targets the `v0.1.1` binary |
| `scripts/install-skill.sh` | Local preparation only | Requires an explicit source and target |
| `inspect`, `snapshot`, `changes`, `session-report`, `retire` | Forthcoming source CLI | Not supported by the released skill |

The released `v0.1.1` command set is `doctor`, `sample`, `status`, `report`,
`scan`, `explain`, `plan`, `apply`, `clean`, `protect`, and `history`. The
forthcoming source commands must not be presented as available until a later
versioned CLI and skill contract are released.

## Capability negotiation for source CLI

The skill may encounter a local source-milestone binary before those commands
are released. Start with the read-only command `stillmac capabilities --format
json`. An unknown-command result is the expected fallback for released
`v0.1.1`; use the released workflow and do not guess source capabilities.

Source commands may be considered callable only when the response has the
exact schema `stillmac.capabilities.v1`, exact profile `m1-m2-source`, and an
explicit `commands` array containing the command being requested. A profile or
`released` value is local feature negotiation, not a support or release claim.
Live Codex compatibility remains unverified.

The no-scope `capabilities`, `changes`, `history`, `protect`, and `unprotect`
commands use only their documented inputs. A project `inspect` invocation may
use a scope only when the user supplied that exact path. Do not derive scope
from a data directory or silently scan a home directory. Before any `retire`
flow, the user must explicitly attest that the selected target is user-managed
and the session has ended. Do not infer those facts from age, merge state,
branch state, or a clean worktree. Use the exact target supplied by the user
with `retire plan --target PATH --user-managed --session-ended`. Review the
exact retire plan, exclusions, protections, expiry, evidence, and recovery
information, then obtain explicit approval before `retire apply ID`. Never
invent a plan ID or silently unprotect a resource.

## Local installation preparation

Run the script from a versioned checkout or an extracted local skill package.
The source directory must contain a valid `SKILL.md`, its directory name must
match the skill name, and its `cli-release` metadata must match the supplied
version.

The destination is an exact user input. The script does not guess a Codex,
provider, repository, or user directory. It requires an absolute source path,
an absolute target path, an existing target parent owned by the invoking user
and not group or world writable, and an absent target directory.

The interface is:

```text
install-skill.sh VERSION SOURCE TARGET
```

Example for a user-scoped target:

```
mkdir -m 700 -p "$HOME/.agents/skills"
sh scripts/install-skill.sh v0.1.1 \
  "$PWD/skills/stillmac" \
  "$HOME/.agents/skills/stillmac"
```

Example for a repository-scoped target:

```
mkdir -m 700 -p "$PWD/.agents/skills"
sh scripts/install-skill.sh v0.1.1 \
  "$PWD/skills/stillmac" \
  "$PWD/.agents/skills/stillmac"
```

The script performs no network access, privilege escalation, shell-profile
change, Codex configuration change, or automatic restart. It refuses to
overwrite an existing target, including a symlink. There is no update mode in
this preparation slice. Review a new version as a new explicit preparation
and handle any removal or replacement as a separately authorised action.

The exact target is claimed with an atomic directory creation after the
private stage is verified. If copying or final verification fails after that
claim, the script leaves the claimed target in place for inspection, removes
only its private stage, prints an incomplete-preparation warning, and exits
non-zero. A target is successful only when its receipt and deterministic
verification complete.

The staged package contains the source files plus a deterministic
`.stillmac-install.json` receipt:

```
{
  "schema_version": "stillmac.skill-install.v1",
  "skill_name": "stillmac",
  "release_version": "v0.1.1",
  "payload_sha256": "..."
}
```

Directories are owner-only (`0700`). Regular files are owner-only and retain
only their owner execute bit when the source file is executable. The receipt
is owner-only (`0600`). Symlinks, special files, and control characters in
file names are rejected. The digest covers relative file and directory names,
file bytes, and executable status. It contains no absolute source or target
path and no timestamp.

## Validation

Run the deterministic local harness:

```
python3 tests/test_skill_distribution.py
sh -n scripts/install-skill.sh
```

If the reference validator is available, also run:

```
skills-ref validate "$PWD/skills/stillmac"
```

The local evidence for this delivery was collected on 2026-09-07. `codex
--version` reported `codex-cli 0.145.0`; `skills-ref` was not available on
`PATH`. The Python harness checks the required frontmatter shape, version
boundary, deterministic receipt, restrictive modes, no-overwrite behaviour,
and the command and approval sequence. That fallback is not a substitute for
the reference validator.

The harness uses synthetic IDs and a synthetic plan ID only. It proves that
the represented sequence does not call `apply` without an approval flag. It
does not invoke a real Codex host, access a real provider directory, execute
StillMac cleanup, or provide live end-to-end Codex activation evidence.

The official OpenAI skills guide says that a skill is a directory containing
`SKILL.md`, that local skill discovery includes repository `.agents/skills`
and user `$HOME/.agents/skills`, and that Codex may need a restart after a
change. The [OpenAI build skills guide](https://developers.openai.com/codex/skills)
was accessed on 2026-09-07. The [Agent Skills specification](https://agentskills.io/specification)
was accessed on 2026-09-07 and defines the required `name` and `description`
frontmatter fields and the `skills-ref validate` route.

## Released cleanup workflow

The skill may orchestrate the released binary only:

1. Run `stillmac scan --format json`, adding `--scope PATH` only when the
   user supplied that project path.
2. List every current row, including its stable ID, decision, reason, and
   protection state.
3. Ask the user to select current `SAFE` stable IDs or `all-safe`. Do not
   choose silently.
4. Run `stillmac plan` with the exact selection and show included rows,
   excluded rows, expiry, and plan ID.
5. Obtain approval for that exact plan. Approval covers only the plan shown.
6. Run `stillmac apply PLAN_ID` and report every receipt row. Treat reclaimed
   bytes as measured logical byte reduction, not as a filesystem free-space
   measurement.

Homebrew, Codex runtime, and Git worktree rows remain inventory-only. The
skill must not invent IDs, paths, plan IDs, cache rules, or approvals. A
baseline `sample` requires explicit consent and establishes temporal
association, not process causation. There is no scheduler or automatic
cleanup.

## Pilot protocol

Pilot validation is a manual, evidence-producing preparation exercise. It is
not a release result and no result is claimed here.

1. Use a disposable local checkout or extracted package at the exact version
   under review.
2. Create an owner-only parent in a disposable test directory and supply its
   absolute target to `install-skill.sh`.
3. Confirm that an existing target is refused without changing its sentinel
   file.
4. Confirm the receipt schema, digest stability, file modes, and source
   immutability.
5. If a supported Codex host is available, restart it only as the host
   documentation requires and check whether the explicitly targeted skill is
   discoverable. Record discovery as tested or not tested.
6. If a local binary returns the exact source capability schema and profile,
   read-only source commands may be tested with their documented inputs. For
   `retire`, require the user-managed and session-ended attestations, review
   the exact plan, and do not apply it as part of skill-install validation.
7. Otherwise use a current released StillMac binary for a read-only `doctor`
   or `scan`. Do not treat a source profile as a release or compatibility
   claim.

Stop on a target collision, source symlink or special file, unsafe
permissions, version mismatch, unexpected command, or output containing
unredacted private paths, usernames, credentials, tokens, prompts, or
conversation content.

Use this redacted feedback shape. Replace every local value that could
identify a person, home, workspace, or account with `[REDACTED]`.

```
pilot_id: [REDACTED]
date_utc: [REDACTED]
host_os: [REDACTED]
architecture: arm64|[REDACTED]
codex_version: [REDACTED]
skills_ref_available: yes|no|not-tested
source_release: v0.1.1|[REDACTED]
target_scope: repository|user|other
target_path: [REDACTED]
installer_exit: 0|nonzero
receipt_schema: stillmac.skill-install.v1|[REDACTED]
skill_discovery: visible|not-visible|not-tested
workflow_result: scan-only|blocked|not-tested
unexpected_output: [REDACTED]
evidence_refs: [REDACTED]
```

Do not attach raw logs, shell history, Codex conversations, configuration
files, tokens, credentials, or unredacted paths to pilot feedback.

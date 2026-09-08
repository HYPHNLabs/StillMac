---
name: stillmac
description: Use StillMac for explicit local macOS baseline observation and approval-gated owner-native Go build cache cleaning.
metadata:
  cli-release: "v0.1.1"
  skill-contract: "stillmac.skill.v1"
---

# StillMac

## Runtime boundary

Use the installed `stillmac` binary. This skill is instruction-only and does
not implement a second cleanup path.

Never invent paths, candidate IDs, plan IDs, cache rules, shell deletion
commands, Git cleanup commands, or host facts. Do not interpret `SAFE` as
permission. In the released `v0.1.1` fallback, only a verified Go build cache
row can be executable. Homebrew, Codex, and Git rows are inventory only. A
recognised local source profile may expose a separate user-managed Git
worktree retirement flow under the source rules below; it does not change the
released fallback or enable Homebrew or Codex actions.

The direct `v0.1.1` GitHub Release installer supports Apple Silicon only.
Homebrew, npx, signing, notarisation, and Intel support remain inactive. Do
not claim that those routes work.

## Capability negotiation

Before using a source-milestone command, first invoke the read-only
`stillmac capabilities --format json` command.

- If the released binary reports an unknown command, use the documented
  `v0.1.1` workflow below. An unknown command is expected for that release.
- Accept source-milestone commands only when the JSON has the exact schema
  `stillmac.capabilities.v1`, the exact profile `m1-m2-source`, and an explicit
  `commands` array containing the command to be used. Reject malformed JSON,
  another schema, another profile, or a missing command.
- Treat the profile and its `released` value as feature negotiation for that
  local binary, never as a release, support, or compatibility claim. Live
  Codex compatibility remains unverified.

The negotiated source command set may include `inspect`, `snapshot`, `changes`,
`session-report`, `retire`, `protect`, and `unprotect`. Use only the exact
entries returned by the capability response. Do not infer capabilities from a
binary version, command error text, age, merged state, or the current working
directory. The no-scope `capabilities`, `changes`, `history`, `protect`, and
`unprotect` commands must use only their documented inputs. A project
`inspect` invocation may use a human-selected scope only when the user
supplied that exact path. Never derive scope from a data directory or silently
scan a home directory.

Before `retire`, the user must explicitly attest that the selected target is
user-managed and the session has ended. Do not infer either fact from age,
branch state, merge state, or a clean worktree. Use the exact target supplied
by the user with `retire plan --target PATH --user-managed --session-ended`.
Review the exact retire plan, including selected IDs, evidence, exclusions,
protections, expiry, and recovery information, then obtain explicit approval
before `retire apply ID`. Never invent a plan ID or silently unprotect a
resource.

## Released v0.1.1 CLI

This skill targets the released `v0.1.1` command set:

- `doctor`, `sample`, `status`, and `report` for the local baseline;
- `scan`, `explain`, `plan`, `apply`, `clean`, `protect`, and
  `history` for the contracted developer-cache workflow.

Use only commands present in that release and follow the current cleanup
contract. If the installed binary reports a different version or lacks a
needed command, stop and report the mismatch.

## Conversational cleanup flow

Follow these steps in order:

1. **Scan** with `stillmac scan --format json`, adding `--scope PATH` only
   when the user supplied that project path.
2. **List** every candidate with its current display number, stable ID, label,
   bytes, decision, and reason. Include blocked, review, and protected rows.
3. **Choice**: ask the user to select current `SAFE` IDs or `all-safe`. A
   human-controlled `clean` TTY may use fresh display numbers, but agent
   planning must use stable IDs. Do not choose silently or mix `all-safe` with
   explicit selections.
4. **Plan** with `stillmac plan` and the exact selected IDs, same scope, and
   selected data directory. Show included and excluded rows, expiry, and plan
   ID.
5. **Approval**: require the user to approve that exact plan. Explain that
   approval authorises `go clean -cache` against the logical exact GOCACHE
   pathname. Receipts report measured non-negative logical byte reduction, not
   filesystem free-space change. Later Go builds may need to rebuild cache
   entries, and malicious concurrent same-UID
   pathname replacement is outside the protection boundary.
6. **Apply** with `stillmac apply PLAN_ID`. Report every receipt row, including
   partial failures. Do not retry a `BLOCKED_CHANGED` row without a new scan
   and plan.

`stillmac clean` may be offered only for a human-controlled TTY. It prints the
same full list and accepts only `apply PLAN_ID`. For automation, always use
separate plan and apply steps.

## Protection and history

Use `stillmac protect ID` only for an ID from the same current scan. Use
`stillmac history --format json` to inspect prior receipts. Protection must
remain visible in future lists and blocks both planning and apply.

## Baseline flow

Before each `sample`, explain the allowlisted process and memory fields and
obtain explicit consent. Then use `doctor`, `sample`, `status`, and
`report`. These observations establish temporal association only, never
process causation.

## Forthcoming source CLI

The source milestones may add `inspect`, `snapshot`, `changes`,
`session-report`, and `retire` as separate commands. They are not part of the
released v0.1.1 CLI. A recognised `m1-m2-source` response may make them
available for a local development workflow only, subject to the exact
capability negotiation above. Do not invoke, simulate, or claim those
commands work on the released binary. A negotiated source profile is not a
public release or compatibility claim.

## Automation

There is no scheduler. Any future end-session automation is scan-only. Never
auto-clean, auto-plan, auto-approve, or generate shell removal commands.

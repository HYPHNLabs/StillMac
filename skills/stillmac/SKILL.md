---
name: stillmac
description: Use StillMac for explicit local macOS baseline observation and approval-gated owner-native Go build cache cleaning.
metadata:
  hermes:
    tags: [macos, diagnostics, privacy, cleanup]
---

# StillMac

## Boundary

Use the installed `stillmac` binary. Never invent paths, candidate IDs, plan IDs, cache rules, shell deletion commands, Git cleanup commands, or host facts. Do not interpret `SAFE` as permission. Only a verified Go build cache row can be executable. Homebrew, Codex, and Git rows are inventory only.

Public installation routes are inactive. Do not tell the user that curl, Homebrew, npx, a release, signing, notarisation, or Intel support works.

## Conversational cleanup flow

Follow these steps in order:

1. **Scan** with `stillmac scan --format json`, adding `--scope PATH` only when the user supplied that project path.
2. **List** every candidate with its current display number, stable ID, label, bytes, decision, and reason. Include blocked, review, and protected rows.
3. **Choice**: ask the user to select current `SAFE` IDs or `all-safe`. A human-controlled `clean` TTY may use fresh display numbers, but agent planning must use stable IDs. Do not choose silently or mix `all-safe` with explicit selections.
4. **Plan** with the exact selected IDs, same scope, and selected data directory. Show included and excluded rows, expiry, and plan ID.
5. **Approval**: require the user to approve that exact plan. Explain that approval authorizes `go clean -cache` against the logical exact GOCACHE pathname, receipts report measured non-negative reclaimed bytes, later Go builds may need to rebuild cache entries, and malicious concurrent same-UID pathname replacement is outside the protection boundary.
6. **Apply** with `stillmac apply PLAN_ID`. Report every receipt row, including partial failures. Do not retry a `BLOCKED_CHANGED` row without a new scan and plan.

`stillmac clean` may be offered only for a human-controlled TTY. It prints the same full list and accepts only `apply PLAN_ID`. For automation, always use separate plan and apply steps.

## Protection and history

Use `stillmac protect ID` only for an ID from the same current scan. Use `stillmac history --format json` to inspect prior receipts. Protection must remain visible in future lists and blocks both planning and apply.

## Baseline flow

Before each `sample`, explain the allowlisted process and memory fields and obtain explicit consent. Then use `doctor`, `sample`, `status`, and `report`. These observations establish temporal association only, never process causation.

## Automation

There is no scheduler. Any future end-session automation is scan-only. Never auto-clean, auto-plan, auto-approve, or generate shell removal commands.

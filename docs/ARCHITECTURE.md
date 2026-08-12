# Architecture

## Overview

StillMac is a single local Go CLI with no third-party dependencies. The current data flow is:

```text
macOS native probes
        ↓
allowlisted parsers and validators
        ↓
validated immutable local history
        ↓
baseline status or preliminary report
```

## Packages

- `cmd/stillmac` — process entry point and stable exit behaviour.
- `internal/cli` — command parsing, command orchestration, output, and generic errors.
- `internal/doctor` — deterministic host and data-directory readiness checks.
- `internal/observe` — native process/memory collection and allowlisted parsing.
- `internal/state` — strict JSON validation, private storage, immutable history, bounds, and rollback.
- `internal/baseline` — pure seven-day coverage calculations from validated samples.
- `internal/report` — preliminary JSON and Markdown rendering.

## Command flow

### `doctor`

Validates macOS, fixed native probes, and the selected data directory. It may create the selected directory and a temporary private write probe; it changes no system configuration.

### `sample`

Collects process and memory measurements sequentially, validates the sample, and appends an immutable private history file. It emits only a small aggregate result.

### `status`

Reads and validates all bounded history, sorts by parsed capture time, and computes elapsed span, distinct UTC dates, distinct 30-minute intervals, largest gap, quality counts, and stable blockers.

### `report`

Selects the latest validated sample and emits a preliminary low-confidence report. It does not perform trend inference or causal attribution.

## Storage model

New observations are immutable files under `samples/`. An older `current-sample.json` may be read for compatibility but is no longer created by current sampling. The store validates every entry before read, append, or eligible retention pruning.

Bounds:

- 672 newest history samples;
- 14 days relative to the newest valid sample;
- 2 MiB per history sample;
- 128 MiB total encoded history.

## Design constraints

- Local and deterministic.
- Standard library only.
- No scheduler or long-running process.
- No network or telemetry.
- Distribution scripts are outside the Go runtime: the installer fetches a versioned release asset and manifest, while the binary remains no-network.
- No scheduler; the Agent Skill is a thin invocation layer over the same core.
- No LLM dependency.
- No host mutation or general cleanup.
- Fixed errors never reveal rejected values or paths.

The normative behaviour is [V0.1-TRACER-CONTRACT.md](V0.1-TRACER-CONTRACT.md). This document is explanatory; the contract and tests take precedence.

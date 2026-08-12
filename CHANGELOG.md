# Changelog

All notable changes will be documented in this file.

StillMac has not published a release.

## Unreleased

### Added

- Deterministic `doctor`, `sample`, `status`, and `report` commands.
- Allowlisted macOS process and memory collection.
- Strict local sample, state, status, doctor, and report schemas.
- Bounded immutable history with private permissions and fail-closed validation.
- Seven-day elapsed-span, observed-day, and interval-coverage status gates.
- Preliminary JSON and Markdown reports with explicit confidence and causation limits.
- Privacy-hostile fixtures, storage failure injection, race tests, and path-free builds.

### Security

- Symbolic-link and non-regular state objects are rejected.
- Unknown, malformed, oversized, duplicate, and out-of-bound history fails closed.
- Native command output and private paths are excluded from state and user-visible errors.

### Not included

- No scheduler, remote activation, telemetry, recommendations, process action, cache inspection, or port inspection. Distribution scripts and a thin Agent Skill are present, but no release, tap, or npx activation exists.

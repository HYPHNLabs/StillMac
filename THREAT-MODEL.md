# Threat Model

## Scope

This threat model covers the current StillMac read-only CLI: `doctor`, `sample`, `status`, and `report`.

Excluded runtime surfaces remain scheduler, network access, cache/port inspection, recommendations, and active remediation. Distribution scripts, uninstaller, and a thin Agent Skill now exist as separately reviewed public surfaces; remote activation is not present.

## Protected assets

- command arguments, environment variables, credentials, and private paths;
- browser, workspace, agent, and personal file contents;
- integrity and confidentiality of StillMac-owned local observations;
- the last known valid observation history;
- accurate representation of confidence, coverage, and causation limits.

## Trust boundaries

1. macOS native output enters the collector and is untrusted.
2. User-selected storage paths and existing filesystem objects are untrusted.
3. Stored JSON is untrusted on every read, even if StillMac previously wrote it.
4. Standard-output consumers are outside StillMac's control.
5. Build and future distribution infrastructure are not part of the current runtime.

## Principal threats and controls

### Sensitive native output leakage

**Threat:** command output contains paths, arguments, or credential-shaped data.

**Controls:** fixed native command forms, allowlisted parsing, basename sanitisation, in-memory discard, fixed path-free errors, hostile-fixture tests.

### State-path substitution

**Threat:** symlinks or non-regular files redirect reads, writes, permission changes, or pruning.

**Controls:** no-follow directory/file opens where available, descriptor-based permission repair, regular-file checks, identity rechecks, and fail-closed handling of unknown entries.

### Corrupt or adversarial state

**Threat:** malformed, oversized, duplicated, backdated, or unknown JSON causes unsafe behaviour or unbounded work.

**Controls:** strict schemas, required-member checks, unknown/trailing-value rejection, canonical timestamps, exact duplicate rejection, bounded directory enumeration, count/age/file/total-size limits, and non-mutating read failures.

### Partial storage failure

**Threat:** an interrupted append or prune destroys prior valid history or reports false success.

**Controls:** private temporary files, fsync, atomic no-replace publication, explicit rollback, injected failure tests, generic failure exits, and preservation of validated prior state.

### Misinterpretation of observations

**Threat:** users infer causation, high confidence, or permission to act.

**Controls:** reports remain preliminary and low confidence; process and memory measurements are described only as temporally associated; `recommendations_enabled` is always false; coverage readiness does not authorise action.

### Supply-chain risk

**Threat:** an unverified build or future installer is treated as a trusted release.

**Controls today:** reproducible builds, checksums, archive type validation, staged doctor, rollback tests, clean-account fixtures, and a fail-closed source installer. A release-generated installer must embed a reviewed manifest digest and verify it before parsing archive hashes; the Go binary remains no-network. Remote release activation, provenance, and owner/licence approval remain required.

## Residual risks

- Process labels are lossy and can collide; PIDs can be reused.
- Local administrators and malware with equivalent user access can read or alter local state despite file modes.
- The current host evidence does not prove compatibility with every target macOS version or Intel hardware.
- Standard output can be redirected or shared by callers outside StillMac's control.

## Future-change rule

Any mutation, scheduler, network, cache, port, or policy feature expands this model and must be reviewed before implementation or release. The installer, uninstaller, and thin Agent Skill are documented distribution surfaces; no scheduler or remote activation exists.

# Security Policy

## Supported versions

StillMac has not published a supported release. The current repository state is a pre-release beta candidate and may change without compatibility guarantees.

## Reporting a vulnerability

Do not disclose a suspected vulnerability in a public issue.

When this repository is hosted on GitHub, use GitHub Private Vulnerability Reporting if it is enabled. If it is not available, contact the repository owner through an owner-controlled private channel and include:

- affected command and revision;
- impact and required preconditions;
- minimal reproduction steps;
- whether local private data may be exposed or modified;
- any suggested mitigation.

Do not include real credentials, private files, or unrelated user data in a report.

## Security boundary

The Go binary is read-only with respect to the host system. It can read allowlisted process and memory measurements and write only to its selected StillMac data directory. It contains no network, telemetry, cleanup, process-control, arbitrary-command, scheduler, updater, or elevated-privilege capability. Separately reviewed distribution surfaces include a fail-closed source installer, a release-generated installer template that pins a reviewed manifest digest before archive validation, a keep-data uninstaller, and a thin Agent Skill; remote release activation is not present.

Security-sensitive invariants include:

- strict allowlisted JSON schemas;
- path-free fixed user errors;
- no-follow and regular-file checks around local state;
- private directory/file permissions;
- bounded immutable history;
- atomic publication and preservation of prior valid state on failure;
- no causal or action-readiness claims from observational data.

See [THREAT-MODEL.md](THREAT-MODEL.md).

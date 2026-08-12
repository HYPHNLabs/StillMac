# Privacy

## Summary

The StillMac Go binary operates locally and has no network client, telemetry, analytics, cloud service, or LLM dependency. The separately distributed installer uses the network only to fetch a versioned release asset and its checksum manifest; it does not send observation data. StillMac collects only a narrow allowlist needed for a process-and-memory observation.

## Collected fields

Each process observation may contain:

- sanitised executable/accounting basename (`comm`);
- process ID and parent process ID at collection time;
- CPU percentage;
- memory percentage;
- elapsed process time in seconds.

Each host observation may contain:

- UTC capture timestamp;
- macOS memory-pressure category: `normal`, `warning`, or `critical`;
- swap used in bytes;
- aggregate data-quality counts and fixed issue codes.

## Never collected

StillMac does not collect or retain:

- command lines or arguments;
- environment variables;
- full executable or workspace paths;
- usernames;
- unrelated filenames or file contents;
- clipboard or browser data;
- cookies, profiles, sessions, or browsing history;
- agent conversations or chat content;
- tokens, API keys, passwords, or credentials;
- cache contents or port data;
- device identifiers or analytics identifiers.

Native command output is parsed in memory and discarded. Native errors are not copied into stored state or user-visible errors.

## Storage and retention

The default data directory is:

```text
$HOME/Library/Application Support/StillMac
```

A caller may select another directory with `--data-dir`. StillMac uses private directory/file permissions where practical and rejects unsafe symbolic-link or non-regular storage objects.

Immutable history is bounded to the newest 672 samples, a maximum age of 14 days relative to the newest valid sample, 2 MiB per history sample, and 128 MiB total encoded history. Retention is enforced only during accepted storage operations.

## Sharing

Reports are emitted to standard output. StillMac does not transmit them. The user or calling tool controls any later copying or sharing.

## Verification

Privacy behaviour is enforced through hostile synthetic fixtures, strict schemas, unknown-field rejection, path-free errors, storage permission tests, and binary/state/report scans. See [docs/V0.1-TRACER-CONTRACT.md](docs/V0.1-TRACER-CONTRACT.md) for the normative contract.

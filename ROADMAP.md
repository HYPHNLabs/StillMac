# Roadmap

This roadmap is directional. Items are not release promises until separately specified, implemented, reviewed, and verified.

## Current beta candidate

- Read-only process and memory observations.
- Private bounded local history.
- Seven-day coverage status.
- Preliminary local reports.

## Before a private remote push

- Complete the bounded 12-hour release-candidate soak.
- Pass final source, state, report, and binary privacy scans.
- Pass independent specification and security/code-quality review.
- Confirm exact repository owner, visibility, and included history.

## Before any public beta

- Decide legal rights holder and licence.
- Establish a private security-reporting route.
- Pass CI on the claimed macOS version and architecture matrix.
- Verify a tagged archive, checksums, and provenance.
- Test from a clean account using only release documentation.
- Maintain the separately reviewed lifecycle contract: distribution scripts now exist locally, but remote release activation remains pending.
- Obtain explicit publication approval.

## Later, after read-only evidence

- Improve multi-sample aggregation and report interpretation.
- Evaluate a user-consented scheduler and lifecycle controls.
- Evaluate an Agent Skill as a thin interface to the same deterministic binary.
- Consider ports and cache metadata only through new privacy contracts.

## Explicitly deferred

Process termination, restart, deletion, cleanup, optimisation, and policy-driven actions require dry-run, protected-resource rules, current-state revalidation, rollback/quarantine, action logs, and separate approval. They are not part of v0.1.

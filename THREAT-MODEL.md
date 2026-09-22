# Threat model

## Scope

This model covers baseline commands, the developer cleanup commands `scan`, `explain`, `plan`, `apply`, `clean`, `protect`, and `history`, and the direct `v0.1.1` GitHub Release installer. Homebrew publication remains inactive. Local Agent Skill preparation and the separate M1/M2 source contracts are described below.

## Protected assets

- command arguments, credentials, private paths, workspace identities, and unrelated filenames;
- integrity of baseline history, plans, target registries, protection records, and receipts;
- exact cache roots and all unrelated user data;
- truthful decisions, action results, and reclaimed-space claims.

## Trust boundaries

Native command output, selected HOME/scope/data paths, filesystem objects, persisted JSON, confirmation input, and output consumers are untrusted. The user account and kernel are trusted. Malicious concurrent same-UID replacement of the Go executable pathname or the logical GOCACHE root is explicitly out of scope and remains residual risk; no userspace check can atomically bind either pathname to `execve` or Go's subsequent pathname resolution.

## Threats and controls

### Path disclosure

Git and cache paths could expose usernames or projects. Public candidates use opaque stable IDs and generic labels. Absolute targets and the actual host ID exist only in a `0600` target registry. Errors are generic and path-free.

### Plan or target substitution

A plan could be edited to point at arbitrary data. The plan hash binds complete public content and a target-registry hash. The private registry binds actual host, exact path, family, rule, root kind, decision, fingerprint, device, and inode. Unknown schemas, IDs, entries, or fields fail closed.

### Time-of-check to time-of-use change

A cache, parent, protection state, rule, decision, cache fingerprint, or Go executable binding could change after planning. Apply revalidates them immediately before the owner-native action, reducing accidental or stale changes. These checks are not atomic with `execve` or Go's pathname resolution and do not defend against a hostile same-UID race. Replacement with identical content still fails inode identity when observed by the checks. A difference yields a `blocked_changed` receipt and state exit.

### Symlink and filesystem redirection

Selected state or target parents could redirect inspection. StillMac rejects symlinked nearest state parents, unsafe state objects and modes, target parents below HOME that are links or non-directories, non-directory roots, and target identity changes. Apply passes no target path argument to Go.

### Over-broad action

An inventory rule could become an executable action. Only verified Go build cache rows can map to `owner-native-go-clean-cache`. Within the cache command namespace, Homebrew, Git, and Codex actions are `none`. There is no shell, recursive removal, force flag, raw plan path execution, arbitrary target, or cache-root rename.

### False free-space claim

The same exact scanner measures the Go cache before and after a successful owner action. Receipts set `moved_bytes` to zero and report both `removed_bytes` and `reclaimed_bytes` as `max(before-after, 0)`. Command failure claims neither, even if Go partially changed its cache.

### Accidental approval

Non-TTY `clean` refuses. Interactive clean prints every candidate and exclusion, creates the exact plan, and accepts only `apply PLAN_ID`. Scripted use must separate plan review from apply.

### Partial apply or output failure

Each attempted row gets an atomic receipt with success or failure. Apply returns structured rows and a failure exit when any row fails. Output writer failure is distinct. History remains the source of truth for the measured result.

### Supply chain

The source installer fails closed. The direct release installer is pinned to the reviewed `v0.1.1` manifest digest and verifies the Apple Silicon archive before installation. Homebrew publication remains inactive; source-only Skill preparation is a separate explicitly selected local operation. Signing and notarisation are not claimed.

## Residual risks

- Equivalent-user malware can read or mutate local state and cache data.
- Malicious same-UID concurrent replacement of the Go executable or logical GOCACHE pathname is out of scope and can race the final checks, `execve`, or Go pathname resolution. Fixed absolute executable selection, fingerprints, exact GOCACHE, fixed arguments, and sanitized environment are defense-in-depth, not an atomic guarantee. No cache source-name rename remains.
- Go cache cleaning can increase the cost of later builds while cache entries are rebuilt.
- Git reachability uses locally resolved integration-base evidence; stale or unavailable refs can make inventory conservative or incomplete.
- The public beta is Apple Silicon only and has been executed on macOS 26.5. Other macOS versions are not yet verified runtime claims.

Future end-session automation may scan only. Auto-clean, scheduler state, new roots, or network behaviour requires a new contract and threat review.

## M1/M2 source candidate

Residue inventory adds no cleanup primitive. It measures only supported artifact
families in explicit scopes, uses opaque IDs and bounded private snapshots, and
refuses to derive reductions from missing or incompatible evidence. Logical
shrink is not a measurement of filesystem free space. Metadata worktree listing
uses fixed Git execution with bounded output and time. Status and ancestry probes
refuse configured helpers and partial-clone configuration; no inherited PATH or
Git configuration injection is used.

Retirement has separate registration, target, plan, approval and receipt schemas.
It permits only native Git removal of an explicitly registered user-managed
linked worktree after both owner attestation and fresh machine checks. An
attestation is not machine evidence of inactivity. Main, current, locked,
dirty, untracked, ignored, agent-managed and otherwise unsupported targets
remain blocked. Restoration is a tracked checkout reconstruction, conditional
on the preserved local commit/ref and an unoccupied exact root. The
[retirement contract](docs/WORKTREE-RETIREMENT-CONTRACT.md) describes remaining
same-account race and recovery limits. Fixture tests do not prove live agent
integration or universal Mac compatibility.

Skill preparation validates a version-bound supplied source and claims an
explicit new destination without replacing an existing installation. It is
local-only and installs neither runtime nor hooks. Agent instructions cannot
replace deterministic CLI checks or human approval of the exact action plan.

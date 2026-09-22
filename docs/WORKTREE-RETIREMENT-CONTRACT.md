# Git linked-worktree retirement contract

This is a separate, opt-in contract from the developer-cache contract. It
registers one user-selected Git linked worktree, records machine checks and an
explicit owner release attestation, and can invoke only the native Git
operation `git worktree remove <exact-registered-path>` without `--force`.

The feature does not remove arbitrary paths, run a shell, delete a branch,
prune Git metadata, inspect conversations, read worktree file contents, use the
network, or bypass an owner-managed lifecycle. It does not run against the
current or main worktree. The active action is never automatic and never
selected by an agent.

## Public Go API

The package is `stillmac/internal/retire`. The parent CLI owns prompts and
formatting. The package returns path-free records and fixed error codes.

```go
type Config struct {
	Home         string
	DataDir      string
	HostID       string
	CurrentPath  string
	Now          func() time.Time
	GitRunner    GitRunner
}

type GitRunner func(dir string, args []string, env []string) (GitResult, error)

func New(Config) *Engine

func (e *Engine) Register(RegisterRequest) (Registration, error)
func (e *Engine) Release(ReleaseRequest) (Registration, error)
func (e *Engine) List() ([]Registration, error)
func (e *Engine) Protect(registrationID string) error
func (e *Engine) Unprotect(registrationID string) error
func (e *Engine) Plan(PlanRequest) (Plan, error)
func (e *Engine) Approve(planID string) (Approval, error)
func (e *Engine) Apply(planID string) (ApplyResult, error)
func (e *Engine) Recover(receiptID string) (RecoveryResult, error)
func (e *Engine) History() ([]Receipt, error)
```

`GitRunner` exists for deterministic tests and must not be wired to a shell.
The default runner resolves and verifies an absolute native Git executable,
uses `exec.Command`, disables hooks and optional locks for inspection, removes
inherited global and system Git configuration, refuses terminal prompts, and
does not pass network or user-controlled Git options. The remove command has
the fixed argument shape `worktree remove <registered path>` and never accepts
`--force`.

The public request and result records are:

```go
type RegisterRequest struct {
	Path                 string
	OwnershipAttestation string
}
type ReleaseRequest struct {
	RegistrationID string
	Attestation    string
}
type PlanRequest struct { RegistrationID string }

type Registration struct {
	SchemaVersion string   `json:"schema_version"`
	ID            string   `json:"registration_id"`
	Status        string   `json:"status"`
	Decision      string   `json:"decision"`
	Reasons       []string `json:"reasons"`
	CapturedAt   string   `json:"captured_at"`
	Label         string   `json:"label"`
	HeadCommit    string   `json:"head_commit"`
	Action        string   `json:"action"`
	Recovery      string   `json:"recovery"`
	Protected     bool     `json:"protected"`
}

type Plan struct {
	SchemaVersion      string       `json:"schema_version"`
	PlanID             string       `json:"plan_id"`
	PlanHash           string       `json:"plan_hash"`
	RegistrationID     string       `json:"registration_id"`
	ExpiresAt          string       `json:"expires_at"`
	HostBinding        string       `json:"host_binding"`
	RuleSet            string       `json:"rule_set"`
	TargetRegistryHash string       `json:"target_registry_hash"`
	Registration       Registration `json:"registration"`
	ApprovalRequired   bool         `json:"approval_required"`
}

type Approval struct {
	SchemaVersion  string `json:"schema_version"`
	PlanID         string `json:"plan_id"`
	PlanHash       string `json:"plan_hash"`
	ExpiresAt      string `json:"expires_at"`
	ApprovedAt     string `json:"approved_at"`
}

type Receipt struct {
	SchemaVersion  string   `json:"schema_version"`
	ReceiptID      string   `json:"receipt_id"`
	Operation      string   `json:"operation"`
	RegistrationID string   `json:"registration_id"`
	PlanID         string   `json:"plan_id"`
	PlanHash       string   `json:"plan_hash"`
	Decision       string   `json:"decision"`
	Result         string   `json:"result"`
	Reasons        []string `json:"reasons"`
	RetainedCommit string   `json:"retained_commit"`
	Recovery       string   `json:"recovery"`
	Timestamp      string   `json:"timestamp"`
}

type ApplyResult struct {
	SchemaVersion string  `json:"schema_version"`
	PlanID        string  `json:"plan_id"`
	PlanHash      string  `json:"plan_hash"`
	Receipt       Receipt `json:"receipt"`
}

type RecoveryResult struct {
	SchemaVersion          string `json:"schema_version"`
	ReceiptID              string `json:"receipt_id"`
	Result                 string `json:"result"`
	RetainedCommit         string `json:"retained_commit"`
	RestoredIgnoredUntracked bool `json:"restored_ignored_untracked"`
	Message                string `json:"message"`
	Timestamp              string `json:"timestamp"`
}
```

The schema identifier is `stillmac.retire.v1`. It is independent from
`stillmac.cleanup.v1` and the baseline schemas. The ruleset identifier is
`worktree-retirement.v1`.

## Registration and owner release

`Register` accepts one absolute, cleaned path and the exact constant
`owner-confirmed-user-managed-worktree` in `OwnershipAttestation`. It must identify an existing
linked worktree in the output of native `git worktree list --porcelain -z`.
The main worktree, the process current worktree, locked or prunable entries,
agent roots, and unsafe paths are retained as blocked registrations so the
owner can see the fixed reason. A registration ID is an opaque hash of the
exact private path. The exact path and repository root are stored only in the
private registration record.

Agent roots are always blocked when the selected path is below `$HOME/.codex`,
`$HOME/.claude`, or `$HOME/.hermes`. Symlink aliases and custom agent roots are
not treated as user-managed merely because their path looks ordinary. The
explicit ownership attestation is required for every registration, while known
managed roots remain blocked. This package does not traverse those roots or
infer that an agent is inactive.

Machine checks are separate from owner activity. A clean tree, an old file,
an apparently merged branch, or an absent process is not evidence that a
session has ended. `Release` requires the exact constant
`owner-confirmed-no-active-session` in `ReleaseRequest.Attestation`. This is a
user-managed trust signal, not proof supplied by StillMac. A successful release
attestation expires 30 minutes after its recorded UTC time. It is discarded by
revalidation when the target changes. The owner must provide a new attestation
after expiry or any failed check.

`List` rechecks each registered exact target. `Protect` and `Unprotect` write
project-specific protection records in this package's private state. A
protected registration cannot be planned, approved, applied, or recovered
until the owner explicitly unprotects it.

## Machine gates

The selected target must pass every gate below at registration, release, plan,
approval, and immediately before action:

1. It is the exact linked worktree previously registered, not the main or
   current worktree.
2. It is not locked, prunable, or under an agent root.
3. Every existing parent and the target root is a real directory with trusted
   ownership and no group or other write permission. The target root and every
   entry below it must be a regular file or directory. Symlinks, devices,
   FIFOs, sockets, and other special files block the target. Names and file
   contents are not retained or emitted.
4. Git status is empty with untracked and ignored material included. Tracked,
   staged, untracked, and ignored changes each block. The index identity,
   `HEAD`, branch or retained reference, lock state, and worktree-list record
   are bound privately and rechecked after approval.
5. The worktree contains no submodule entry in either its index or retained
   `HEAD` tree.
6. The current `HEAD` commit is preserved by a local branch or tag whose exact
   object ID is retained. A detached commit with no local preservation
   reference is blocked. A changed or deleted preservation reference blocks.
7. The owner release attestation exists and has not expired.

No gate treats a clean or merged worktree as evidence of inactivity. If the
owner cannot supply the release attestation, the executable decision remains
blocked and the required signal is an owner-managed confirmation that no
interactive or automated session still owns the worktree.

## Plan, approval, and apply

`Plan` snapshots one selected registration, even when the snapshot is blocked,
so blocked state is durable and inspectable. A plan is immutable, expires after
15 minutes, binds an opaque host ID, and hashes one private target registry.
The target registry contains only the selected target's exact path, repository
root, commit, index identity, preservation reference, release attestation, and
machine snapshot. Public plans never contain those paths.

`Approve` accepts only the exact plan ID and records the plan hash and expiry.
It is an explicit user or caller action, but it is not a security boundary
against an agent that already has permission to invoke this package. Approval
of a blocked or expired plan fails.

`Apply` accepts only a plan ID. It requires the matching approval, then
revalidates the plan, target registry, host, expiry, protection, path identity,
worktree-list record, lock state, index, ref, status including ignored files,
submodules, preserved commit, and short-lived release attestation. Any change
creates a path-free `blocked_changed` receipt and performs no action.

The only successful mutation is a direct, absolute native Git invocation of:

```text
git worktree remove <exact registered path>
```

The implementation does not pass `--force`, remove a directory, delete a
branch, prune, run a shell, or accept a user path at apply time. A non-zero Git
result is recorded as `native_action_failed`; its final filesystem state is
not guessed and the owner must inspect it before retrying.

## Receipts and recovery

Successful removal writes a private atomic receipt before returning success.
The path-free receipt retains the exact `HEAD` commit reference and states that
recovery can reconstruct a tracked checkout only. Failed revalidation and
native Git failures also write receipts when the private state remains
writable. State and receipts use `0700` directories and `0600` regular JSON,
reject symlinks, non-regular objects, unknown entries, unsafe permissions,
unknown fields, malformed hashes, and ID traversal.

`Recover` accepts only a receipt ID for a successful retirement. It verifies
the exact registered root is absent, the parent chain is safe, the preserved
commit still exists locally, the preservation reference still points to that
commit, and the target is not already registered. It then uses direct native
Git `worktree add --detach <exact root> <retained commit>` with no force flag.
It never deletes an occupant. Recovery is a tracked checkout reconstruction,
not an exact backup: ignored and untracked files cannot be restored, and the
original in-progress session state is not reconstructed. Recovery itself is
recorded with a path-free receipt. A failed recovery does not claim that the
target exists or that the original files were restored.

## Git execution boundary

Inspection and mutation use direct process execution with no shell. The
environment disables system and global Git configuration, terminal prompts,
pager/editor use, and optional hooks or file-system monitors. Before any status
or checkout operation, the local and worktree Git configuration is inspected
with includes disabled. Any filter clean, smudge, or process command,
`core.fsmonitor`, `core.hooksPath`, external attributes file, include or
worktree-configuration marker, partial-clone or promisor setting blocks the
target. This prevents Git LFS, lazy fetch, or another configured helper from
executing. `GIT_NO_LAZY_FETCH=1` is also set for every native command. Local
repository metadata is still read by Git because it is required to identify
the linked worktree. Native command output, errors, filenames, repository
paths, and file contents are discarded and never copied into public output.

The command choices follow the primary Git documentation for linked-worktree
listing, removal, locking, and recovery:

- [git-worktree documentation](https://git-scm.com/docs/git-worktree)
- [Git environment variables](https://git-scm.com/docs/git)
- [Git hooks](https://git-scm.com/docs/githooks)

## Non-goals and limitations

- No activity or inactivity proof is inferred from Git state, timestamps,
  process absence, branch merge state, or clean files.
- An owner release attestation is a trust boundary, not a detector and not a
  defence against a malicious same-account process acting concurrently.
- User changes between the final checks and Git pathname resolution are not
  made atomic by this package. The native Git command remains the owner-native
  authority and no force override is available.
- Git may fail after an action begins. A failed command is reported as
  indeterminate and is never converted into a success claim.
- Recovery cannot restore ignored or untracked files, original process state,
  or a perfect copy of the removed checkout.
- There is no scheduler, automatic end-session action, network operation,
  branch deletion, repository pruning, direct filesystem deletion, or agent
  lifecycle integration.

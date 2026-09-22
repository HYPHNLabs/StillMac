# Architecture

StillMac is one standard-library Go CLI. The released v0.1.1 has two bounded verticals; the M1/M2 source candidate adds explicit residue reports and separately contracted worktree retirement.

```text
fixed process and memory probes -> allowlisted observation -> private samples -> status/report

exact cache and Git scan -> path-free candidates -> immutable plan + private targets
                                                    -> revalidation -> owner-native Go action + receipt
```

## Packages

- `cmd/stillmac`: process entry and exit status.
- `internal/cli`: parsing, text/JSON output, TTY confirmation, and injected dependencies.
- `internal/observe`, `state`, `baseline`, `report`, `doctor`: existing baseline behaviour.
- `internal/cleanup`: exact-root rules, Git inventory, fingerprints, plans, protection, private state, apply, and history.

## Cleanup dependency boundary

`cli.Dependencies` can inject clock, HOME, host ID, stdin, TTY status, Git runner, Go cleaner, and a cleanup service factory. Production defaults use the real user HOME, hostname, clock, stdin, terminal mode, fixed Git executable, and verified owner-native Go tool discovery.

The scanner emits no target path. Planning reconstructs targets only from built-in family rules and stores them in a private registry. The public plan binds the registry hash. Apply never trusts a public path or derives scope from the data directory.

## Action boundary

Only `go-build-cache.v1` can map to `owner-native-go-clean-cache`. Within the cache namespace, Homebrew, Codex, and Git always map to `none`. Apply validates the exact rule, root identity, private executable binding, fixed arguments, and sanitized environment, then invokes the absolute Go executable without a shell. These checks are defense-in-depth and not atomic with `execve` or Go pathname resolution; same-UID hostile concurrent replacement is out of scope. No cache filesystem rename or removal primitive exists in the cleanup package; its sole `os.Rename` publishes private JSON state atomically.

## Storage

Baseline history remains under `samples/`. Cleanup state is isolated under `cleanup/` with `plans`, `targets`, `protected`, and `receipts`. JSON publication uses private temporary files, sync, close, and atomic rename.

The tracer contract and cleanup contract are normative. Distribution scripts remain outside the Go runtime.

## Source candidate additions

- `internal/residue`: explicit allowlisted artifact metadata, path-free snapshots,
  compatible growth comparisons and user-invoked session reports. It delegates
  legacy exact-cache decisions to `internal/cleanup`; report limits state which
  part of the scan they bound.
- `internal/retire`: explicit ownership/session attestations, registered linked
  worktree identity, immutable plans, approval, native Git removal and tracked
  checkout reconstruction. It never routes through cache `apply`.
- `capabilities`: a static source contract identifier for agents. It does not
  assert a published release, successful installation or live compatibility.
- Local Agent Skill preparation remains outside the Go runtime.

The [residue](RESIDUE-CONTRACT.md) and
[retirement](WORKTREE-RETIREMENT-CONTRACT.md) contracts govern these additions.
See [acceptance gates](M1-M2-ACCEPTANCE.md) for the distinction between fixture
validation, field use and release readiness.

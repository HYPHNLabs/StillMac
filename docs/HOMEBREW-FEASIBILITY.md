# Homebrew Feasibility

## Decision

As at 2026-09-07, Homebrew remains inventory-only in StillMac. The candidate
decision stays `REVIEW` with action `none`. StillMac must not invoke
`brew cleanup`, `brew bundle cleanup`, Homebrew Ruby internals, or any shell
deletion command.

The public Homebrew interface does not provide enough evidence to bind a
cleanup action to StillMac's exact-root immutable plan contract. Adding a
Homebrew action now would weaken the product boundary.

## Reviewed semantics

The official `brew cleanup` command:

- removes stale lock files and outdated downloads for formulae and casks;
- removes old versions of installed formulae;
- accepts formula or cask names as optional selectors;
- supports `--dry-run` to show what would be removed;
- supports age pruning and `--scrub`, which changes the download-cache scope.

These are Homebrew package and cache semantics, not a command that accepts an
absolute target root and an immutable target set. The official manpage also
documents a direct recursive removal example for clearing the complete cache.
That example is outside StillMac's contract.

`brew --cache` displays the configured Homebrew download cache, and the
manpage documents `HOMEBREW_CACHE` as the cache configuration. This can help
StillMac identify a bounded inventory root, but setting an environment
variable does not prove that every `brew cleanup` mutation is confined to
that root.

Homebrew's published Ruby API exposes a `cache:` parameter on
`Homebrew::Cleanup#initialize`. The same official API page marks that API as
private, restricted to the Homebrew/brew repository, and subject to removal.
StillMac cannot use it as a stable third-party action interface.

The [Homebrew brew(1) manpage](https://docs.brew.sh/Manpage.html) and the
[Homebrew Cleanup Ruby API reference](https://docs.brew.sh/rubydoc/Homebrew/Cleanup.html)
were accessed on 2026-09-07. The local `brew help cleanup` output was also
checked on that date from Homebrew 6.0.19. The local check did not run a
cleanup command.

## Exact-root comparison

StillMac's cleanup contract requires all of the following:

1. A candidate identity and rule version that bind to one exact private root.
2. An immutable plan hash and a private target registry.
3. Revalidation of ownership, device, inode, type, fingerprint, and action
   configuration immediately before an action.
4. An owner-native action that cannot select or mutate anything outside the
   bound root.
5. Structured before and after measurements and a receipt for each attempted
   target.
6. Fail-closed behaviour for expiry, target changes, protection, malformed
   state, and partial failure.

The public `brew cleanup` documentation establishes dry-run, age, scrub, and
formula/cask selection behaviour. It does not establish an absolute-root
selector, a stable machine-readable target registry, a plan token that binds
preview to apply, or a cache-only mutation guarantee. The conclusion that
the public command cannot currently satisfy the full comparison is an
inference from those documented inputs and outputs, not a claim about
Homebrew's private implementation.

`brew bundle cleanup` is even less suitable. It removes dependencies not
present in a Brewfile and can affect formulae, casks, taps, and other
supported dependency types. It is not an exact cache-root action.

## Narrow future contract

A future Homebrew action may be reconsidered only after a separate contract
and threat review proves a supported, owner-native interface with all of these
properties:

- accepts or provably binds one exact cache root without relying on an
  inherited `PATH` or unreviewed ambient configuration;
- produces a stable, machine-readable dry-run target set with item identity,
  relative location, type, size, and rule version;
- applies exactly that set, or accepts an immutable Homebrew plan token that
  binds preview to apply;
- proves that formula prefixes, installed versions, locks, logs, and other
  cache roots are excluded unless separately selected and contracted;
- supports immediate executable and configuration revalidation;
- produces per-item measured before and after bytes, method, result, and
  partial-failure receipts;
- never needs `sudo`, force, recursive shell removal, a cache-root rename, or
  network access.

An environment override such as `HOMEBREW_CACHE` may be part of a future
proof, but it is not proof on its own. A private Ruby parameter is not an
acceptable substitute for a supported public contract. Until this evidence
exists, keep the Homebrew row inventory-only and exclude it from
`all-safe`.

## Disposable feasibility pilot

No pilot result is claimed here. If a future review is authorised, use only a
disposable local cache root and preview commands:

```
pilot_root=$(mktemp -d)
mkdir -m 700 "$pilot_root/cache"
HOMEBREW_CACHE="$pilot_root/cache" HOMEBREW_NO_AUTO_UPDATE=1 brew --cache
HOMEBREW_CACHE="$pilot_root/cache" HOMEBREW_NO_AUTO_UPDATE=1 brew cleanup --dry-run
```

The pilot must confirm the reported cache root, classify every preview item,
check whether any output refers to a location outside the supplied root, and
verify that the root is unchanged before and after the preview. It must not
run real cleanup, `--prune=all`, `--scrub` as an action, `brew bundle cleanup`,
or any direct deletion. A preview result is evidence about that Homebrew
version and environment only. It is not action approval and cannot make the
current StillMac candidate executable.

Use this redacted feedback shape:

```
pilot_id: [REDACTED]
date_utc: [REDACTED]
brew_version: [REDACTED]
host_os: [REDACTED]
architecture: [REDACTED]
cache_root: [REDACTED]
preview_only: yes
outside_root_observed: yes|no|not-tested
root_changed: yes|no|not-tested
machine_readable_targets: yes|no|not-tested
plan_binding: yes|no|not-tested
unexpected_output: [REDACTED]
evidence_refs: [REDACTED]
```

Do not attach raw Homebrew output containing home paths, usernames, formula
paths, workspace names, credentials, or unrelated filenames.

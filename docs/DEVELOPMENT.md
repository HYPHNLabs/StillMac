# Development

## Requirements

- macOS
- Go 1.23 or later

StillMac uses the Go standard library only.

## Build

```bash
mkdir -p ./bin
go build -trimpath -o ./bin/stillmac ./cmd/stillmac
```

`-trimpath` is mandatory for candidate/release builds.

## Test and static analysis

```bash
gofmt -w .
go test -count=1 -race ./...
go vet ./...
```

## Cross-build

Cross-building proves compilation, not runtime compatibility:

```bash
GOOS=darwin GOARCH=arm64 go build -trimpath -o /tmp/stillmac-darwin-arm64 ./cmd/stillmac
GOOS=darwin GOARCH=amd64 go build -trimpath -o /tmp/stillmac-darwin-amd64 ./cmd/stillmac
```

Do not claim Intel support until the `amd64` artifact has been executed and tested on Intel hardware or an approved equivalent environment.

## Privacy inspection

Inspect the candidate source list and binary before a commit or release. Real private paths, credentials, usernames, internal planning files, and runtime state are prohibited.
The deterministic public residue scan also covers tracked text and generated binaries for workspace paths, private PRD names, Telegram-like routes, and obvious credential assignments.

The tests deliberately contain synthetic hostile values such as fake absolute macOS home paths and credential-shaped strings. Those fixtures are necessary to prove non-persistence and non-disclosure; classify them as fixtures rather than silently deleting them.

## TDD rule

For any production behaviour change:

1. Write one focused behavioural test.
2. Run it and confirm the expected RED failure.
3. Implement the smallest production change.
4. Run the focused test and full suite to confirm GREEN.
5. Refactor only while all tests remain green.

## Local Git boundary

Local commits are allowed for the clean candidate package. Private planning PRDs, build artifacts, runtime state, editor settings, and environment files are ignored. A remote, push, tag, release, licence claim, or publication requires a separate explicit decision.

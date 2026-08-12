## Summary

Describe the behavioural change and why it is needed.

## Contract

- Affected command/schema:
- Compatibility impact:
- Migration impact:

## Privacy and security

- New data collected or stored: none / describe
- Filesystem or native-command impact:
- Threat-model changes:

## Verification

- [ ] Focused test was observed failing before production code
- [ ] `gofmt -w .`
- [ ] `go test -count=1 -race ./...`
- [ ] `go vet ./...`
- [ ] `go build -trimpath -o ./bin/stillmac ./cmd/stillmac`
- [ ] Generated outputs and binary inspected for private residue

## Scope

- [ ] No scheduler, installer, updater, network, telemetry, cleanup, process action, or LLM capability added without a separately approved contract

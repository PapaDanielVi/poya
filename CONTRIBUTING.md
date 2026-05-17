# Contributing to poya

Thank you for your interest in contributing! This document explains how to get started, the project conventions, and how to add new functionality.

## Getting Started

1. Fork the repository
2. Clone your fork: `git clone https://github.com/YOUR_USERNAME/poya.git`
3. Create a branch: `git checkout -b feat/my-feature`
4. Make your changes
5. Run the full test suite: `go test -race ./...`
6. Commit and push: `git commit -m "feat: add my feature" && git push`
7. Open a pull request

### Requirements

- Go 1.26+
- All tests must pass with the race detector: `go test -race ./...`
- `go vet ./...` must report no issues

## Project Structure

```
poya/
├── poya.go              # SDK core: New, Start, Stop, Register, RegisterConfig
├── dcvalue.go           # DcValue[T] — scalar config value
├── dcstruct.go          # DcStruct[T] — struct config value (JSON-decoded)
├── metrics.go           # Metrics interface, realMetrics, noopMetrics
├── poya_test.go         # SDK tests
├── dcvalue_test.go      # DcValue tests
├── dcstruct_test.go     # DcStruct tests
└── provider/
    ├── provider.go      # Provider interface (Get, Watch, Close)
    ├── etcd/            # etcd provider
    ├── redis/           # Redis provider
    └── vault/           # HashiCorp Vault provider
```

## How to Add a New Provider

1. Create a new directory under `provider/` (e.g., `provider/consul/`)
2. Implement the `Provider` interface from `provider/provider.go`:

```go
type Provider interface {
    Get(ctx context.Context, key string) (string, error)
    Watch(ctx context.Context, key string, onChange func(key string, value string)) error
    Close() error
}
```

3. Add a `Config` struct for provider-specific options
4. Add a constructor (`New`) that returns your provider
5. Add a compile-time interface check: `var _ provider.Provider = (*Provider)(nil)`
6. Add integration tests (use build tags: `//go:build integration`)
7. Update the README with setup instructions

**Watch strategy guidelines:**
- Prefer event-driven watches (like etcd's native Watch API) over polling
- If polling is necessary, make the interval configurable with a sensible default (5s)
- The `Watch` method must block until the context is cancelled
- Only call `onChange` when the value actually changes (deduplicate)

## How to Add a New Value Type

1. Create a new file (e.g., `dcyaml.go`) with a generic type `DcYaml[T]`
2. Follow the `DcValue[T]` / `DcStruct[T]` pattern:
   - `NewDcYaml[T](defaultValue T) *DcYaml[T]` — constructor
   - `Get() T` — the only public method
   - `InternalKey(key string)` — called by SDK during registration
   - `InternalDefault() T` — called by SDK during registration
   - `InternalAtomic() *atomic.Value` — called by SDK during registration
   - One decode method (e.g., `InternalSetYaml([]byte) error`) — called by sync loop
3. Add detection in `poya.go`:
   - `isDcYaml(v reflect.Value) bool` — detect the type via method signatures
   - `handleDcYaml(s *SDK, fv reflect.Value, fullKey string)` — extract and register
   - Add a new `entryKind` and update `updateEntry` / `updateYamlEntry`
4. Add tests in `dcyaml_test.go`
5. Update the README

**Important:** All SDK-internal methods must be **exported** (capitalized). Unexported methods on generic types are invisible to Go's `reflect.MethodByName` even within the same package.

## Code Style

- Match the existing code style exactly
- No comments on obvious code — only explain non-obvious decisions
- Use `sync/atomic` for lock-free reads; use `sync.RWMutex` for map protection
- Keep the hot path (`Get()`) allocation-free
- Use the Metrics interface for telemetry — never add if-checks for metrics enable/disable

## Testing Conventions

- Unit tests use `mockProvider` from `poya_test.go` — reuse it
- Test concurrent access with `-race` — data races are blocking issues
- Test both the enabled and disabled paths for optional features
- Table-driven tests are preferred for parsing/decoding logic
- Flaky tests are blocking issues — if a test fails intermittently, fix it before submitting

## Pull Request Process

1. Describe what you changed and why in the PR description
2. Reference any related issues
3. Ensure CI passes (tests + race detector + vet)
4. Keep PRs focused — one feature or fix per PR
5. Update documentation (README, CLAUDE.md) if your change affects the public API

## Code Review

All submissions require review. We look for:

- Correctness — does it work, including edge cases?
- Race safety — does it pass `-race`?
- Simplicity — is this the minimum code to solve the problem?
- Test coverage — are new code paths tested?
- Documentation — are new types and functions documented?

## License

By contributing, you agree that your contributions will be licensed under the MIT License.

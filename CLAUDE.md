# Poya SDK — AI Coding Guidelines

## Project Purpose

Poya is a dynamic config SDK for Go. Developers register typed config values (`DcValue[T]`), choose a provider (etcd, Redis, Vault, MySQL, PostgreSQL, file), and the SDK keeps values in sync in the background. Developers only call `Get()`.

## Architecture

```
DcValue[T]  → unified type: scalars parsed via type switch, structs JSON-decoded
entry       → internal type-erased wrapper holding atomic.Value + entryKind
Metrics     → interface with Prometheus/otel backends and NoopMetrics stub
Logger      → interface with Debug/Info/Warn/Error (slog-based default, noop stub)
Provider    → interface with etcd (prefix watch), Redis/Vault/MySQL/PostgreSQL (batch poll), File (fsnotify)
```

Key types:
- `DcValue[T]` in `dcvalue.go` — single generic type for both scalars and structs. Uses `reflect.TypeOf` at construction to determine kind. Has `InternalKind()`, `InternalSetJSON()` for struct handling.
- `entry` in `poya.go` — has `kind entryKind` (`entryKindScalar` or `entryKindStruct`) determining how raw provider values are decoded
- `Metrics` interface in `metrics/metrics.go` — implementations in `metrics/prometheus/`, `metrics/otel/`,
- `Logger` interface in `logger/logger.go` — structured logging with slog-based default and noop stub

## 1. Think Before Coding

**Don't assume. Don't hide confusion. Surface tradeoffs.**

Before implementing:
- State your assumptions explicitly. If uncertain, ask.
- If multiple interpretations exist, present them — don't pick silently.
- If a simpler approach exists, say so. Push back when warranted.
- If something is unclear, stop. Name what's confusing. Ask.

## 2. Simplicity First

**Minimum code that solves the problem. Nothing speculative.**

- No features beyond what was asked.
- No abstractions for single-use code.
- No "flexibility" or "configurability" that wasn't requested.
- No error handling for impossible scenarios.
- If you write 200 lines and it could be 50, rewrite it.

Ask yourself: "Would a senior engineer say this be overcomplicated?" If yes, simplify.

## 3. Surgical Changes

**Touch only what you must. Clean up only your own mess.**

When editing existing code:
- Don't "improve" adjacent code, comments, or formatting.
- Don't refactor things that aren't broken.
- Match existing style, even if you'd do it differently.
- If you notice unrelated dead code, mention it — don't delete it.

When your changes create orphans:
- Remove imports/variables/functions that YOUR changes made unused.
- Don't remove pre-existing dead code unless asked.

The test: Every changed line should trace directly to the user's request.

## 4. Goal-Driven Execution

**Define success criteria. Loop until verified.**

Transform tasks into verifiable goals:
- "Add validation" → "Write tests for invalid inputs, then make them pass"
- "Fix the bug" → "Write a test that reproduces it, then make it pass"
- "Refactor X" → "Ensure tests pass before and after"

For multi-step tasks, state a brief plan:
```
1. [Step] → verify: [check]
2. [Step] → verify: [check]
3. [Step] → verify: [check]
```

Strong success criteria let you loop independently. Weak criteria ("make it work") require constant clarification.

## Lessons Learned (Go + Generics)

### Generics + Reflection
- **Unexported methods on generic types are invisible via reflection**, even in the same package. Fix: use exported method names (e.g., `InternalKey` instead of `setKey`).
- **`atomic.Value` requires consistent types**. When JSON-decoding structs via `any`, `json.Unmarshal` into `*any` produces `map[string]any`, not the original struct. Fix: use `reflect.New(reflect.TypeOf(default))` to allocate the correct concrete type before unmarshaling.
- **Struct fields passed by value to `interface{}` are not addressable** via reflection. `RegisterConfig` must take a pointer so `fv.CanAddr()` returns true for fields.
- **Detecting struct vs scalar vs array generic types** is done at construction time via `reflect.TypeOf(defaultValue).Kind()` — `reflect.Struct` for structs, `reflect.Slice` for arrays, everything else is scalar. This is more reliable than checking method signatures at registration time.

### DcStruct Merge into DcValue
- **A single `DcValue[T]` handles scalars, structs, and arrays**. The kind is determined once at construction via `reflect.TypeOf` and stored in the `kind` field. This simplifies the entire registration pipeline — no need for separate types or registration functions.
`- **The `entryKind` enum in `poya.go` cleanly dispatches decoding**. `updateEntry` switches on `entry.kind` to call `updateScalarEntry` (type switch via `parseValue`), `updateStructEntry` (JSON unmarshal via `reflect.New`), or `updateArrayEntry` (JSON unmarshal into a new slice). Three kinds: `entryKindScalar`, `entryKindStruct`, `entryKindArray`.


### Array Values
- **Slice types (`[]string`, `[]int`, `[]float64`, etc.) are supported as a first-class kind**. `NewDcValue` detects them via `reflect.TypeOf(default).Kind() == reflect.Slice` and sets `entryKindArray`.
- **`updateArrayEntry` uses `json.Unmarshal` into `reflect.New(reflect.TypeOf(default))`**, same pattern as structs but producing a slice. The provider value must be a JSON array (e.g. `["a","b"]` or `[1,2,3]`).
- **`parseValue` does not handle array types** — they bypass the scalar type-switch entirely and go through `updateArrayEntry` which does JSON decoding. This means arrays always expect JSON-formatted provider values, never plain comma-separated strings.
- **Any element type works** as long as `encoding/json` can unmarshal it: `[]string`, `[]int`, `[]bool`, `[]float64`, even `[]MyStruct`.
- **`InternalSetJSON` is shared between struct and array kinds** — both use `reflect.New` + `json.Unmarshal` + `rv.Elem().Interface()` to store the decoded value.
- **`SetDefaultAndValue` (used by decode hooks) also detects slice kinds** so hooks can initialize array-typed DcValues in-place.
### Provider Watch Pattern
- **The SDK launches one goroutine per provider, not per key.** `Start()` collects all registered keys into a slice and passes them to `provider.Watch(ctx, keys, onChange)`. Each provider decides the most efficient strategy internally.
- **etcd uses a single prefix watch.** It computes the longest common prefix from all keys and does one `clientv3.Watch(ctx, prefix, WithPrefix())` call. Key names are extracted from event `Kv.Key`.
- **Polling providers batch-fetch all keys per cycle.** Redis uses `MGET`; MySQL/PostgreSQL use `SELECT ... WHERE key IN (...)`. One goroutine iterates all keys per tick, comparing against a `lastValues` map.
- **Polling providers should silently skip errors** in the watch loop (`continue` on error) rather than returning, since transient failures are expected and the next tick will retry.
- **SQL providers use a `Repository` interface** with `Get(ctx, key)` and `GetAll(ctx, keys)` methods so users can inject custom query logic for non-standard table schemas. The `DefaultRepository` handles simple key-value tables.
- **File provider uses fsnotify** (fsevents on macOS, inotify on Linux). Watches both the file and its directory to handle atomic writes (rename). On change, re-reads and parses the file, then calls `onChange` for all registered keys. Supports flat JSON and YAML key:value formats (not nested).

### Logger Interface Design
- **Follow the sugared-logger pattern** (`Debug(msg string, keysAndValues ...any)`) rather than structured `slog.With(...)` style. This matches idiomatic Go logging APIs (zap sugared, klog).
- **Always provide a noop stub** (`noopLogger`) for the disabled case, same pattern as `NoopMetrics`. This eliminates nil-checks and if-checks at call sites.

### Metrics Package Organization
- **Split the Metrics interface from its implementations**. The interface lives in `metrics/metrics.go` (package `metrics`), while implementations live in `metrics/prometheus/`, `metrics/otel/`. This keeps the interface importable without pulling in backend dependencies.

### Struct Tag Parsing
- **The `poyaTag` struct uses `key` and `prefix` fields** parsed from comma-separated tag values (`poya:"key=host,prefix=db"`). Fields without any tag default to their lowercased field name.

### Concurrent Agent Pitfalls
- **Multiple agents editing the same file will conflict and overwrite each other's changes**. When coordinating many parallel tasks, agents must not touch the same files. If they do, the last writer wins and earlier changes are lost. Plan agent scopes carefully to avoid overlap.

### time.Duration Support
- **`time.Duration` is supported as a scalar type** via `parseValue`'s `time.ParseDuration(raw)` case. `NewDcValue(time.Duration(30*time.Second))` sets kind to `EntryKindScalar` (since `time.Duration` is `int64`). Provider values must be valid duration strings like `"30s"`, `"1m"`, `"500ms"`.

### Exported EntryKind
- **`entryKind` was exported to `EntryKind`** to fix `revive` lint error about unexported return types on `InternalKind()`. Constants renamed to `EntryKindScalar`, `EntryKindStruct`, `EntryKindArray`. All references updated across `poya.go`, `dcvalue.go`, and test files.

### Lint Fixes (golangci-lint)
- **`errcheck` fixes**: Added `//nolint:errcheck` to deferred `Close()` calls where errors are intentionally ignored (common pattern for cleanup).
- **`funlen` fix**: Refactored `parseValue` into `parseSigned`/`parseUnsigned` helpers to reduce statement count below 50.
- **`gocognit` fixes**: Extracted `calcNestedPrefix` helper from `registerConfig` and `initialFetch`/`pollOnce` helpers from Redis `Watch` to reduce cognitive complexity.
- **`govet` fix**: Avoided `else if` with short variable declarations (not supported in Go 1.21), refactored to separate `if` blocks with `continue`.
- **`modernize` fix**: Added `//nolint:modernize` to goroutine creation (standard `sync.WaitGroup` pattern is acceptable).
- **`godoclint` fix**: Updated doc comments to use `[atomic.Value]` link format.

### Docker Image Updates
- **Stable versions applied to all docker-compose files**:
	- etcd: `quay.io/coreos/etcd:v3.6.11`
	- MariaDB (MySQL example): `mariadb:10`
	- PostgreSQL: `postgres:18.4`
	- Redis: `redis:8.2.6`
	- Vault: `hashicorp/vault:2.0`

# Poya SDK — AI Coding Guidelines

## Project Purpose

Poya is a dynamic config SDK for Go. Developers register typed config values (`DcValue[T]`, `DcStruct[T]`), choose a provider (etcd, Redis, Vault), and the SDK keeps values in sync in the background. Developers only call `Get()`.

## Architecture

```
DcValue[T]  → scalar values (string, int, bool, etc.), parsed via type switch
DcStruct[T] → complex structs, JSON-decoded from provider
entry       → internal type-erased wrapper holding atomic.Value + entryKind
Metrics     → interface with realMetrics (Prometheus) and noopMetrics (stub)
Provider    → interface with etcd (watch), Redis (poll), Vault (poll)
```

Key types:
- `DcValue[T]` in `dcvalue.go` — only `Get()` is public; `InternalKey`, `InternalSet`, `InternalDefault`, `InternalAtomic` are SDK-internal
- `DcStruct[T]` in `dcstruct.go` — same pattern but uses `InternalSetJSON` instead of `InternalSet`
- `entry` in `poya.go` — has `kind entryKind` (`entryKindScalar` or `entryKindStruct`) determining how raw provider values are decoded
- `Metrics` interface in `metrics.go` — `realMetrics` uses a per-instance Prometheus registry (not global) to avoid duplicate registration panes

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

Ask yourself: "Would a senior engineer say this is overcomplicated?" If yes, simplify.

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
- **Unexported methods on generic types are invisible via reflection**, even in the same package. `MethodByName("dcValue")` on `reflect.ValueOf(dcValue.Instance().Interface())` returns invalid for unexported methods of `DcValue[T]`. Fix: use exported method names (e.g., `InternalKey` instead of `setKey`).
- **`atomic.Value` requires consistent types**. Stores the first type; storing a different type panics at runtime. When JSON-decoding structs via `any`, `json.Unmarshal` into `*any` produces `map[string]any`, not the original struct. Fix: use `reflect.New(reflect.TypeOf(default))` to allocate the correct concrete type before unmarshaling.
- **Struct fields passed by value to `interface{}` are not addressable** via reflection. `RegisterConfig` must take a pointer so `fv.CanAddr()` returns true for fields.

### Concurrency
- Reading `len(map)` without holding the mutex that protects the map triggers the race detector. Always access shared state under the appropriate lock.
- `prometheus.MustRegister` to the global registry panics on duplicate registration. Use `prometheus.NewRegistry()` per SDK instance.

### Metrics Pattern
- Use an interface (`Metrics`) with a no-op stub (`noopMetrics`) for the disabled case. This avoids if-checks in hot paths — the interface dispatch is cheaper and cleaner.

# sweep

macOS disk-analyzer/cleaner TUI. Go 1.26, module `github.com/rahul-17280/sweep`.
Charm v2 (Bubble Tea, Lip Gloss, Bubbles) for the TUI; `charlievieth/fastwalk` for scanning.

## Commands
Build:  `go build ./...`
Run:    `go run ./cmd/sweep .`
Vet:    `go vet ./...`
Format: `gofmt -w .`   — `gofmt -l .` must print nothing before you finish.
Test:   `go test -race ./...`

## Code style
- **Comments: none by default.** Add one only where the code is genuinely convoluted or the
  "why" is non-obvious (a platform quirk, an invariant, a concurrency rule). Never restate what
  the code already says — no file headers, no per-function narration, no field name echoes.
- Errors: wrap with `fmt.Errorf("doing x: %w", err)`. No panics for expected failures.
- Small consumer-side interfaces. `context.Context` is always the first parameter.
- No global mutable state. Plain structs over getters. Keep functions short.
- `internal/{scan,tree,format,volume}` must not import Bubble Tea; only `internal/ui` may.

## Concurrency
- Anything reachable from multiple goroutines must be race-free under `go test -race`.
- One goroutine owns shared state; pass data over a bounded channel, never share a pointer.
- Every channel send selects on `ctx.Done()` so cancellation can never deadlock a producer.

## Testing
- **End-to-end over unit tests.** Exercise public entry points (`scan.Measure`,
  `scan.ScanTree`, the CLI) — not private helpers.
- No trivial tests: skip getters, constants, and "it compiles". Test what can actually break —
  hardlink dedup, size accounting, cancellation, tree rollup, path handling.
- Real fixtures via `t.TempDir()`; cross-check totals against `du` where it applies.
  Gate slow or system-dependent tests behind `testing.Short()`.

## Boundaries
- macOS-first. OS-specific code goes in `*_darwin.go` / `*_other.go` behind build tags.
- Do not follow symlinks outside the scan root or cross filesystems unless explicitly asked.
- Deletions move to `~/.Trash`; never hard-delete without an explicit flag.
- Do not commit unless asked.

## Git
Conventional commits: `feat:`, `fix:`, `refactor:`, `docs:`, `chore:`.

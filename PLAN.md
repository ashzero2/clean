# Plan: `sweep` — a Charm v2 TUI disk analyzer & cleaner (macOS)

## Goal
Build a fast, production-quality TUI that (a) shows a **dashboard** of disk usage and
reclaimable space by category, (b) scans the **home folder** and **any arbitrary folder(s)**,
(c) uses a curated, user-overridable **catalog of category templates** (user folders like
Documents/Videos/Pictures *and* developer artifacts like Rust `target/`, Xcode DerivedData,
npm/pnpm/pip/cargo caches), and (d) safely reclaims space via mark-and-trash. macOS-first.

### Decisions locked in with the user
- **Platform:** macOS-first; scanner/trash behind interfaces so Linux/Windows can be added later.
- **Size model:** du-like — allocated (`st_blocks * 512`) by default, hardlinks counted once by
  `(dev, ino)`, with an apparent-size toggle.
- **Landing surface:** **dashboard** is the app's home; the du-style tree browser is a drill-down.
- **Discovery:** **on-demand per category** — nothing heavy runs at startup; category sizes fill in
  lazily and each category scans fully only when opened.
- **Catalog:** curated built-in defaults **plus** an optional `~/.config/sweep/catalog.yaml` override.
- **Name:** `sweep` (binary + Go module `github.com/<USER>/sweep`, placeholder).

## Product surface (views)
1. **Dashboard** (landing) — mirrors the mockup: header, **volume usage bar** (used/total via
   `statfs`), **"Potentially reclaimable"** total, a list of **category rows** (icon, label,
   right-aligned size, size bar, per-row state: `pending / scanning / done / error`), and a
   `help` footer (`↑↓ Navigate · Space Select · Enter Inspect · Q Quit`). Sizes fill in **lazily**.
2. **Category detail** — the concrete items behind a category (e.g. every discovered `node_modules`
   with path + size), sorted by size, with mark/select and inspect.
3. **Tree browser** — du-style directory drill-down for a chosen root (home, an arbitrary path, or a
   category root). Virtualized via `bubbles/v2/table` (constant render cost per row count).
4. **Confirm modal** and **Help overlay**.
5. **CLI** for scripting/verification (non-interactive when stdout is not a TTY).

## Tech stack
- **Go 1.26** (installed; darwin/arm64).
- **Charm v2** (released 2026-02-23) — `charm.land/bubbletea/v2`, `charm.land/lipgloss/v2`,
  `charm.land/bubbles/v2` (`table`, `viewport`, `spinner`, `key`, `help`, `progress`).
- **`github.com/charlievieth/fastwalk`** — parallel `WalkDir` (~2.5x faster than `filepath.WalkDir` on
  macOS, 50% fewer allocs). The scan/discovery engine.
- **`golang.org/x/sys/unix`** — `Statfs` for volume usage.
- **`github.com/spf13/cobra`** — CLI/flags. **`gopkg.in/yaml.v3`** — catalog/config parsing.
- No external trash dep for v1 (see "Deletion safety").

Rationale: the two best-known tools here use other stacks (gdu → tview, dua-cli → Rust). Charm v2's
Cursed Renderer (ncurses-style diffing) directly benefits a live-updating dashboard.

## Project layout
```
sweep/
  go.mod                         // module github.com/<USER>/sweep (placeholder)
  cmd/sweep/main.go              // cobra root: flags -> dashboard | tree | non-interactive
  internal/scan/
    scan.go                      // full-tree Scanner (fastwalk), event stream, cancellation
    size_darwin.go               // //go:build darwin — alloc = st.Blocks*512, nlink
    size_other.go                // //go:build !darwin — apparent-only fallback
    hardlink.go                  // (dev,ino) dedup set
    stat.go                      // cheap single-path sizing (Statfs/du-style) for categories
  internal/tree/
    tree.go                      // arena tree: []Node + parent/child indices
    aggregate.go                 // roll-ups, sort, prune, recompute
  internal/catalog/
    catalog.go                   // Category, Tier, Kind types; loader = defaults + YAML merge
    defaults.go                  // built-in categories (see table below)
    classify.go                  // classify a path -> category; discovery walk -> item list
  internal/scope/
    scope.go                     // resolve targets: home | paths | category -> roots
  internal/config/
    config.go                    // ~/.config/sweep/{config,catalog}.yaml load/merge
  internal/volume/
    volume.go                    // statfs: total/used/available
  internal/trash/
    trash.go                     // Trasher interface
    trash_darwin.go              // move to ~/.Trash with collision handling
  internal/ui/
    app.go                       // root tea.Model + router/state machine
    dashboard.go                 // landing dashboard (bars, category rows, lazy fill)
    category.go                  // category detail view
    browse.go                    // tree browser (table)
    confirm.go                   // mark + confirm modal
    styles.go                    // lipgloss theme, icons, size-bar renderer
  internal/format/size.go        // binary-unit formatting + bar rendering
```
`scan`, `tree`, `catalog`, `scope`, `volume`, `trash`, `config` are **UI-free and unit-testable**;
only `ui` imports Bubble Tea.

## Catalog model (`internal/catalog`)
```go
type Tier int // Safe, Rebuildable, Review, UserData
type Kind int // Path (fixed), Discover (glob under roots), UserFolder

type Category struct {
    ID        string   // "rust-target"
    Label     string   // "Rust target/"
    Icon      string   // nerd-font glyph (with plain fallback)
    Tier      Tier
    Kind      Kind
    Paths     []string // for Kind=Path   (with ~ / ${VAR} expansion)
    Roots     []string // for Kind=Discover: where to search
    Match     []string // for Kind=Discover: globs, e.g. "**/target"
    CleanHint string   // e.g. "cargo clean"
    Doc       string   // what it is / what deletion costs
}
```
**Tier semantics (the safety spine):**
- `Safe` — caches/temp; deleting only costs a re-download. **Counts toward "reclaimable".**
- `Rebuildable` — build output (`target/`, `node_modules`, `.venv`); costs a rebuild. **Counts.**
- `Review` — real data needing judgment (Xcode Archives, Docker, models, simulators). **Shown but not counted.**
- `UserData` — Documents/Pictures/etc. **Never auto-marked; inspect/delete only, manual.**

**"Potentially reclaimable" = sum of `Safe` + `Rebuildable`** (this is why the mockup's green number is
smaller than the artifact list total — an honest, safe default).

**Built-in defaults** (paths from research; `$HOME` = `~`):
| Category | Tier | Kind | Path / Match |
|---|---|---|---|
| Rust `target/` | rebuildable | discover | `**/target` under project roots |
| Cargo registry | safe | path | `~/.cargo/registry`, `~/.cargo/git` |
| Xcode DerivedData | safe | path | `~/Library/Developer/Xcode/DerivedData` |
| Xcode Archives | review | path | `~/Library/Developer/Xcode/Archives` |
| Xcode DeviceSupport | review | path | `~/Library/Developer/Xcode/iOS DeviceSupport` |
| CoreSimulator | review | path | `~/Library/Developer/CoreSimulator` |
| npm | safe | path | `~/.npm` |
| pnpm store | safe | path | `~/.local/share/pnpm/store` |
| Yarn | safe | path | `~/Library/Caches/Yarn` |
| pip | safe | path | `~/Library/Caches/pip` |
| uv | safe | path | `~/.cache/uv` |
| Maven `.m2` | safe | path | `~/.m2/repository` |
| Gradle caches | safe | path | `~/.gradle/caches` |
| Homebrew | safe | path | `~/Library/Caches/Homebrew` |
| CocoaPods | safe | path | `~/Library/Caches/CocoaPods` |
| Go build cache | safe | path | `~/Library/Caches/go-build` |
| Go module cache | safe | path | `~/go/pkg/mod` |
| `node_modules` | rebuildable | discover | `**/node_modules` under project roots |
| Python venvs | rebuildable | discover | `**/.venv`, `**/venv` |
| Docker | review | path | `~/Library/Containers/com.docker.docker` |
| Ollama models | review | path | `~/.ollama/models` |
| Hugging Face cache | review | path | `~/.cache/huggingface` |
| Trash | safe | path | `~/.Trash` (empty action) |
| Desktop/Documents/Downloads/Pictures/Music/Movies/Public | userdata | userfolder | `~/<name>` |

`~/.config/sweep/catalog.yaml` may add, disable, or re-tier any entry (same schema as
`Category`). Merging is by `id`.

## Scopes & discovery (`internal/scope`)
- **home** → root `$HOME` (excludes other volumes; `~/.Trash` handled as its own category).
- **paths** → arbitrary folder(s) from CLI/`--path` or a scope menu.
- **category** → a `Category`'s `Paths` (cheap `stat`) or its `Match` globs under `Roots` (bounded walk).
- **Project roots** for discovery default to `[~/Projects, ~/code, ~/src, ~/Documents, ~/Developer]`
  and are configurable in `config.yaml` — this is what keeps discovery bounded and fast.

## Scanning engine (`internal/scan`)
- `fastwalk.Walk(&fastwalk.Config{Follow: opts.FollowSymlinks}, root, walkFn)`; **walkFn runs
  concurrently**, so it only emits events on a buffered channel — a **single builder goroutine** owns
  the tree (no locks, no races).
- Sizes via per-OS helper:
  ```go
  //go:build darwin
  func statSizes(fi fs.FileInfo) (alloc int64, nlink uint32) {
      st, ok := fi.Sys().(*syscall.Stat_t)
      if !ok { return fi.Size(), 1 }
      return st.Blocks * 512, uint32(st.Nlink) // macOS: 512-byte block units
  }
  ```
- **Cheap category sizing** (`stat.go`): for `Kind=Path`, just `du`-style recursive stat (no full
  tree kept) — this is what makes on-demand dashboard fills fast.
- **Hardlink dedup:** `nlink > 1` → `map[{dev,ino}]bool`; first occurrence adds `alloc`, later ones
  add 0 but stay visible (`H` flag).
- **Errors non-fatal** (permission-denied under `~/Library`, `/System`): flag the dir, keep walking.
- **Cancellation** via `context.Context` (`Esc` stops new work, keeps partial results).
- **Streaming:** started from a `tea.Cmd`; emits `scanStarted`, throttled `scanProgress`, `scanDone`,
  `scanErr`, and (for the dashboard) `categoryResult{ID, Size, Items, Err}`.

## Volume usage (`internal/volume`)
`unix.Statfs(path)`: `total = Blocks*Bsize`, `free = Bfree*Bsize`, `avail = Bavail*Bsize`. Show
`used = (Blocks-Bfree)*Bsize` / `total`. Note APFS "purgeable" space makes `avail` fuzzy — label the
bar as used/total, not as a deletion promise.

## TUI architecture (`internal/ui`)
- Root model is a **router/stack**: `dashboard → categoryDetail | treeBrowser → confirm`, plus `help`.
- **Dashboard** rows are a custom delegate over `bubbles/v2/viewport` (two-line card style: label row +
  size bar) — this matches the mockup better than `table`, which is reserved for the tree browser.
- **Lazy fill:** a bounded scan manager (3–4 workers) scans categories **on focus/visibility** first,
  then the rest at low priority; each row shows `pending → scanning → done/error`. `Esc` cancels.
  A `R` key rescans, `S` "scan all" forces the full set.
- **Tree browser** uses `bubbles/v2/table` (`Name | Size | %bar | Items | Flags`); `table` renders only
  ~2×viewport rows, so 100k children stay constant-cost.
- **Bars:** hand-rendered with full-block glyphs + lipgloss foregrounds (the mockup's thick rounded
  two-tone bar is not what `bubbles/progress` draws).
- **Keys:** `↑↓/j k` move, `l/→/Enter` inspect, `h/←` back, `space` mark, `d` trash marked,
  `D` permanent, `a` apparent/allocated toggle, `s` sort, `/` filter, `R` rescan, `?` help, `q` quit —
  all registered with `bubbles/v2/key` so `help` can't drift from the handlers.
- **Production hardening:** responsive layout + minimum size + reflow on `tea.WindowSizeMsg`;
  color-profile fallback (truecolor→256→ANSI) honoring `NO_COLOR`; icons degrade to ASCII without a
  Nerd Font. `tea.WithColorProfile` + `tea.WithWindowSize` enable **golden-file render tests**.

## Config (`internal/config`)
`~/.config/sweep/config.yaml` — discovery `roots`, `exclude` globs, `theme`, `size_mode`
(allocated|apparent), default scan scope. `catalog.yaml` — category overrides. Missing files are fine.

## Deletion safety (`internal/trash`)
- Default = **move to `~/.Trash`** (collision-suffixed); `--permanent` opts into real delete.
  Document the limitation: plain moves don't support Finder "Put Back".
- `Trasher` interface so the macOS impl is swappable.
- **Mark-then-act** (from dua-cli): never delete on one keystroke; collect a marked set, show count +
  reclaimable total, confirm once.
- Guardrails: `--no-delete` disables all destructive ops; refuse destructive ops while a filter is
  active (visible list may omit files still on disk); **UserData/Review tiers require explicit
  confirmation and are never auto-marked**.
- After deletion, remove nodes and `Recompute` sizes up the parent chain; refresh the category total
  and the reclaimable figure.

## CLI (`cmd/sweep`)
```text
sweep [path...] [flags]
  --home               scan the home folder (tree browser at $HOME)
  --category <id>      jump straight to a category
  --apparent           apparent size instead of allocated
  --no-delete          disable trash/delete
  --permanent          delete instead of trashing
  --exclude strings    path globs to skip
  --follow-symlinks    guarded against loops
  -x, --one-file-system
  --no-hidden          ignore dotfiles/dirs
  --summary            non-interactive: print category totals (verification/scripting)
  --json <file>        export category sizes + tree as JSON
```
Non-interactive output (auto when stdout is not a TTY) is the Phase-1/2 verification path.

## Phases
1. **Foundation (no TUI).** `go mod init`; `format`, `scan` (fastwalk, `size_darwin`, hardlink dedup,
   cheap `stat`), `tree`, `volume`; cobra with `--summary`/`--json`. Verify numbers vs `du`/`gdu`.
2. **Catalog + scopes.** `catalog` (defaults + YAML merge), `config`, `scope`; category sizing +
   bounded discovery; `sweep --summary --json` prints per-category sizes. Verify vs `du -sh` per path.
3. **Dashboard TUI (read-only).** Router skeleton, volume bar, reclaimable total, category rows with
   lazy fill + per-row state, responsive layout, help footer, color-profile handling + golden tests.
4. **Category detail + tree browser.** Item lists (paths+sizes), and `table`-based du drill-down for
   home/arbitrary roots; wire `Enter Inspect` from both dashboard and category detail.
5. **Reclaim.** Mark set + total, confirm modal, trash/permanent, size recompute, guardrail flags,
   tier-aware confirmations.
6. **Polish + config.** Theming, icon fallback, exclude patterns, one-file-system, search/filter,
   big/old-file views, `R`/`S` rescans.

## Verification
- **Unit tests:** `size_darwin` (`alloc == st.Blocks*512`, `apparent == st.Size()`); hardlink dedup via
  `os.Link` (counted once); `tree` roll-up/`Recompute`; `catalog` merge precedence (defaults ← YAML);
  `classify` (path → expected category); `volume` math.
- **Differential:** fixture `testdata/` tree; assert category totals ≈ `du -sk <path>` and compare
  with `gdu -nps`. Golden-file TUI renders at fixed width + forced color profile.
- **Manual:** run on real `$HOME`; confirm dashboard launches instantly, categories fill lazily,
  `Esc` cancels cleanly, category detail and tree navigate, and mark→trash frees the shown total
  (`df` before/after on the same volume). Verify no login shell bash issue: no `--login`; nothing here.

## Risks / notes
- **fastwalk concurrency:** walkFn must never touch the tree directly (single-owner builder goroutine).
- **Permission errors** (`~/Library`, `/System`) are expected and must never abort a scan.
- **Discovery cost of `**/node_modules`/`**/target`:** bound it to configured project roots; skip
  `~/Library`, other volumes, and prune matches (don't descend *into* a matched `node_modules`).
- **Symlink loops** when following links — use fastwalk's guarded `Config.Follow`.
- **APFS accuracy:** clones/snapshots/iCloud-stubs mean "reclaimable" ≠ apparent/allocated; we report
  du-like figures and label the reclaimable total an estimate (clone-aware accounting is a later phase;
  see `duh`/Delve).
- **Memory:** on-demand design avoids holding a full home tree — categories keep only sizes/aggregates;
  full trees exist only for the tree browser of a chosen root.
- **Icon/font:** Nerd Font glyphs need a fallback for terminals without one.

## Explicit non-goals for v1
Windows/Linux support; APFS clone/snapshot-aware reclaimable accounting; web UI; treemap; duplicate-file
detection; persisted/saved scans; scheduled auto-clean.

## Sources
- Charm v2: https://charm.land/blog/v2/ · upgrade guide: https://github.com/charmbracelet/bubbletea/blob/main/UPGRADE_GUIDE_V2.md
- Bubbles v2: https://pkg.go.dev/charm.land/bubbles/v2
- fastwalk: https://github.com/charlievieth/fastwalk
- gdu: https://github.com/dundee/gdu · dua-cli architecture: https://deepwiki.com/Byron/dua-cli/7-architecture
- macOS dev cache paths: https://cleanwithcrumb.com/blog/developer-cache-locations-map-mac/
- APFS byte accounting: https://khaosstudio.com/delve/blog/counting-bytes/ · https://github.com/cheapsteak/duh
- macOS standard folders: https://support.apple.com/guide/mac-help/folders-that-come-with-your-mac-mchlp1143/mac

# Terminal — Agent Instructions

## Project Map

**Always read `PROJECT_MAP.md` at the start of every session** before exploring code.
It maps every package/file with LLM-oriented summaries so you can target fixes without
scrabbling through files. After making structural changes (new files, renamed packages,
significant refactors), update `PROJECT_MAP.md` to reflect those changes.

## What This Project Is

A VT100/xterm-compatible terminal emulator **library** (`github.com/fyne-io/terminal`) built
on the Fyne GUI framework. The library renders a full terminal widget backed by a PTY. A
standalone app (`cmd/fyneterm`) ships alongside it.

## Go Module

`github.com/fyne-io/terminal` — local Fyne fork override at
`/Users/dfj/go/src/github.com/drunlade/fyne` (see `go.mod` replace directive).

## Architecture at a Glance

```
PTY (OS)
  │
  ▼
term.go          — Widget lifecycle, PTY read loop, Config, Listener pattern
  │
  ├─ escape.go   — ANSI/VT escape sequence parser (CSI, OSC, DCS, APC, …)
  ├─ output.go   — Execute parsed commands: write chars, move cursor, charset maps
  ├─ color.go    — Map ANSI / 256-color codes → Fyne theme colors
  ├─ osc.go      — OSC handlers (window title, clipboard, …)
  ├─ apc.go      — APC / DCS handlers
  │
  ├─ render.go   — Layout, font sizing, cursor draw, hands cells to TermGrid
  │
  └─ internal/widget/
       ├─ termgrid.go        — TermGrid widget: dirty-region render loop, blink
       ├─ termgridhelper.go  — Cell styles, highlight ranges, blink control
       └─ termraster.go      — Single-texture raster (1 GL call for whole grid)

Input path:
  input.go / input_other.go / input_windows.go  — key events → PTY bytes
  mouse.go                                       — mouse events → PTY bytes
  select.go                                      — text selection logic

Platform shims:
  term_unix.go    — creack/pty, signal handlers
  term_windows.go — ActiveState ConPTY
```

## Key Invariants

- **Single raster**: the entire grid is one `canvas.Raster`. Never add per-cell widgets.
- **Dirty rendering**: only repaint cells flagged dirty. Touch `termgrid.go` carefully.
- **Font cache**: global per-size; loaded once. See `termraster.go`.
- **Fyne fork**: using a local fork of Fyne (`drunlade/fyne`). API may differ from upstream.

## Build & Test

```bash
# Run tests
go test ./...

# Run the standalone app
go run ./cmd/fyneterm

# Build
go build ./...
```

## Platform Notes

- Unix: `term_unix.go` + `input_other.go`
- Windows: `term_windows.go` + `input_windows.go` (ConPTY)
- Build tags gate platform files; no explicit tags needed — Go selects by filename suffix.

## What to Avoid

- Do not add per-cell Fyne widgets — all rendering goes through the single raster.
- Do not break the dirty-render path in `termgrid.go` / `termraster.go`.
- Do not add external dependencies without checking the Fyne fork compatibility.

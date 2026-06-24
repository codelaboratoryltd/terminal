# Change Log

All notable changes to this project will be documented in this file.
This project adheres to [Semantic Versioning](http://semver.org/).

## [Unreleased] - 2025-09-16
### Added
- `CSI Ps b` (REP): repeat the preceding graphic character `Ps` times. Previously
  the repeats were silently dropped, leaving gaps where terminfo's `rep` capability
  (and box-drawing/fill output) expected characters.
- `OSC 10/11/12`: set and query the default foreground, background and cursor colours.
  Set forms accept X11 colour specs (`#rgb`, `#rrggbb`, `#rrrrggggbbbb`, `rgb:r/g/b`);
  the `?`-query form replies with `rgb:RRRR/GGGG/BBBB`, letting apps (e.g. vim/neovim)
  detect the background and pick an appropriate colour scheme.

## [0.1.45]
### Fixed
- SGR sequences whose first parameter is `0` (e.g. `\x1b[0;1m` reset+bold) no longer
  have the leading `0` stripped, which previously dropped the reset and leaked
  blink/underline/colour attributes into following text.
- Unified the cursor and text blink onto a single blink clock so they pulse in
  lockstep instead of drifting on two independent 500ms tickers. Also removes the
  duplicate blink goroutine and fixes freshly drawn blinking text briefly showing
  a stale phase inherited from a shared interned style.

## [0.1.44]
### Fixed/Changed
- Various performance related improvements to grid rendering

## [0.1.40]
### Fixed
- Patch various race conditions that could cause sudden program exit

## [0.1.39]
### Changed
- Make font size in stretched mode always super-sample to try and prevent blur

## [0.1.38]
### Fixed
 - Fallback to non-bold font if character is not available in bold font

## [0.1.37]
### Changed
- Further improvements to the dirty region renderer and stretch-to-fit mode
### Fixed
- Fix various places where we use Fyne Do/DoWait incorrectly
- Fix various places where we might trigger runtime race conditions (crash to desktop)

## [0.1.36]
### Fixed
- Fix selection highlight bleeding into blank cells
### Added
- Add stretch-to-fit mode for fixed PTY terminals

## [0.1.35]
### Fixed
- Allow for fonts with bottom-of-cell descender underscore which otherwise get clipped

## [0.1.34]
### Added
- Expose blinking cursor position on term grid

## [0.1.33]
### Changed
- Implemented more effecient terminal grid renderer, significantly reduces CPU/GPU load and memory usage
- Restrict term grid refreshes to prevent load spides on PTY updates
### Added
- Add workaround for DejaVu font underscores getting cropped by Fyne's text rendering

## [0.1.32]
### Added
- Add support for slow text-blink

## [0.1.31]
### Fixed
- Fix text selection over multiple lines highlighting the wrong cells
- Fix blinking getting leaked into other cells not intended to blink
- Fix double-click selection blocking on "-" and "_"
### Added
- Add option to hide the tab-bar when only one tab is open
- Add fractional font-scaling to try and allow the terminal to scale closer to borders
- Add support for disabled/enable cursor blinking

## [0.1.30]
### Fixed
- Fix on-screen keyboard arrow keys not working

## [0.1.27]
### Fixed
- Escape and CSI parsing across PTY read boundaries (`afterEsc` / `csi`, 8-bit C1 CSI `0x9b`), with regression tests for split sequences.
- `handleEscape` now keys the final byte by rune and passes parameter bytes as a string of preceding runes (avoids multibyte CSI mis-dispatch).
- IND/NEL/RI (C1 and `ESC` `D`/`E`/`M`) use synchronous `scrollUp`/`scrollDown` so scroll matches further output in the same chunk.
- `TermGrid` dispatches `TextGrid.Refresh` via `fyne.Do` for driver-thread safety; `mayContainBlink` skips full-grid scans when no blink cells.
- Invalidate TermGrid blink cache when blink SGR or wholesale buffer clears / alt-screen switches change cell styles or content.

### Changed
- Reuse a `readMerge` scratch buffer when merging PTY leftovers with the next read (avoids allocating a new slice each chunk).
- Accumulate CSI parameters with `strings.Builder` in `parseEscape`; minor `handleOutputChar` hot-path cleanup.
- `scrollUp`/`scrollDown` call `content.Refresh` after mutating rows so the grid updates when a read ends with incomplete UTF-8 (no `run()` refresh that pass).
- Ignore a root-level `fyneterm` build artifact in `.gitignore`.

### Tests
- Reset quad-tap debounce state between `select` subtests for stable `TestDoubleTapped`.

## [0.1.26]
### Fixed
- Protect against nil values with styles and themes

## [0.1.25]
### Fixed
 - Another fix multi-line paste (previous one worked only for MacOS and Linux)

## [0.1.24]
### Fixed
 - Fix multi-line paste

## [0.1.3]
### Fixed
 - Fix region scrolling
 - Fix missing apc/osc codes and screen modes
 - Fix blinking and cursor rendering
 - Fix non-tmux usage (part of the apc/osc fixes too)
 - Fix colour modes (mainly to use proper ANSI colours by default!)

## [0.1.2]
### Change
 - Protecting cursor movement to correct thread

## [0.1.1]
### Changed
 - Improve guessCellSize optimisation

## [0.1.0]
 - Initial

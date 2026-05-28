# Change Log

All notable changes to this project will be documented in this file.
This project adheres to [Semantic Versioning](http://semver.org/).

## [Unreleased] - 2025-09-16

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

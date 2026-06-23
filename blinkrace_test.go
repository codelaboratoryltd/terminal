package terminal

import (
	"sync"
	"testing"
	"time"
)

// TestBlinkClockLifecycleRace hammers the blink-clock lifecycle from multiple
// goroutines, mirroring how it is reached concurrently in practice: focus
// changes on the Fyne main goroutine and escape sequences on the PTY reader
// goroutine (escape.go -> refreshCursor -> ensureBlinkClock). Before the
// blinkMu guard, two racing startBlinkClock calls could both pass the
// "already running?" check and leak an orphan ticker goroutine, producing an
// erratic multi-oscillator cursor blink. Run with -race.
func TestBlinkClockLifecycleRace(t *testing.T) {
	term := New()
	term.focused = true // so cursorShouldBlink() is true and the clock wants to run

	const goroutines = 8
	var wg sync.WaitGroup
	stop := time.Now().Add(200 * time.Millisecond) // shorter than blinkInterval: the ticker never fires

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for time.Now().Before(stop) {
				term.ensureBlinkClock() // starts the clock (guarded check-and-set)
				term.stopBlinkClock()   // cancels it
			}
		}()
	}

	wg.Wait()
	term.stopBlinkClock()
}

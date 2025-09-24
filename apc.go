package terminal

import (
	"log"
	"strings"
)

// APCHandler handles a APC command for the given terminal.
type APCHandler func(*Terminal, string)

func (t *Terminal) handleAPC(code string) {
	if t.apcHandlers == nil {
		return
	}
	for apcCommand, handler := range t.apcHandlers {
		if strings.HasPrefix(code, apcCommand) {
			// Extract the argument from the code
			arg := code[len(apcCommand):]
			// Invoke the corresponding handler function
			handler(t, arg)
			return
		}
	}

	if t.debug {
		// Handle other APC sequences or log the received APC code
		log.Println("Unrecognised APC", code)
	}
}

// handleDCS processes Device Control String data (between ESC P ... ST)
// Implements tmux passthrough: "tmux;" prefix means the rest is a nested sequence
func (t *Terminal) handleDCS(code string) {
	if strings.HasPrefix(code, "tmux;") {
		inner := code[len("tmux;"):]
		// Some tmux versions double ESC within the payload (ESC ESC). Collapse them back.
		inner = strings.ReplaceAll(inner, string([]byte{27, 27}), string([]byte{27}))
		if strings.IndexByte(inner, 27) == -1 {
			// Fast-path: plain text, write directly
			for _, r := range inner {
				t.handleOutputChar(rune(r))
			}
		} else {
			t.handleOutput([]byte(inner))
		}
		return
	}
	if strings.HasPrefix(code, "screen;") {
		inner := code[len("screen;"):]
		if strings.IndexByte(inner, 27) == -1 {
			for _, r := range inner {
				t.handleOutputChar(rune(r))
			}
		} else {
			t.handleOutput([]byte(inner))
		}
		return
	}
	// Future: handle other DCS (e.g., DECRQSS, XTGETTCAP) as needed
	if t.debug {
		log.Println("Unhandled DCS", code)
	}
}

// RegisterAPCHandler registers an APC handler on this terminal instance
// for the given APC command string.
func (t *Terminal) RegisterAPCHandler(APC string, handler APCHandler) {
	if t.apcHandlers == nil {
		t.apcHandlers = make(map[string]func(*Terminal, string))
	}
	t.apcHandlers[APC] = handler
}

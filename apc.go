package terminal

import (
	"log"
	"strings"
)

// APCHandler handles a APC command for the given terminal.
type APCHandler func(*Terminal, string)

var apcHandlers = map[string]func(*Terminal, string){}

func (t *Terminal) handleAPC(code string) {
	for apcCommand, handler := range apcHandlers {
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
		// tmux encodes ESC as ESC ESC in some cases; a basic passthrough here is to
		// re-inject bytes as-is. We rely on our parser to handle them.
		_, _ = t.in.Write([]byte(inner))
		return
	}
	// Future: handle other DCS (e.g., DECRQSS, XTGETTCAP) as needed
	if t.debug {
		log.Println("Unhandled DCS", code)
	}
}

// RegisterAPCHandler registers a APC handler for the given APC command string.
func RegisterAPCHandler(APC string, handler APCHandler) {
	apcHandlers[APC] = handler
}

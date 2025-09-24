package terminal

import (
	"log"
	"os"
	"strconv"
	"strings"

	"fyne.io/fyne/v2/storage"
)

func (t *Terminal) handleOSC(code string) {
	if len(code) == 0 {
		return
	}

	// Parse the command number and data
	parts := strings.SplitN(code, ";", 2)
	if len(parts) < 2 {
		if t.debug {
			log.Println("Invalid OSC format:", code)
		}
		return
	}

	commandNum, err := strconv.Atoi(parts[0])
	if err != nil {
		if t.debug {
			log.Println("Invalid OSC command number:", parts[0])
		}
		return
	}
	data := parts[1]

	// Check if there's a registered handler for this command
	if t.oscHandlers != nil {
		if handler, exists := t.oscHandlers[commandNum]; exists {
			handler(data)
			return
		}
	}

	// Fall back to default behavior for built-in commands
	switch commandNum {
	case 0:
		// set icon name and window title
		t.setTitle(data)
	case 1:
		// set icon name, if Fyne supports in the future
	case 2:
		// set window title
		t.setTitle(data)
	case 7:
		t.setDirectory(data)
	case 133:
		// Shell integration sequences for prompt marking
		t.handleOSC133(data)
	default:
		if t.debug {
			log.Println("Unrecognised OSC:", code)
		}
	}
}

func (t *Terminal) setDirectory(uri string) {
	u, err := storage.ParseURI(uri)
	if err != nil {
		// working around a Fyne bug where file URI does not parse host
		off := 4
		count := 0
		for count < 3 && off < len(uri) {
			off++
			if uri[off] == '/' {
				count++
			}

		}
		os.Chdir(uri[off:])
		return
	}

	// fallback to guessing it's a path
	os.Chdir(u.Path())
}

func (t *Terminal) setTitle(title string) {
	t.config.Title = title
	t.onConfigure()
}

// handleOSC133 handles shell integration sequences for prompt marking
func (t *Terminal) handleOSC133(data string) {
	switch data {
	case "A":
		// Mark the start of a command prompt
		t.handlePromptStart()
	case "B":
		// Mark the end of a command prompt (start of command input)
		t.handlePromptEnd()
	case "C":
		// Mark the start of command output
		t.handleCommandStart()
	case "D":
		// Mark the end of command output
		t.handleCommandEnd()
	default:
		// For other OSC 133 sequences (like D;exit_code), we can ignore them
		// or handle them in the future if needed
		if t.debug {
			log.Println("OSC 133 sequence not implemented:", data)
		}
	}
}

// handlePromptStart marks the beginning of a command prompt
func (t *Terminal) handlePromptStart() {
	// This could be used to mark prompt positions for navigation features
	// For now, we'll just silently handle it to prevent the "Unrecognised OSC" messages
	if t.debug {
		log.Println("Shell integration: Prompt start")
	}
}

// handlePromptEnd marks the end of a command prompt (start of command input)
func (t *Terminal) handlePromptEnd() {
	// This marks where the user starts typing commands
	if t.debug {
		log.Println("Shell integration: Prompt end / Command input start")
	}
}

// handleCommandStart marks the start of command output
func (t *Terminal) handleCommandStart() {
	// This marks where command output begins
	if t.debug {
		log.Println("Shell integration: Command output start")
	}
}

// handleCommandEnd marks the end of command output
func (t *Terminal) handleCommandEnd() {
	// This marks where command output ends
	if t.debug {
		log.Println("Shell integration: Command output end")
	}
}

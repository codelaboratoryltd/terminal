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

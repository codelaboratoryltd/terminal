package terminal

import (
	"testing"

	"fyne.io/fyne/v2"

	"github.com/stretchr/testify/assert"
)

func TestTerminalBackspace(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Hi", "Hi"},
		{"Hello\b", "Hell"},
		{"Hello\bË", "HellË"},
		{"Hello\bÜ", "HellÜ"},
	}

	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			term := New()
			term.Resize(fyne.NewSize(50, 50))
			term.handleOutput([]byte(test.input))
			assert.Equal(t, test.expected, term.content.Text())
		})
	}
}

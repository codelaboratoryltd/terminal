package terminal

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOSC_Title(t *testing.T) {
	term := New()
	assert.Equal(t, "", term.config.Title)

	term.handleOSC("0;Test")
	assert.Equal(t, "Test", term.config.Title)

	term.handleOSC("0;Testing;123")
	assert.Equal(t, "Testing;123", term.config.Title)
}

func TestOSCHandler(t *testing.T) {
	term := New()

	// Test registering and calling OSC handler
	var receivedCommand int
	var receivedData string

	handler := func(data string) {
		receivedCommand = 42
		receivedData = data
	}

	term.RegisterOSCHandler(42, handler)

	// Simulate OSC sequence: ESC ] 42 ; test data BEL
	term.handleOSC("42;test data")

	assert.Equal(t, 42, receivedCommand)
	assert.Equal(t, "test data", receivedData)
}

func TestOSCBuiltinHandlers(t *testing.T) {
	term := New()

	// Test that built-in handlers still work
	term.handleOSC("0;Window Title")
	assert.Equal(t, "Window Title", term.config.Title)

	term.handleOSC("2;Another Title")
	assert.Equal(t, "Another Title", term.config.Title)
}

func TestOSCHandlerOverride(t *testing.T) {
	term := New()

	// Test that custom handler overrides built-in behavior
	var customTitleSet bool
	var customTitle string

	handler := func(data string) {
		customTitleSet = true
		customTitle = data
	}

	term.RegisterOSCHandler(0, handler)

	// This should call our custom handler instead of setTitle
	term.handleOSC("0;Custom Title")

	assert.True(t, customTitleSet)
	assert.Equal(t, "Custom Title", customTitle)

	// Built-in title should not be set since our handler overrides it
	assert.NotEqual(t, "Custom Title", term.config.Title)
}

package context

import "github.com/charmbracelet/bubbles/key"

// Context holds shared application state that components need access to.
type Context struct {
	ActiveTab    int
	HelpBindings []key.Binding
}

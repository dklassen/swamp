package model

import "charm.land/lipgloss/v2"

var (
	CursorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("212"))
	HelpStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).MarginTop(1)
	// DimStyle is HelpStyle's same muted color without its MarginTop(1) --
	// that margin renders a leading blank line, which is right for the
	// screen-bottom help line HelpStyle exists for but breaks any inline
	// use (a table cell, a line that isn't the very start of the view).
	DimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	ErrStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	WarnStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	PassStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	FieldLabel   = lipgloss.NewStyle().Bold(true)
	FocusedLabel = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	SectionStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
)

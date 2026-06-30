package tui

import tea "github.com/charmbracelet/bubbletea"

var program *tea.Program

func SetProgram(p *tea.Program) {
	program = p
}

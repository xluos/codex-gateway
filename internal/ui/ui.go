package ui

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	subtleStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	warnStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
	errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	labelStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
)

func Banner() string {
	return titleStyle.Render(strings.TrimSpace(`
   ____          __           
  / ___|___   __/ /___ _ __ __
 | |   / _ \ / _  / _ \ \ / /
 | |__| (_) | (_| |  __/>  < 
  \____\___/ \__,_|\___/_/\_\
`)) + "\n" + titleStyle.Render("Codex Gateway") + "\n" + subtleStyle.Render("OpenAI OAuth local gateway")
}

func Section(title string) string {
	return "\n" + labelStyle.Render(title)
}

func KV(label, value string) string {
	return fmt.Sprintf("%s %s", labelStyle.Render(label+":"), value)
}

func Success(msg string) string {
	return successStyle.Render("OK") + " " + msg
}

func Warn(msg string) string {
	return warnStyle.Render("WARN") + " " + msg
}

func Error(msg string) string {
	return errorStyle.Render("ERR") + " " + msg
}

func Muted(msg string) string {
	return subtleStyle.Render(msg)
}

func PrintLines(w io.Writer, lines ...string) {
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			_, _ = io.WriteString(w, "\n")
			continue
		}
		_, _ = io.WriteString(w, line)
		_, _ = io.WriteString(w, "\n")
	}
}

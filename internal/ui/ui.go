package ui

import (
	_ "embed"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

var (
	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	subtleStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	warnStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
	errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	labelStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
)

//go:embed banner_plain.txt
var bannerPlain string

//go:embed banner_ansi.txt
var bannerANSI string

func Banner(w io.Writer) string {
	return bannerForWriter(w, os.Getenv, isTTYWriter)
}

func bannerForWriter(w io.Writer, getenv func(string) string, ttyDetector func(io.Writer) bool) string {
	if shouldUseANSIBanner(w, getenv, ttyDetector) {
		return bannerANSI + "\n" + titleStyle.Render("Codex Gateway") + "\n" + subtleStyle.Render("OpenAI OAuth local gateway")
	}
	return bannerPlain + "\nCodex Gateway\nOpenAI OAuth local gateway"
}

func shouldUseANSIBanner(w io.Writer, getenv func(string) string, ttyDetector func(io.Writer) bool) bool {
	if getenv("NO_COLOR") != "" {
		return false
	}
	return ttyDetector(w)
}

func isTTYWriter(w io.Writer) bool {
	file, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(file.Fd()))
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

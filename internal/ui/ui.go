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

type rgb struct {
	r int
	g int
	b int
}

//go:embed banner_plain.txt
var bannerPlain string

var bannerANSI = renderANSIBanner(bannerPlain, []rgb{
	{r: 80, g: 200, b: 255},
	{r: 70, g: 190, b: 255},
	{r: 60, g: 180, b: 255},
	{r: 50, g: 170, b: 245},
	{r: 40, g: 160, b: 235},
	{r: 30, g: 150, b: 230},
	{r: 20, g: 140, b: 220},
	{r: 10, g: 130, b: 210},
	{r: 0, g: 120, b: 200},
	{r: 0, g: 110, b: 190},
	{r: 0, g: 100, b: 180},
})

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

func renderANSIBanner(plain string, palette []rgb) string {
	lines := strings.Split(strings.TrimSuffix(plain, "\n"), "\n")
	var out strings.Builder
	colorIndex := 0

	for _, line := range lines {
		if line == "" {
			out.WriteString("\n")
			continue
		}
		if colorIndex >= len(palette) {
			out.WriteString(line)
			out.WriteString("\n")
			continue
		}

		color := palette[colorIndex]
		out.WriteString(fmt.Sprintf("\x1b[38;2;%d;%d;%dm%s\x1b[0m\n", color.r, color.g, color.b, line))
		colorIndex++
	}

	return out.String()
}

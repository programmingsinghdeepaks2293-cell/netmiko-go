package netmiko

import (
	"regexp"
	"strings"
)

var (
	// ansiRegex matches standard VT100 / ANSI escape sequences
	ansiRegex = regexp.MustCompile(`\x1B(?:[@-Z\\-_]|\[[0-?]*[ -/]*[@-~])`)

	// lineBreakRegex matches CRLF, CR, and LF
	lineBreakRegex = regexp.MustCompile(`\r\n|\r|\n`)
)

// StripAnsi removes ANSI escape sequences from output.
func StripAnsi(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

// NormalizeLinebreaks standardizes all line breaks to LF (\n).
func NormalizeLinebreaks(s string) string {
	return lineBreakRegex.ReplaceAllString(s, "\n")
}

// StripCommand removes the echoed command from the beginning of response output.
func StripCommand(command, response string) string {
	respNorm := NormalizeLinebreaks(response)
	cmdNorm := strings.TrimSpace(command)

	lines := strings.Split(respNorm, "\n")
	if len(lines) == 0 {
		return response
	}

	// Check if first line matches or contains command
	firstLine := strings.TrimSpace(lines[0])
	if strings.Contains(firstLine, cmdNorm) {
		return strings.Join(lines[1:], "\n")
	}
	return respNorm
}

// StripPrompt removes the trailing prompt from command output.
func StripPrompt(response string) string {
	respNorm := NormalizeLinebreaks(response)
	lines := strings.Split(respNorm, "\n")
	if len(lines) <= 1 {
		return response
	}

	// Drop last line if it resembles a prompt (e.g. ends with #, >, $)
	last := strings.TrimSpace(lines[len(lines)-1])
	if strings.HasSuffix(last, "#") || strings.HasSuffix(last, ">") || strings.HasSuffix(last, "$") || strings.HasSuffix(last, "%") {
		return strings.Join(lines[:len(lines)-1], "\n")
	}
	return respNorm
}

// CleanOutput combines ANSI stripping, command stripping, and prompt stripping.
func CleanOutput(command, output string, stripCmd, stripPmt bool) string {
	out := StripAnsi(output)
	out = NormalizeLinebreaks(out)
	if stripCmd {
		out = StripCommand(command, out)
	}
	if stripPmt {
		out = StripPrompt(out)
	}
	return strings.TrimSpace(out)
}

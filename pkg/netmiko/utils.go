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
	if cmdNorm != "" && strings.Contains(firstLine, cmdNorm) {
		return strings.Join(lines[1:], "\n")
	}
	return respNorm
}

// StripPrompt removes a trailing prompt line from output, using a heuristic: the
// last non-empty line is treated as a prompt if it is a single token ending in
// #, >, $ or %. Prefer the pattern-based cleanup used by SendCommand when a
// prompt pattern is known.
func StripPrompt(response string) string {
	respNorm := NormalizeLinebreaks(response)
	lines := strings.Split(respNorm, "\n")
	idx := lastNonEmptyLine(lines)
	if idx < 0 {
		return respNorm
	}
	last := strings.TrimSpace(lines[idx])
	if strings.ContainsAny(last, " \t") || !strings.ContainsAny(last[len(last)-1:], "#>$%") {
		return respNorm
	}
	return strings.Join(lines[:idx], "\n")
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

// cleanWithPattern removes the echoed command and, if the last line matches the
// prompt pattern that terminated the read, that prompt line.
func cleanWithPattern(command, raw string, stripCmd, stripPmt bool, prompt *regexp.Regexp) string {
	out := NormalizeLinebreaks(StripAnsi(raw))
	if stripCmd {
		out = StripCommand(command, out)
	}
	if stripPmt && prompt != nil {
		lines := strings.Split(out, "\n")
		idx := lastNonEmptyLine(lines)
		if idx >= 0 && prompt.MatchString(strings.TrimSpace(lines[idx])) {
			out = strings.Join(lines[:idx], "\n")
		}
	}
	return strings.TrimSpace(out)
}

// lastNonEmptyLine returns the index of the last line that is not blank, or -1.
func lastNonEmptyLine(lines []string) int {
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return i
		}
	}
	return -1
}

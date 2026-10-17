package netmiko

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/sirikothe/gotextfsm"
)

// ParseTextFSM parses raw CLI output using a TextFSM template string and returns a slice of map records.
func ParseTextFSM(templateContent string, rawOutput string) ([]map[string]any, error) {
	fsm := gotextfsm.TextFSM{}
	if err := fsm.ParseString(templateContent); err != nil {
		return nil, fmt.Errorf("failed to parse TextFSM template: %w", err)
	}

	parser := gotextfsm.ParserOutput{}
	if err := parser.ParseTextString(rawOutput, fsm, true); err != nil {
		return nil, fmt.Errorf("failed to execute TextFSM parser on output: %w", err)
	}

	res := make([]map[string]any, len(parser.Dict))
	for i, record := range parser.Dict {
		m := make(map[string]any, len(record))
		for k, v := range record {
			m[k] = v
		}
		res[i] = m
	}
	return res, nil
}

// ParseTextFSMFile reads a template file from disk and parses rawOutput.
func ParseTextFSMFile(templateFilePath string, rawOutput string) ([]map[string]any, error) {
	data, err := os.ReadFile(templateFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read TextFSM template file %q: %w", templateFilePath, err)
	}
	return ParseTextFSM(string(data), rawOutput)
}

// ntcIndexEntry is one row of an NTC-templates "index" file.
type ntcIndexEntry struct {
	template string
	platform string
	command  *regexp.Regexp
}

var (
	ntcMu         sync.Mutex
	ntcCache      = map[string][]ntcIndexEntry{}
	ntcOptionalRe = regexp.MustCompile(`\[\[([^\]]+)\]\]`)
)

// ntcTemplateDir returns NET_TEXTFSM, or the default ~/ntc-templates location if it exists.
func ntcTemplateDir() (string, error) {
	if dir := os.Getenv("NET_TEXTFSM"); dir != "" {
		return dir, nil
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidate := filepath.Join(home, "ntc-templates", "ntc_templates", "templates")
		if _, statErr := os.Stat(candidate); statErr == nil {
			return candidate, nil
		}
	}
	return "", errors.New("NET_TEXTFSM directory not found; set NET_TEXTFSM to the NTC templates directory")
}

// normalizeNTCPlatform maps Netmiko device types to NTC platform names.
func normalizeNTCPlatform(p string) string {
	p = strings.ToLower(strings.TrimSpace(p))
	if p == "juniper" {
		return "juniper_junos"
	}
	return p
}

// normalizeCommand lowercases a command and collapses runs of whitespace.
func normalizeCommand(c string) string {
	return strings.ToLower(strings.Join(strings.Fields(c), " "))
}

// loadNTCIndex parses the "index" file in an NTC templates directory. NTC index
// commands use "[[xyz]]" to mark optional completions, e.g. "sh[[ow]] ip int[[erface]] br[[ief]]".
func loadNTCIndex(dir string) ([]ntcIndexEntry, error) {
	ntcMu.Lock()
	defer ntcMu.Unlock()

	if entries, ok := ntcCache[dir]; ok {
		return entries, nil
	}
	data, err := os.ReadFile(filepath.Join(dir, "index"))
	if err != nil {
		return nil, err
	}

	var entries []ntcIndexEntry
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ",", 4)
		if len(parts) != 4 {
			continue
		}
		tmpl := strings.TrimSpace(parts[0])
		if strings.EqualFold(tmpl, "template") {
			continue // header row
		}
		platform := strings.ToLower(strings.TrimSpace(parts[2]))
		pattern := ntcOptionalRe.ReplaceAllString(normalizeCommand(parts[3]), "(?:${1})?")
		re, err := regexp.Compile("^(?:" + pattern + ")$")
		if err != nil {
			continue
		}
		entries = append(entries, ntcIndexEntry{template: tmpl, platform: platform, command: re})
	}

	ntcCache[dir] = entries
	return entries, nil
}

// FindNTCTemplate finds the NTC template for a platform and command. It consults
// the NTC index first, so abbreviated commands such as "show ip interface brief"
// resolve to cisco_ios_show_ip_int_brief.textfsm, and falls back to a filename match.
func FindNTCTemplate(platform, command string) (string, error) {
	dir, err := ntcTemplateDir()
	if err != nil {
		return "", err
	}

	plat := normalizeNTCPlatform(platform)
	cmd := normalizeCommand(command)

	if entries, err := loadNTCIndex(dir); err == nil {
		for _, e := range entries {
			if e.platform != plat || !e.command.MatchString(cmd) {
				continue
			}
			path := filepath.Join(dir, e.template)
			if _, statErr := os.Stat(path); statErr == nil {
				return path, nil
			}
		}
	}

	name := fmt.Sprintf("%s_%s.textfsm", plat, strings.ReplaceAll(cmd, " ", "_"))
	full := filepath.Join(dir, name)
	if _, statErr := os.Stat(full); statErr == nil {
		return full, nil
	}
	return "", fmt.Errorf("no NTC template matches platform %q and command %q in %s", platform, command, dir)
}

// SendCommandStructured sends a command to the device and parses the output into structured data.
//
// If a template is explicitly provided, it will be used (either as file path or template content).
// Otherwise, it will attempt to find a matching NTC template using FindNTCTemplate.
func (bc *BaseConnection) SendCommandStructured(command string, template ...string) ([]map[string]any, error) {
	raw, err := bc.SendCommand(command)
	if err != nil {
		return nil, err
	}

	if len(template) > 0 && template[0] != "" {
		t := template[0]
		// Check if it's a file on disk
		if _, statErr := os.Stat(t); statErr == nil {
			return ParseTextFSMFile(t, raw)
		}
		// Treat as raw template content
		return ParseTextFSM(t, raw)
	}

	// Auto-lookup in NTC templates
	templatePath, err := FindNTCTemplate(bc.Device.DeviceType, command)
	if err != nil {
		return nil, fmt.Errorf("could not auto-resolve template for %s %q: %w", bc.Device.DeviceType, command, err)
	}

	return ParseTextFSMFile(templatePath, raw)
}

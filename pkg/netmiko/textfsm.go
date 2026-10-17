package netmiko

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

// FindNTCTemplate searches the NET_TEXTFSM directory or NTC-templates directory for a matching template.
//
// Template filenames typically follow: <platform>_<command_with_underscores>.textfsm
// e.g. cisco_ios_show_ip_int_brief.textfsm
func FindNTCTemplate(platform, command string) (string, error) {
	templateDir := os.Getenv("NET_TEXTFSM")
	if templateDir == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			candidate := filepath.Join(home, "ntc-templates", "ntc_templates", "templates")
			if _, statErr := os.Stat(candidate); statErr == nil {
				templateDir = candidate
			}
		}
	}
	if templateDir == "" {
		return "", fmt.Errorf("NET_TEXTFSM directory not found; set NET_TEXTFSM environment variable")
	}

	cmdClean := strings.ToLower(strings.TrimSpace(command))
	cmdParts := strings.Fields(cmdClean)
	templateName := fmt.Sprintf("%s_%s.textfsm", strings.ToLower(platform), strings.Join(cmdParts, "_"))

	fullPath := filepath.Join(templateDir, templateName)
	if _, err := os.Stat(fullPath); err == nil {
		return fullPath, nil
	}

	return "", fmt.Errorf("template %q not found in %s", templateName, templateDir)
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

package juniper

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/netmiko-go/netmiko/pkg/netmiko"
)

// junosConfigPromptRe matches a Junos configuration-mode prompt, which ends in "#".
var junosConfigPromptRe = regexp.MustCompile(`[^\r\n]*#\s*$`)

// JuniperJunos implements the Netmiko driver for Juniper JunOS devices.
type JuniperJunos struct {
	*netmiko.BaseConnection
}

func NewJuniperJunos(dev netmiko.Device) *JuniperJunos {
	bc := netmiko.NewBaseConnection(dev)
	bc.DefaultPromptTerm = ">"
	return &JuniperJunos{BaseConnection: bc}
}

var _ netmiko.Connection = (*JuniperJunos)(nil)

func (j *JuniperJunos) Connect(ctx context.Context) error {
	if err := j.BaseConnection.Connect(ctx); err != nil {
		return err
	}
	if _, err := j.SetBasePrompt(); err != nil {
		return err
	}
	_ = j.DisablePaging()
	return nil
}

func (j *JuniperJunos) DisablePaging() error {
	_, _ = j.SendCommand("set cli screen-length 0")
	_, _ = j.SendCommand("set cli screen-width 0")
	return nil
}

// ConfigMode enters Junos configuration mode.
func (j *JuniperJunos) ConfigMode() error {
	inConfig, err := j.CheckConfigMode()
	if err == nil && inConfig {
		return nil
	}
	if err := j.WriteChannel("configure\n"); err != nil {
		return err
	}
	_, err = j.ReadUntilPattern(regexp.MustCompile(`[#]\s*$`), 10*time.Second)
	return err
}

// ExitConfigMode exits Junos configuration mode.
func (j *JuniperJunos) ExitConfigMode() error {
	inConfig, err := j.CheckConfigMode()
	if err == nil && !inConfig {
		return nil
	}
	if err := j.WriteChannel("exit\n"); err != nil {
		return err
	}
	_, err = j.ReadUntilPattern(regexp.MustCompile(`[>]\s*$`), 10*time.Second)
	return err
}

// CheckConfigMode reports whether the prompt indicates configuration mode (ends in "#").
func (j *JuniperJunos) CheckConfigMode() (bool, error) {
	prompt, err := j.FindPrompt()
	if err != nil {
		return false, err
	}
	return strings.HasSuffix(prompt, "#"), nil
}

// SendConfigSet enters configuration mode, applies each set/delete statement, and
// returns to operational mode. Changes are staged but not committed; call Commit
// afterwards to apply them.
func (j *JuniperJunos) SendConfigSet(commands []string, opts ...netmiko.ConfigOption) (string, error) {
	var o netmiko.ConfigOptions
	for _, opt := range opts {
		opt(&o)
	}
	timeout := o.ReadTimeout
	if timeout <= 0 {
		timeout = j.Device.Timeout
	}

	if err := j.ConfigMode(); err != nil {
		return "", fmt.Errorf("failed to enter configuration mode: %w", err)
	}

	var out strings.Builder
	for _, cmd := range commands {
		if strings.TrimSpace(cmd) == "" {
			continue
		}
		if err := j.WriteChannel(cmd + "\n"); err != nil {
			return netmiko.CleanOutput("", out.String(), false, true), err
		}
		res, err := j.ReadUntilPattern(junosConfigPromptRe, timeout)
		out.WriteString(res)
		if err != nil {
			_ = j.ExitConfigMode()
			return netmiko.CleanOutput("", out.String(), false, true), fmt.Errorf("config command %q failed: %w", cmd, err)
		}
	}

	if err := j.ExitConfigMode(); err != nil {
		return netmiko.CleanOutput("", out.String(), false, true), fmt.Errorf("failed to exit configuration mode: %w", err)
	}
	return netmiko.CleanOutput("", out.String(), false, true), nil
}

// Commit commits configuration changes.
func (j *JuniperJunos) Commit() (string, error) {
	inConfig, _ := j.CheckConfigMode()
	if !inConfig {
		return "", errors.New("cannot commit: not in configuration mode")
	}
	return j.SendCommand("commit", netmiko.WithReadTimeout(60*time.Second))
}

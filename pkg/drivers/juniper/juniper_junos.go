package juniper

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/netmiko-go/netmiko/pkg/netmiko"
)

// JuniperJunos implements the Netmiko driver for Juniper JunOS devices.
type JuniperJunos struct {
	*netmiko.BaseConnection
}

func NewJuniperJunos(dev netmiko.Device) *JuniperJunos {
	bc := netmiko.NewBaseConnection(dev)
	bc.DefaultPromptTerm = ">"
	return &JuniperJunos{BaseConnection: bc}
}

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

// CheckConfigMode checks if currently in configuration mode ([edit ...]#).
func (j *JuniperJunos) CheckConfigMode() (bool, error) {
	prompt, err := j.FindPrompt()
	if err != nil {
		return false, err
	}
	return strings.HasSuffix(prompt, "#"), nil
}

// Commit commits configuration changes.
func (j *JuniperJunos) Commit() (string, error) {
	inConfig, _ := j.CheckConfigMode()
	if !inConfig {
		return "", errors.New("cannot commit: not in configuration mode")
	}
	return j.SendCommand("commit", netmiko.WithReadTimeout(60*time.Second))
}

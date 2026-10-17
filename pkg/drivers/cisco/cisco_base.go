package cisco

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/netmiko-go/netmiko/pkg/netmiko"
)

// CiscoBase provides the foundation for Cisco network OS drivers (IOS, XE, NX-OS, XR).
type CiscoBase struct {
	*netmiko.BaseConnection
}

func NewCiscoBase(dev netmiko.Device) *CiscoBase {
	bc := netmiko.NewBaseConnection(dev)
	bc.DefaultPromptTerm = "#"
	return &CiscoBase{BaseConnection: bc}
}

// SessionPreparation disables terminal pagination and establishes base prompt.
func (c *CiscoBase) SessionPreparation() error {
	// Set base prompt
	if _, err := c.SetBasePrompt(); err != nil {
		return err
	}
	// Disable paging
	_ = c.DisablePaging()
	return nil
}

// DisablePaging sends standard Cisco terminal length command.
func (c *CiscoBase) DisablePaging() error {
	_, err := c.SendCommand("terminal length 0")
	return err
}

// SetTerminalWidth sets terminal width to maximum.
func (c *CiscoBase) SetTerminalWidth() error {
	_, err := c.SendCommand("terminal width 511")
	return err
}

// Enable elevates privileges to privileged EXEC mode (#).
func (c *CiscoBase) Enable() error {
	inEnable, err := c.CheckEnableMode()
	if err == nil && inEnable {
		return nil
	}

	if err := c.WriteChannel("enable\n"); err != nil {
		return err
	}

	// Expect password challenge or prompt
	re := regexp.MustCompile(`([Pp]assword:|[#>$])\s*$`)
	out, err := c.ReadUntilPattern(re, 10*time.Second)
	if err != nil {
		return fmt.Errorf("enable failed: %w", err)
	}

	if strings.Contains(strings.ToLower(out), "password:") {
		secret := c.Device.Secret
		if secret == "" {
			secret = c.Device.Password
		}
		if secret == "" {
			return errors.New("enable password required but secret/password is empty")
		}
		if err := c.WriteChannelSensitive(secret + "\n"); err != nil {
			return err
		}
		_, err = c.ReadUntilPrompt(10 * time.Second)
		if err != nil {
			return fmt.Errorf("failed after entering enable secret: %w", err)
		}
	}

	inEnable, err = c.CheckEnableMode()
	if err != nil || !inEnable {
		return errors.New("failed to enter enable mode: prompt does not end with #")
	}

	return nil
}

// ExitEnableMode steps down from privileged EXEC mode.
func (c *CiscoBase) ExitEnableMode() error {
	inEnable, _ := c.CheckEnableMode()
	if !inEnable {
		return nil
	}
	if err := c.WriteChannel("disable\n"); err != nil {
		return err
	}
	_, err := c.ReadUntilPrompt(10 * time.Second)
	return err
}

// CheckEnableMode verifies whether current prompt indicates privileged EXEC mode (#).
func (c *CiscoBase) CheckEnableMode() (bool, error) {
	prompt, err := c.FindPrompt()
	if err != nil {
		return false, err
	}
	return strings.HasSuffix(prompt, "#"), nil
}

// ConfigMode enters global configuration mode.
func (c *CiscoBase) ConfigMode() error {
	inConfig, err := c.CheckConfigMode()
	if err == nil && inConfig {
		return nil
	}

	if err := c.WriteChannel("configure terminal\n"); err != nil {
		return err
	}
	_, err = c.ReadUntilPattern(regexp.MustCompile(`\(config.*\)#\s*$`), 10*time.Second)
	return err
}

// ExitConfigMode exits configuration mode back to privileged EXEC mode.
func (c *CiscoBase) ExitConfigMode() error {
	inConfig, err := c.CheckConfigMode()
	if err == nil && !inConfig {
		return nil
	}
	if err := c.WriteChannel("end\n"); err != nil {
		return err
	}
	_, err = c.ReadUntilPattern(regexp.MustCompile(`[a-zA-Z0-9_\-\.\:\(\)]+#\s*$`), 10*time.Second)
	return err
}

// CheckConfigMode checks if (config is present in prompt.
func (c *CiscoBase) CheckConfigMode() (bool, error) {
	prompt, err := c.FindPrompt()
	if err != nil {
		return false, err
	}
	return strings.Contains(prompt, "(config"), nil
}

// SaveConfig saves running configuration to startup configuration.
func (c *CiscoBase) SaveConfig() (string, error) {
	return c.SendCommand("copy running-config startup-config\n\n", netmiko.WithReadTimeout(30*time.Second))
}

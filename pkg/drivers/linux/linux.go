package linux

import (
	"context"
	"strings"

	"github.com/netmiko-go/netmiko/pkg/netmiko"
)

// LinuxSSH implements the Netmiko driver for general Linux / Unix servers.
type LinuxSSH struct {
	*netmiko.BaseConnection
}

func NewLinuxSSH(dev netmiko.Device) *LinuxSSH {
	bc := netmiko.NewBaseConnection(dev)
	bc.DefaultPromptTerm = "$"
	return &LinuxSSH{BaseConnection: bc}
}

var _ netmiko.Connection = (*LinuxSSH)(nil)

func (l *LinuxSSH) Connect(ctx context.Context) error {
	if err := l.BaseConnection.Connect(ctx); err != nil {
		return err
	}
	if _, err := l.SetBasePrompt(); err != nil {
		return err
	}
	// Keep only "user@host": the part after ":" is the working directory and changes with cd.
	if i := strings.Index(l.BasePrompt, ":"); i > 0 {
		l.BasePrompt = l.BasePrompt[:i]
	}
	_, _ = l.SendCommand("stty -echo")
	_, _ = l.SendCommand("stty cols 500 rows 500")
	return nil
}

// SendConfigSet is not supported on Linux hosts; use SendCommand instead.
func (l *LinuxSSH) SendConfigSet(commands []string, opts ...netmiko.ConfigOption) (string, error) {
	return "", netmiko.ErrNotSupported
}

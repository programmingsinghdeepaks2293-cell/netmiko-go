package linux

import (
	"context"

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

func (l *LinuxSSH) Connect(ctx context.Context) error {
	if err := l.BaseConnection.Connect(ctx); err != nil {
		return err
	}
	if _, err := l.SetBasePrompt(); err != nil {
		return err
	}
	_, _ = l.SendCommand("stty -echo")
	_, _ = l.SendCommand("stty cols 500 rows 500")
	return nil
}

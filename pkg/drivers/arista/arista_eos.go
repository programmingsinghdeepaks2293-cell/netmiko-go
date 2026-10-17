package arista

import (
	"context"

	"github.com/netmiko-go/netmiko/pkg/drivers/cisco"
	"github.com/netmiko-go/netmiko/pkg/netmiko"
)

// AristaEOS implements the Netmiko driver for Arista EOS switches.
type AristaEOS struct {
	*cisco.CiscoBase
}

func NewAristaEOS(dev netmiko.Device) *AristaEOS {
	return &AristaEOS{
		CiscoBase: cisco.NewCiscoBase(dev),
	}
}

func (a *AristaEOS) Connect(ctx context.Context) error {
	if err := a.BaseConnection.Connect(ctx); err != nil {
		return err
	}
	if _, err := a.SetBasePrompt(); err != nil {
		return err
	}
	_ = a.DisablePaging()
	return nil
}

func (a *AristaEOS) DisablePaging() error {
	_, err := a.SendCommand("terminal length 0")
	if err != nil {
		return err
	}
	_, err = a.SendCommand("terminal width 32767")
	return err
}

package cisco

import (
	"context"

	"github.com/netmiko-go/netmiko/pkg/netmiko"
)

// CiscoIOS implements the driver for standard Cisco IOS and IOS-XE devices.
type CiscoIOS struct {
	*CiscoBase
}

func NewCiscoIOS(dev netmiko.Device) *CiscoIOS {
	return &CiscoIOS{
		CiscoBase: NewCiscoBase(dev),
	}
}

func (ios *CiscoIOS) Connect(ctx context.Context) error {
	if err := ios.BaseConnection.Connect(ctx); err != nil {
		return err
	}
	return ios.SessionPreparation()
}

// CiscoNXOS implements the driver for Cisco Nexus (NX-OS) switches.
type CiscoNXOS struct {
	*CiscoBase
}

func NewCiscoNXOS(dev netmiko.Device) *CiscoNXOS {
	return &CiscoNXOS{
		CiscoBase: NewCiscoBase(dev),
	}
}

func (nxos *CiscoNXOS) Connect(ctx context.Context) error {
	if err := nxos.BaseConnection.Connect(ctx); err != nil {
		return err
	}
	if _, err := nxos.SetBasePrompt(); err != nil {
		return err
	}
	_, _ = nxos.SendCommand("terminal length 0")
	return nil
}

// CiscoXR implements the driver for Cisco IOS-XR routers.
type CiscoXR struct {
	*CiscoBase
}

func NewCiscoXR(dev netmiko.Device) *CiscoXR {
	return &CiscoXR{
		CiscoBase: NewCiscoBase(dev),
	}
}

func (xr *CiscoXR) Connect(ctx context.Context) error {
	if err := xr.BaseConnection.Connect(ctx); err != nil {
		return err
	}
	if _, err := xr.SetBasePrompt(); err != nil {
		return err
	}
	_, _ = xr.SendCommand("terminal length 0")
	_, _ = xr.SendCommand("terminal width 511")
	return nil
}

// Commit applies configuration changes on IOS-XR.
func (xr *CiscoXR) Commit() (string, error) {
	return xr.SendCommand("commit")
}

// Compile-time checks that every driver satisfies netmiko.Connection.
var (
	_ netmiko.Connection = (*CiscoIOS)(nil)
	_ netmiko.Connection = (*CiscoNXOS)(nil)
	_ netmiko.Connection = (*CiscoXR)(nil)
)

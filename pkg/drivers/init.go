package drivers

import (
	"github.com/netmiko-go/netmiko/pkg/drivers/arista"
	"github.com/netmiko-go/netmiko/pkg/drivers/cisco"
	"github.com/netmiko-go/netmiko/pkg/drivers/juniper"
	"github.com/netmiko-go/netmiko/pkg/drivers/linux"
	"github.com/netmiko-go/netmiko/pkg/netmiko"
)

func init() {
	// Cisco drivers
	ciscoIOSFactory := func(dev netmiko.Device) netmiko.Connection {
		return cisco.NewCiscoIOS(dev)
	}
	netmiko.RegisterDriver("cisco_ios", ciscoIOSFactory)
	netmiko.RegisterDriver("cisco_xe", ciscoIOSFactory)

	netmiko.RegisterDriver("cisco_nxos", func(dev netmiko.Device) netmiko.Connection {
		return cisco.NewCiscoNXOS(dev)
	})

	netmiko.RegisterDriver("cisco_xr", func(dev netmiko.Device) netmiko.Connection {
		return cisco.NewCiscoXR(dev)
	})

	// Arista EOS
	netmiko.RegisterDriver("arista_eos", func(dev netmiko.Device) netmiko.Connection {
		return arista.NewAristaEOS(dev)
	})

	// Juniper JunOS
	junosFactory := func(dev netmiko.Device) netmiko.Connection {
		return juniper.NewJuniperJunos(dev)
	}
	netmiko.RegisterDriver("juniper_junos", junosFactory)
	netmiko.RegisterDriver("juniper", junosFactory)

	// Linux
	netmiko.RegisterDriver("linux", func(dev netmiko.Device) netmiko.Connection {
		return linux.NewLinuxSSH(dev)
	})
}

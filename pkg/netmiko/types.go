package netmiko

import (
	"context"
	"io"
	"regexp"
	"time"
)

// Device contains all parameters required to establish and manage a connection.
type Device struct {
	DeviceType               string        `json:"device_type" yaml:"device_type"`
	Host                     string        `json:"host" yaml:"host"`
	Port                     int           `json:"port" yaml:"port"`
	Username                 string        `json:"username" yaml:"username"`
	Password                 string        `json:"password" yaml:"password"`
	Secret                   string        `json:"secret,omitempty" yaml:"secret,omitempty"` // Enable password
	Timeout                  time.Duration `json:"timeout" yaml:"timeout"`
	GlobalDelayFactor        float64       `json:"global_delay_factor" yaml:"global_delay_factor"`
	SessionLog               io.Writer     `json:"-" yaml:"-"`
	FastCLI                  bool          `json:"fast_cli" yaml:"fast_cli"`
	SSHKeyPath               string        `json:"ssh_key_path,omitempty" yaml:"ssh_key_path,omitempty"`
	SSHStrictHostKeyChecking bool          `json:"ssh_strict_host_key_checking" yaml:"ssh_strict_host_key_checking"`
	SSHKnownHostsFile        string        `json:"ssh_known_hosts_file,omitempty" yaml:"ssh_known_hosts_file,omitempty"`
}

// DefaultDevice returns a Device struct with reasonable defaults.
func DefaultDevice() Device {
	return Device{
		Port:              22,
		Timeout:           30 * time.Second,
		GlobalDelayFactor: 1.0,
	}
}

// CommandOptions configures behavior for SendCommand.
type CommandOptions struct {
	ExpectPattern *regexp.Regexp
	ReadTimeout   time.Duration
	DelayFactor   float64
	StripPrompt   bool
	StripCommand  bool
	CmdVerify     bool
}

// CommandOption is a functional option for configuring SendCommand.
type CommandOption func(*CommandOptions)

func WithExpectPattern(re *regexp.Regexp) CommandOption {
	return func(o *CommandOptions) {
		o.ExpectPattern = re
	}
}

func WithReadTimeout(d time.Duration) CommandOption {
	return func(o *CommandOptions) {
		o.ReadTimeout = d
	}
}

func WithDelayFactor(f float64) CommandOption {
	return func(o *CommandOptions) {
		o.DelayFactor = f
	}
}

func WithStripPrompt(b bool) CommandOption {
	return func(o *CommandOptions) {
		o.StripPrompt = b
	}
}

func WithStripCommand(b bool) CommandOption {
	return func(o *CommandOptions) {
		o.StripCommand = b
	}
}

// ConfigOptions configures behavior for SendConfigSet.
type ConfigOptions struct {
	ConfigModeCommand string
	ExitConfigCommand string
	DelayFactor       float64
	ReadTimeout       time.Duration
	StripPrompt       bool
}

// ConfigOption is a functional option for configuring SendConfigSet.
type ConfigOption func(*ConfigOptions)

func WithConfigModeCommand(cmd string) ConfigOption {
	return func(o *ConfigOptions) {
		o.ConfigModeCommand = cmd
	}
}

func WithExitConfigCommand(cmd string) ConfigOption {
	return func(o *ConfigOptions) {
		o.ExitConfigCommand = cmd
	}
}

// Connection defines the standard Netmiko driver operations across all network vendors.
type Connection interface {
	// Connect establishes the underlying transport and interactive channel.
	Connect(ctx context.Context) error
	// Disconnect gracefully terminates the interactive session and closes connection.
	Disconnect() error

	// Prompt and privilege management
	FindPrompt() (string, error)
	SetBasePrompt() (string, error)
	GetBasePrompt() string
	Enable() error
	ExitEnableMode() error
	CheckEnableMode() (bool, error)

	// Configuration mode management
	ConfigMode() error
	ExitConfigMode() error
	CheckConfigMode() (bool, error)

	// Command execution
	SendCommand(command string, opts ...CommandOption) (string, error)
	SendCommandTiming(command string, idleTimeout time.Duration, opts ...CommandOption) (string, error)
	SendConfigSet(commands []string, opts ...ConfigOption) (string, error)

	// Low-level channel I/O
	WriteChannel(data string) error
	ReadChannel() (string, error)
	ReadUntilPattern(pattern *regexp.Regexp, timeout time.Duration) (string, error)
	ReadUntilPrompt(timeout time.Duration) (string, error)

	// Device parameters
	GetDevice() Device
}

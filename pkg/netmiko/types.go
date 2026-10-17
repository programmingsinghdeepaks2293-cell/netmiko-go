package netmiko

import (
	"context"
	"errors"
	"io"
	"regexp"
	"time"
)

// ErrNotSupported is returned by operations a driver does not implement.
var ErrNotSupported = errors.New("operation not supported by this driver")

// Device contains all parameters required to establish and manage a connection.
type Device struct {
	DeviceType string `json:"device_type" yaml:"device_type"`
	Host       string `json:"host" yaml:"host"`
	Port       int    `json:"port" yaml:"port"`
	Username   string `json:"username" yaml:"username"`
	// Password and Secret are kept out of JSON so they are not serialized by accident.
	Password          string        `json:"-" yaml:"password"`
	Secret            string        `json:"-" yaml:"secret,omitempty"` // Enable password
	Timeout           time.Duration `json:"timeout" yaml:"timeout"`
	GlobalDelayFactor float64       `json:"global_delay_factor" yaml:"global_delay_factor"`
	// SessionLog receives raw session traffic. The enable secret is never written to it.
	SessionLog io.Writer `json:"-" yaml:"-"`
	SSHKeyPath string    `json:"ssh_key_path,omitempty" yaml:"ssh_key_path,omitempty"`
	// KnownHostsPath is the OpenSSH known_hosts file used to verify the device's host key.
	// If empty, ~/.ssh/known_hosts is used.
	KnownHostsPath string `json:"known_hosts_path,omitempty" yaml:"known_hosts_path,omitempty"`
	// InsecureIgnoreHostKey disables host key verification. Only use it on networks you trust.
	InsecureIgnoreHostKey bool `json:"insecure_ignore_host_key,omitempty" yaml:"insecure_ignore_host_key,omitempty"`
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
	StripPrompt   bool
	StripCommand  bool
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

func WithConfigReadTimeout(d time.Duration) ConfigOption {
	return func(o *ConfigOptions) {
		o.ReadTimeout = d
	}
}

// Connection defines the standard Netmiko driver operations across all network vendors.
// Operations a driver does not support return ErrNotSupported.
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
	SendCommandStructured(command string, template ...string) ([]map[string]any, error)
	SendConfigSet(commands []string, opts ...ConfigOption) (string, error)

	// Commit and persistence (ErrNotSupported on drivers without them)
	Commit() (string, error)
	SaveConfig() (string, error)

	// Low-level channel I/O
	WriteChannel(data string) error
	ReadChannel() (string, error)
	ReadUntilPattern(pattern *regexp.Regexp, timeout time.Duration) (string, error)
	ReadUntilPrompt(timeout time.Duration) (string, error)

	// Device parameters
	GetDevice() Device
}

// Compile-time check that the base implementation satisfies the interface.
var _ Connection = (*BaseConnection)(nil)

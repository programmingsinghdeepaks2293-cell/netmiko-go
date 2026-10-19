package mock

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"time"
)

// MockTerminal simulates an interactive network CLI session over an in-memory transport.
type MockTerminal struct {
	mu           sync.Mutex
	inBuf        bytes.Buffer
	outBuf       bytes.Buffer
	Prompt       string
	Hostname     string
	Linux        bool
	InPrivileged bool
	InConfig     bool
	closed       bool
	readNotify   chan struct{}
}

// NewMockCiscoTerminal creates a mock terminal pre-configured like a Cisco IOS switch.
func NewMockCiscoTerminal(hostname string) *MockTerminal {
	m := &MockTerminal{
		Hostname:     hostname,
		InPrivileged: false,
		InConfig:     false,
		readNotify:   make(chan struct{}, 100),
	}
	m.updatePrompt()
	m.emit(m.Prompt)
	return m
}

func NewMockLinuxTerminal(username, hostname string) *MockTerminal {
	m := &MockTerminal{
		Hostname:   hostname,
		Linux:      true,
		Prompt:     username + "@" + hostname + ":~$ ",
		readNotify: make(chan struct{}, 100),
	}
	m.emit(m.Prompt)
	return m
}

func (m *MockTerminal) updatePrompt() {
	if m.Linux {
		return
	}
	if m.InConfig {
		m.Prompt = m.Hostname + "(config)# "
	} else if m.InPrivileged {
		m.Prompt = m.Hostname + "# "
	} else {
		m.Prompt = m.Hostname + "> "
	}
}

func (m *MockTerminal) emit(data string) {
	m.outBuf.WriteString(data)
	select {
	case m.readNotify <- struct{}{}:
	default:
	}
}

func (m *MockTerminal) Read(p []byte) (n int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.outBuf.Len() == 0 && !m.closed {
		m.mu.Unlock()
		select {
		case <-m.readNotify:
		case <-time.After(50 * time.Millisecond):
		}
		m.mu.Lock()
	}

	if m.outBuf.Len() == 0 {
		if m.closed {
			return 0, io.EOF
		}
		return 0, nil
	}

	return m.outBuf.Read(p)
}

func (m *MockTerminal) Write(p []byte) (n int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return 0, io.ErrClosedPipe
	}

	m.inBuf.Write(p)
	data := m.inBuf.String()

	// Check if a newline was sent (command completed)
	if strings.Contains(data, "\n") {
		parts := strings.Split(data, "\n")
		// The last part might be incomplete input
		m.inBuf.Reset()
		m.inBuf.WriteString(parts[len(parts)-1])

		for _, cmdLine := range parts[:len(parts)-1] {
			cmd := strings.TrimSpace(cmdLine)
			m.handleCommand(cmd)
		}
	}

	return len(p), nil
}

func (m *MockTerminal) handleCommand(cmd string) {
	// Echo the command back as terminal emulator would
	m.emit(cmd + "\r\n")

	switch {
	case cmd == "":
		m.emit("\r\n" + m.Prompt)
	case cmd == "enable":
		if m.InPrivileged {
			m.emit(m.Prompt)
		} else {
			m.emit("Password: ")
		}
	case cmd == "cisco" || cmd == "secret": // password entered
		m.InPrivileged = true
		m.updatePrompt()
		m.emit("\r\n" + m.Prompt)
	case cmd == "disable":
		m.InPrivileged = false
		m.InConfig = false
		m.updatePrompt()
		m.emit(m.Prompt)
	case cmd == "configure terminal" || cmd == "conf t":
		m.InConfig = true
		m.updatePrompt()
		m.emit("Enter configuration commands, one per line.  End with CNTL/Z.\r\n" + m.Prompt)
	case cmd == "end" || cmd == "exit":
		if m.InConfig {
			m.InConfig = false
			m.updatePrompt()
			m.emit(m.Prompt)
		} else {
			m.emit(m.Prompt)
		}
	case strings.HasPrefix(cmd, "show ip int br") || strings.HasPrefix(cmd, "show ip interface brief"):
		m.emit("Interface              IP-Address      OK? Method Status                Protocol\r\n")
		m.emit("GigabitEthernet0/0     192.168.1.1     YES manual up                    up\r\n")
		m.emit("GigabitEthernet0/1     unassigned      YES unset  administratively down down\r\n")
		m.emit(m.Prompt)
	case cmd == "show version":
		m.emit("Cisco IOS Software, C2960 Software\r\n")
		m.emit(m.Prompt)
	case strings.HasPrefix(cmd, "terminal length"):
		m.emit(m.Prompt)
	case strings.HasPrefix(cmd, "terminal width"):
		m.emit(m.Prompt)
	case cmd == "pwd" && m.Linux:
		m.emit("/home/" + strings.SplitN(m.Prompt, "@", 2)[0] + "\r\n" + m.Prompt)
	default:
		// Generic command response
		m.emit("Output for " + cmd + "\r\n" + m.Prompt)
	}
}

func (m *MockTerminal) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	select {
	case m.readNotify <- struct{}{}:
	default:
	}
	return nil
}

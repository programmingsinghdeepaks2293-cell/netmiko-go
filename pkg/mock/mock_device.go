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
	InPrivileged bool
	InConfig     bool
	// FixedPrompt, when set, is used verbatim as the prompt (e.g. "ubuntu@srv:~$ ").
	FixedPrompt string
	// Junos switches the terminal to JunOS-style prompts and commands.
	Junos bool

	awaitingPassword bool
	closed           bool
	readNotify       chan struct{}
}

func newMockTerminal(hostname string) *MockTerminal {
	return &MockTerminal{
		Hostname:   hostname,
		readNotify: make(chan struct{}, 100),
	}
}

// NewMockCiscoTerminal creates a mock terminal pre-configured like a Cisco IOS switch.
func NewMockCiscoTerminal(hostname string) *MockTerminal {
	m := newMockTerminal(hostname)
	m.updatePrompt()
	m.emit(m.Prompt)
	return m
}

// NewMockJunosTerminal creates a mock terminal that behaves like a JunOS CLI.
// Pass the login and host as hostname, e.g. "user@junos-router".
func NewMockJunosTerminal(hostname string) *MockTerminal {
	m := newMockTerminal(hostname)
	m.Junos = true
	m.updatePrompt()
	m.emit(m.Prompt)
	return m
}

// NewMockTerminalWithPrompt creates a mock terminal that always shows the given prompt.
func NewMockTerminalWithPrompt(prompt string) *MockTerminal {
	m := newMockTerminal("")
	m.FixedPrompt = prompt
	m.updatePrompt()
	m.emit(m.Prompt)
	return m
}

func (m *MockTerminal) updatePrompt() {
	switch {
	case m.FixedPrompt != "":
		m.Prompt = m.FixedPrompt
	case m.Junos && m.InConfig:
		m.Prompt = m.Hostname + "# "
	case m.Junos:
		m.Prompt = m.Hostname + "> "
	case m.InConfig:
		m.Prompt = m.Hostname + "(config)# "
	case m.InPrivileged:
		m.Prompt = m.Hostname + "# "
	default:
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
	if m.outBuf.Len() == 0 && !m.closed {
		m.mu.Unlock()
		select {
		case <-m.readNotify:
		case <-time.After(50 * time.Millisecond):
		}
		m.mu.Lock()
	}
	defer m.mu.Unlock()

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

	// Process each complete line; keep any trailing partial line buffered.
	if strings.Contains(data, "\n") {
		parts := strings.Split(data, "\n")
		m.inBuf.Reset()
		m.inBuf.WriteString(parts[len(parts)-1])

		for _, cmdLine := range parts[:len(parts)-1] {
			m.handleCommand(strings.TrimSpace(cmdLine))
		}
	}

	return len(p), nil
}

func (m *MockTerminal) handleCommand(cmd string) {
	if m.awaitingPassword {
		// Real devices do not echo the enable secret back.
		m.awaitingPassword = false
		m.InPrivileged = true
		m.updatePrompt()
		m.emit("\r\n" + m.Prompt)
		return
	}

	// Echo the command back as a terminal emulator would
	m.emit(cmd + "\r\n")

	if m.Junos {
		m.handleJunos(cmd)
		return
	}

	switch {
	case cmd == "":
		m.emit("\r\n" + m.Prompt)
	case cmd == "enable":
		if m.InPrivileged {
			m.emit(m.Prompt)
		} else {
			m.awaitingPassword = true
			m.emit("Password: ")
		}
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
		}
		m.emit(m.Prompt)
	case strings.HasPrefix(cmd, "show ip int br") || strings.HasPrefix(cmd, "show ip interface brief"):
		m.emit("Interface              IP-Address      OK? Method Status                Protocol\r\n")
		m.emit("GigabitEthernet0/0     192.168.1.1     YES manual up                    up\r\n")
		m.emit("GigabitEthernet0/1     unassigned      YES unset  administratively down down\r\n")
		m.emit(m.Prompt)
	case strings.HasPrefix(cmd, "terminal length"):
		m.emit(m.Prompt)
	case strings.HasPrefix(cmd, "terminal width"):
		m.emit(m.Prompt)
	default:
		// Generic command response
		m.emit("Output for " + cmd + "\r\n" + m.Prompt)
	}
}

// handleJunos implements the subset of JunOS CLI behavior the drivers rely on.
func (m *MockTerminal) handleJunos(cmd string) {
	switch {
	case cmd == "":
		m.emit("\r\n" + m.Prompt)
	case cmd == "configure":
		m.InConfig = true
		m.updatePrompt()
		m.emit("Entering configuration mode\r\n" + m.Prompt)
	case cmd == "exit" || cmd == "quit":
		if m.InConfig {
			m.InConfig = false
			m.updatePrompt()
		}
		m.emit(m.Prompt)
	default:
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

package tui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
)

// shellOutputMsg carries data read from the PTY.
type shellOutputMsg []byte

// shellExitMsg signals the shell process has exited.
type shellExitMsg struct{}

// shellSpecInfo bundles per-spec context for spawning a shell. Env carries
// the spec's full isolation env (HOME, <PREFIX>_HOME, PATH with binDir, …)
// captured from the JSONL spec_start record so the shell sees exactly what
// the test saw.
type shellSpecInfo struct {
	Name    string
	Wave    string
	WorkDir string
	HomeDir string
	BinDir  string
	Env     map[string]string
}

// shellModel manages an embedded interactive shell. On platforms where
// creack/pty does not work, the shell pane is unavailable and the TUI shows
// a fallback message instead.
type shellModel struct {
	ptmx     *os.File
	cmd      *exec.Cmd
	emulator *vt.Emulator
	specName string // "wave/spec"
	width    int
	height   int
	exited   bool
	mu       sync.Mutex
}

// newShellFromEntry spawns a shell for a test entry. Returns the shell model
// and an initial Cmd that reads the first chunk of PTY output.
func newShellFromEntry(info *shellSpecInfo, width, height int) (*shellModel, tea.Cmd, error) {
	if width < 2 {
		width = 80
	}
	if height < 2 {
		height = 24
	}

	shellPath := os.Getenv("SHELL")
	if shellPath == "" {
		shellPath = "/bin/bash"
	}

	cmd := exec.Command(shellPath)
	cmd.Dir = info.WorkDir

	// Build env: start from the spec's recorded env (which already has HOME,
	// <PREFIX>_HOME, PATH, GIT_*, TERM=dumb), then override TERM to a real
	// terminal type and set a recognizable PS1.
	env := []string{
		fmt.Sprintf("PS1=(gotit:%s) $ ", info.Name),
		"TERM=xterm-256color", // override TERM=dumb that the spec ran with
	}
	if info.Env != nil {
		for k, v := range info.Env {
			if k == "TERM" {
				continue // already overridden above
			}
			env = append(env, k+"="+v)
		}
	}
	// Pass through user/locale vars so prompts and tab-completion behave.
	for _, key := range []string{"LANG", "LC_ALL", "USER", "LOGNAME"} {
		if v := os.Getenv(key); v != "" {
			env = append(env, key+"="+v)
		}
	}
	cmd.Env = env

	ws := &pty.Winsize{Rows: uint16(height), Cols: uint16(width)}
	ptmx, err := pty.StartWithSize(cmd, ws)
	if err != nil {
		return nil, nil, fmt.Errorf("start shell: %w", err)
	}

	em := vt.NewEmulator(width, height)

	m := &shellModel{
		ptmx:     ptmx,
		cmd:      cmd,
		emulator: em,
		specName: info.Wave + "/" + info.Name,
		width:    width,
		height:   height,
	}
	return m, m.waitForOutput(), nil
}

func (m *shellModel) waitForOutput() tea.Cmd {
	return func() tea.Msg {
		buf := make([]byte, 4096)
		n, err := m.ptmx.Read(buf)
		if err != nil {
			return shellExitMsg{}
		}
		return shellOutputMsg(buf[:n])
	}
}

func (m *shellModel) handleOutput(data shellOutputMsg) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.emulator.Write([]byte(data))
}

func (m *shellModel) sendKey(msg tea.KeyMsg) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.exited {
		return
	}
	data := keyToBytes(msg)
	if len(data) > 0 {
		m.ptmx.Write(data)
	}
}

func (m *shellModel) resize(width, height int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if width < 2 || height < 2 {
		return
	}
	m.width = width
	m.height = height
	pty.Setsize(m.ptmx, &pty.Winsize{Rows: uint16(height), Cols: uint16(width)})
	m.emulator.Resize(width, height)
}

func (m *shellModel) View() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.exited {
		return shellExitedStyle.Render("(shell exited)")
	}
	return m.emulator.Render()
}

// FallbackShellHint returns a message describing how to inspect a spec's
// preserved temp HOME from an external terminal. Used when the embedded
// shell isn't available (e.g. Windows without ConPTY support).
func FallbackShellHint(workDir string) string {
	if workDir == "" {
		return "(no preserved work dir for this spec)"
	}
	return strings.TrimSpace(fmt.Sprintf(`
Embedded shell unavailable on this platform.

To inspect this spec's run, open a separate terminal and:
    cd %s
    # the binary is on PATH; the runner's env vars are in the JSONL log
`, workDir))
}

func (m *shellModel) Close() {
	if m.cmd.Process != nil {
		m.cmd.Process.Signal(syscall.SIGHUP)
		done := make(chan struct{})
		go func() {
			m.cmd.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			m.cmd.Process.Kill()
			<-done
		}
	}
	if m.ptmx != nil {
		m.ptmx.Close()
	}
	if m.emulator != nil {
		m.emulator.Close()
	}
	m.exited = true
}

func keyToBytes(msg tea.KeyMsg) []byte {
	switch msg.Type {
	case tea.KeyEnter:
		return []byte{'\r'}
	case tea.KeyBackspace:
		return []byte{0x7f}
	case tea.KeyTab:
		return []byte{'\t'}
	case tea.KeyEscape:
		return []byte{0x1b}
	case tea.KeyUp:
		return []byte{0x1b, '[', 'A'}
	case tea.KeyDown:
		return []byte{0x1b, '[', 'B'}
	case tea.KeyRight:
		return []byte{0x1b, '[', 'C'}
	case tea.KeyLeft:
		return []byte{0x1b, '[', 'D'}
	case tea.KeyHome:
		return []byte{0x1b, '[', 'H'}
	case tea.KeyEnd:
		return []byte{0x1b, '[', 'F'}
	case tea.KeyDelete:
		return []byte{0x1b, '[', '3', '~'}
	case tea.KeyCtrlA:
		return []byte{0x01}
	case tea.KeyCtrlB:
		return []byte{0x02}
	case tea.KeyCtrlC:
		return []byte{0x03}
	case tea.KeyCtrlD:
		return []byte{0x04}
	case tea.KeyCtrlE:
		return []byte{0x05}
	case tea.KeyCtrlF:
		return []byte{0x06}
	case tea.KeyCtrlG:
		return []byte{0x07}
	case tea.KeyCtrlK:
		return []byte{0x0b}
	case tea.KeyCtrlL:
		return []byte{0x0c}
	case tea.KeyCtrlN:
		return []byte{0x0e}
	case tea.KeyCtrlO:
		return []byte{0x0f}
	case tea.KeyCtrlP:
		return []byte{0x10}
	case tea.KeyCtrlR:
		return []byte{0x12}
	case tea.KeyCtrlS:
		return []byte{0x13}
	case tea.KeyCtrlT:
		return []byte{0x14}
	case tea.KeyCtrlU:
		return []byte{0x15}
	case tea.KeyCtrlW:
		return []byte{0x17}
	case tea.KeyCtrlZ:
		return []byte{0x1a}
	case tea.KeySpace:
		return []byte{' '}
	default:
		if len(msg.Runes) > 0 {
			return []byte(string(msg.Runes))
		}
		return nil
	}
}

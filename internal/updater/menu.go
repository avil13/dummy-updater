package updater

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"unicode"
	"unicode/utf8"
)

// stty is available on both supported platforms; no terminal dependency is needed.
func terminalMode(input *os.File) (enter, restore func() error, err error) {
	stty := func(args ...string) (string, error) {
		cmd := exec.Command("stty", args...)
		cmd.Stdin = input
		data, err := cmd.Output()
		return strings.TrimSpace(string(data)), err
	}
	state, err := stty("-g")
	if err != nil {
		return nil, nil, fmt.Errorf("ls requires an interactive terminal and stty: %w", err)
	}
	enter = func() error {
		_, err := stty("-icanon", "-echo", "-isig", "-ixon", "min", "0", "time", "1")
		return err
	}
	restore = func() error { _, err := stty(state); return err }
	return enter, restore, nil
}

type menuUI struct {
	ctx    context.Context
	input  io.Reader
	output io.Writer
	poll   bool // Terminal reads time out every 100 ms, allowing cancellation and bare Esc.
}

func (m *menuUI) readByte(wait bool) (byte, error) {
	var b [1]byte
	for {
		if err := m.ctx.Err(); err != nil {
			return 0, err
		}
		n, err := m.input.Read(b[:])
		if n > 0 {
			if b[0] == 3 {
				return 0, context.Canceled
			}
			return b[0], nil
		}
		if m.poll && (err == nil || errors.Is(err, io.EOF)) {
			if wait {
				continue
			}
			return 0, io.EOF
		}
		return 0, err
	}
}

func (m *menuUI) key() (string, error) {
	b, err := m.readByte(true)
	if err != nil {
		return "", err
	}
	switch b {
	case 27:
		next, err := m.readByte(false)
		if errors.Is(err, io.EOF) {
			return "back", nil
		}
		if err != nil {
			return "", err
		}
		if next == '[' || next == 'O' {
			last, err := m.readByte(false)
			if err != nil && !errors.Is(err, io.EOF) {
				return "", err
			}
			switch last {
			case 'A':
				return "up", nil
			case 'B':
				return "down", nil
			}
			return "", nil
		}
		return "back", nil
	case '\r', '\n':
		return "enter", nil
	case 127, 8:
		return "erase", nil
	default:
		return string([]byte{b}), nil
	}
}

func menuText(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}

func (m *menuUI) choose(title string, choices []string, selected int) (int, error) {
	for {
		var frame strings.Builder
		frame.WriteString("\033[H\033[2J" + menuText(title) + "\n↑/↓ (j/k) — выбор; Enter — открыть; q/Esc — назад\n\n")
		start := selected / 12 * 12
		for i := start; i < min(start+12, len(choices)); i++ {
			marker := "  "
			if i == selected {
				marker = "> "
			}
			fmt.Fprintf(&frame, "%s%d) %s\n", marker, i+1, menuText(choices[i]))
		}
		if _, err := io.WriteString(m.output, frame.String()); err != nil {
			return -1, err
		}
		key, err := m.key()
		if err != nil {
			return -1, err
		}
		switch key {
		case "q", "back":
			return -1, nil
		case "up", "k":
			selected = (selected + len(choices) - 1) % len(choices)
		case "down", "j":
			selected = (selected + 1) % len(choices)
		case "enter":
			return selected, nil
		}
	}
}

var errEditCanceled = errors.New("editing canceled")

func (m *menuUI) ask(prompt string) (string, error) {
	if _, err := fmt.Fprint(m.output, menuText(prompt)); err != nil {
		return "", err
	}
	var value []byte
	for {
		key, err := m.key()
		if err != nil {
			return "", err
		}
		switch key {
		case "back":
			return "", errEditCanceled
		case "enter":
			_, err := fmt.Fprintln(m.output)
			return string(value), err
		case "erase":
			if len(value) > 0 {
				_, size := utf8.DecodeLastRune(value)
				value = value[:len(value)-size]
			}
		default:
			if len(key) == 1 && key[0] >= 32 {
				value = append(value, key[0])
			}
		}
		if _, err := fmt.Fprint(m.output, "\r\033[2K", menuText(prompt), menuText(string(value))); err != nil {
			return "", err
		}
	}
}

func (m *menuUI) view(title, content string) error {
	lines := strings.Split(content, "\n")
	start := 0
	for {
		var frame strings.Builder
		fmt.Fprintf(&frame, "\033[H\033[2J%s\n↑/↓ (j/k) — прокрутка; Enter/q/Esc — назад\n\n", menuText(title))
		for _, line := range lines[start:min(start+16, len(lines))] {
			frame.WriteString(menuText(line) + "\n")
		}
		if _, err := io.WriteString(m.output, frame.String()); err != nil {
			return err
		}
		key, err := m.key()
		if err != nil {
			return err
		}
		switch key {
		case "q", "back", "enter":
			return nil
		case "up", "k":
			start = max(0, start-1)
		case "down", "j":
			start = min(max(0, len(lines)-16), start+1)
		}
	}
}

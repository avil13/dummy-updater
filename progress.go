package main

import (
	"fmt"
	"io"
	"strings"
	"sync"
)

// projectOutput keeps only the visible tail; the transcript is handled separately.
type projectOutput struct {
	mu       sync.Mutex
	out      io.Writer
	lines    []string
	line     []byte
	escape   byte
	carriage bool
}

func newProjectOutput(out io.Writer) *projectOutput {
	fmt.Fprint(out, strings.Repeat("\n", 10))
	return &projectOutput{out: out}
}

func (p *projectOutput) Write(data []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, b := range data {
		// Strip command-supplied terminal controls so they cannot escape the block.
		switch p.escape {
		case 'e':
			p.escape = 0
			if b == '[' {
				p.escape = 'c'
			}
			if b == ']' {
				p.escape = 'o'
			}
			continue
		case 'c':
			if b >= 0x40 && b <= 0x7e {
				p.escape = 0
			}
			continue
		case 'o':
			if b == 7 {
				p.escape = 0
			}
			if b == 27 {
				p.escape = 's'
			}
			continue
		case 's':
			p.escape = 'o'
			if b == '\\' {
				p.escape = 0
			}
			continue
		}
		if p.carriage && (b >= 32 || b == '\t') {
			p.line = nil
			p.carriage = false
		}
		switch {
		case b == 27:
			p.escape = 'e'
		case b == '\n':
			p.lines = append(p.lines, string(p.line))
			if len(p.lines) > 10 {
				p.lines = p.lines[1:]
			}
			p.line = nil
			p.carriage = false
		case b == '\r':
			p.carriage = true
		case b == '\t':
			if len(p.line) < 4092 {
				p.line = append(p.line, "    "...)
			}
		case b >= 32 && b != 127:
			if len(p.line) < 4096 {
				p.line = append(p.line, b)
			}
		}
	}
	lines := append([]string(nil), p.lines...)
	if len(p.line) > 0 {
		lines = append(lines, string(p.line))
	}
	if len(lines) > 10 {
		lines = lines[len(lines)-10:]
	}
	var frame strings.Builder
	// Disable wrapping while drawing: long lines must not occupy extra rows.
	frame.WriteString("\033[10A\033[?7l")
	for i := 0; i < 10; i++ {
		frame.WriteString("\r\033[2K")
		if i < len(lines) {
			frame.WriteString(strings.ToValidUTF8(lines[i], "�"))
		}
		frame.WriteByte('\n')
	}
	frame.WriteString("\033[?7h")
	_, err := io.WriteString(p.out, frame.String())
	return len(data), err
}

func (p *projectOutput) finish(target, icon string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	fmt.Fprint(p.out, "\033[11A\r\033[J")
	fmt.Fprintf(p.out, "%s [%s]\n", icon, target)
}

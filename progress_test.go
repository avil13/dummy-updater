package main

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestProjectOutput(t *testing.T) {
	var screen bytes.Buffer
	p := newProjectOutput(&screen)
	var full strings.Builder
	for i := 0; i < 25; i++ {
		fmt.Fprintf(&full, "line %d\n", i)
	}
	p.Write([]byte(full.String()))
	if len(p.lines) != 10 || p.lines[0] != "line 15" || p.lines[9] != "line 24" {
		t.Fatalf("unexpected visible tail: %q", p.lines)
	}
	screen.Reset()
	p.Write([]byte("\033[31"))
	p.Write([]byte("mprogress 10%\rprogress 20%\033[0m\033]title"))
	p.Write([]byte("\033\\\tготово"))
	if string(p.line) != "progress 20%    готово" {
		t.Fatalf("unexpected partial line: %q", p.line)
	}
	if strings.Contains(screen.String(), "\033[31m") || strings.Contains(screen.String(), "title") {
		t.Fatal("command terminal controls leaked into the display")
	}
	p.Write([]byte(strings.Repeat("x", 100000)))
	if len(p.line) > 4096 {
		t.Fatal("unbounded partial line")
	}
	p.Write([]byte("\nWindows line\r"))
	p.Write([]byte("\n"))
	if p.lines[len(p.lines)-1] != "Windows line" {
		t.Fatalf("CRLF lost a line: %q", p.lines)
	}
	screen.Reset()
	p.finish("target", "❌")
	if screen.String() != "\033[11A\r\033[J❌ [target]\n" {
		t.Fatalf("unexpected final status: %q", screen.String())
	}
}

func TestProjectOutputConcurrent(t *testing.T) {
	var screen bytes.Buffer
	p := newProjectOutput(&screen)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				p.Write([]byte("output\n"))
			}
		}()
	}
	wg.Wait()
	if len(p.lines) != 10 {
		t.Fatalf("unexpected tail: %q", p.lines)
	}
}

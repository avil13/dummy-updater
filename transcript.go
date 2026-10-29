package main

import (
	"errors"
	"os"
	"sync"
)

// projectTranscript uses the file as a ring, then restores chronological order on close.
type projectTranscript struct {
	*os.File
	mu    sync.Mutex
	pos   int64
	limit int64
	full  bool
}

func (t *projectTranscript) Write(data []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	written := 0
	if int64(len(data)) >= t.limit {
		written = len(data) - int(t.limit)
		data = data[written:]
		t.pos = 0
	}
	for len(data) > 0 {
		size := min(len(data), int(t.limit-t.pos))
		n, err := t.File.WriteAt(data[:size], t.pos)
		written += n
		t.pos += int64(n)
		if t.pos == t.limit {
			t.pos = 0
			t.full = true
		}
		if err != nil {
			return written, err
		}
		data = data[n:]
	}
	return written, nil
}

func (t *projectTranscript) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	var err error
	if t.full && t.pos != 0 {
		data := make([]byte, t.limit)
		if _, err = t.File.ReadAt(data, 0); err == nil {
			if _, err = t.File.WriteAt(data[t.pos:], 0); err == nil {
				_, err = t.File.WriteAt(data[:t.pos], t.limit-t.pos)
			}
		}
	}
	return errors.Join(err, t.File.Close())
}

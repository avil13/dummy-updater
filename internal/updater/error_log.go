package updater

import (
	"io"
	"os"
	"sync"
)

type errorLog struct {
	*os.File
	mu    sync.Mutex
	limit int64
}

func openErrorLog(path string, limit int64) (*errorLog, error) {
	if limit == 0 {
		return &errorLog{}, nil
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	output := &errorLog{File: file, limit: limit}
	// Also shrink logs left by older versions, even if this run has no errors.
	if _, err := output.Write(nil); err != nil {
		_ = file.Close()
		return nil, err
	}
	return output, nil
}

func (l *errorLog) Write(data []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	size := len(data)
	if l.limit == 0 {
		return size, nil
	}
	if int64(size) > l.limit {
		data = data[size-int(l.limit):]
	}
	info, err := l.Stat()
	if err != nil {
		return 0, err
	}
	if info.Size() > l.limit-int64(len(data)) {
		tail := make([]byte, l.limit-int64(len(data)))
		if len(tail) > 0 {
			if _, err := l.ReadAt(tail, info.Size()-int64(len(tail))); err != nil {
				return 0, err
			}
		}
		if err := l.Truncate(0); err != nil {
			return 0, err
		}
		if _, err := l.WriteAt(tail, 0); err != nil {
			return 0, err
		}
	}
	if _, err := l.Seek(0, io.SeekEnd); err != nil {
		return 0, err
	}
	if _, err := l.File.Write(data); err != nil {
		return 0, err
	}
	return size, nil
}

func (l *errorLog) Close() error {
	if l.File == nil {
		return nil
	}
	return l.File.Close()
}

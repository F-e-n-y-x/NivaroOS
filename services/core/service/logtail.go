package service

import (
	"bytes"
	"io"
	"os"
)

// tailLines returns the last n lines of the file at path, reading backwards
// from the end in blocks - never the whole file (the NivaroOS log grows
// without bound; it was 10.7 MB when the UI started freezing on it).
func tailLines(path string, n int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	end, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return "", err
	}
	const block = 64 << 10
	var buf []byte
	pos := end
	for pos > 0 {
		step := int64(block)
		if pos < step {
			step = pos
		}
		pos -= step
		chunk := make([]byte, step)
		if _, err := f.ReadAt(chunk, pos); err != nil && err != io.EOF {
			return "", err
		}
		buf = append(chunk, buf...)
		// n lines need n newlines after the start of the first one
		// (+1 for the file's trailing newline).
		if bytes.Count(buf, []byte{'\n'}) > n {
			break
		}
	}
	trimmed := bytes.TrimSuffix(buf, []byte{'\n'})
	if i := indexNthFromEnd(trimmed, '\n', n); i >= 0 {
		buf = buf[i+1:]
	}
	return string(buf), nil
}

// indexNthFromEnd is the index of the n-th sep counting from the end, or -1.
func indexNthFromEnd(b []byte, sep byte, n int) int {
	for i := len(b) - 1; i >= 0; i-- {
		if b[i] == sep {
			n--
			if n == 0 {
				return i
			}
		}
	}
	return -1
}

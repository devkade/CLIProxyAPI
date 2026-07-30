package executor

import (
	"bufio"
	"bytes"
	"io"
	"testing"
)

type oneByteReader struct {
	data []byte
}

func (r *oneByteReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	p[0] = r.data[0]
	r.data = r.data[1:]
	return 1, nil
}

func TestReadCodexSSELinePreservesFrameBoundariesAcrossOneByteFragmentation(t *testing.T) {
	stream := []byte("data: {\"type\":\"response.output_item.done\",\"response\":{\"output\":[]}}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\"}}\n\n")
	reader := bufio.NewReader(&oneByteReader{data: append([]byte(nil), stream...)})

	line1, err := readCodexSSELine(reader)
	if err != nil {
		t.Fatalf("line1 err = %v", err)
	}
	if !bytes.Equal(line1, []byte("data: {\"type\":\"response.output_item.done\",\"response\":{\"output\":[]}}\n")) {
		t.Fatalf("line1 = %q", line1)
	}

	line2, err := readCodexSSELine(reader)
	if err != nil {
		t.Fatalf("line2 err = %v", err)
	}
	if !bytes.Equal(line2, []byte("\n")) {
		t.Fatalf("line2 = %q", line2)
	}

	line3, err := readCodexSSELine(reader)
	if err != nil {
		t.Fatalf("line3 err = %v", err)
	}
	if !bytes.Equal(line3, []byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\"}}\n")) {
		t.Fatalf("line3 = %q", line3)
	}

	line4, err := readCodexSSELine(reader)
	if err != nil {
		t.Fatalf("line4 err = %v", err)
	}
	if !bytes.Equal(line4, []byte("\n")) {
		t.Fatalf("line4 = %q", line4)
	}

	line5, err := readCodexSSELine(reader)
	if len(line5) != 0 || err != io.EOF {
		t.Fatalf("line5 = %q, err = %v", line5, err)
	}
}

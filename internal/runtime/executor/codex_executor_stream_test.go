package executor

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	_ "github.com/router-for-me/CLIProxyAPI/v7/internal/translator"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
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

func TestCodexExecutorExecuteStreamPreservesSSEFrameSeparatorsAndDoneFrame(t *testing.T) {
	upstream := []byte("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\"}}\n\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"你好\"}\n\n" +
		"data: {\"type\":\"response.reasoning_summary_text.delta\",\"delta\":\"推理\"}\r\n\r\n" +
		"data: {\"type\":\"response.function_call_arguments.delta\",\"delta\":\"{\\\"city\\\":\\\"北京\\\"}\"}\n\n" +
		"data: {\"type\":\"response.output_text.done\",\"text\":\"完成\"}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"output\":[]}}\r\n\r\n" +
		"data: [DONE]\n\n")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		for _, b := range upstream {
			_, _ = w.Write([]byte{b})
			flusher.Flush()
		}
	}))
	t.Cleanup(server.Close)

	executor := NewCodexExecutor(&config.Config{})
	result, err := executor.ExecuteStream(context.Background(), &cliproxyauth.Auth{Attributes: map[string]string{
		"base_url": server.URL,
		"api_key":  "test",
	}}, cliproxyexecutor.Request{
		Model:   "gpt-5.5",
		Payload: []byte(`{"model":"gpt-5.5","input":"hello"}`),
	}, cliproxyexecutor.Options{
		SourceFormat:   sdktranslator.FormatOpenAIResponse,
		ResponseFormat: sdktranslator.FormatOpenAIResponse,
		Stream:         true,
	})
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}

	var downstream []byte
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			t.Fatalf("stream error = %v", chunk.Err)
		}
		downstream = append(downstream, chunk.Payload...)
	}
	if got, want := bytes.Count(downstream, []byte("\n\n"))+bytes.Count(downstream, []byte("\r\n\r\n")), 7; got != want {
		t.Fatalf("downstream delimiter count = %d, want %d; downstream=%q", got, want, downstream)
	}
	if !bytes.Equal(downstream, upstream) {
		t.Fatalf("downstream bytes differ\n got: %q\nwant: %q", downstream, upstream)
	}
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

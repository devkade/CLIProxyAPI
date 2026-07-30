package responses

import (
	"bytes"
	"context"
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertCodexResponseToOpenAIResponsesPreservesFrameLineEndings(t *testing.T) {
	for _, raw := range [][]byte{
		[]byte("data: {\"type\":\"response.created\"}\n"),
		[]byte("data: {\"type\":\"response.created\"}\r\n"),
		[]byte("\n"),
		[]byte("\r\n"),
	} {
		out := ConvertCodexResponseToOpenAIResponses(context.Background(), "gpt-5.5", nil, nil, raw, nil)
		if len(out) != 1 || !bytes.Equal(out[0], raw) {
			t.Fatalf("ConvertCodexResponseToOpenAIResponses(%q) = %q, want exact input", raw, out)
		}
	}
}

func TestConvertCodexResponseToOpenAIResponsesNonStreamIncomplete(t *testing.T) {
	raw := []byte(`{"type":"response.incomplete","response":{"id":"resp_1","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[],"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}}`)

	out := ConvertCodexResponseToOpenAIResponsesNonStream(context.Background(), "gpt-5.5", nil, nil, raw, nil)

	if got := gjson.GetBytes(out, "status").String(); got != "incomplete" {
		t.Fatalf("status = %q, want incomplete; payload=%s", got, out)
	}
	if got := gjson.GetBytes(out, "incomplete_details.reason").String(); got != "max_output_tokens" {
		t.Fatalf("incomplete reason = %q, want max_output_tokens; payload=%s", got, out)
	}
}

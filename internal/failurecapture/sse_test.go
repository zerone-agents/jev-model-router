package failurecapture

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestStreamRetainsSafeEvidence(t *testing.T) {
	stream := NewStream(1024)
	stream.Feed([]byte("data: {\"type\":\"content_block_start\",\"content_block\":{\"type\":\"server_tool_use\","))
	stream.Feed([]byte("\"input\":\"private-token\"}}\r\n\r\ndata: {\"type\":\"partial-token"))
	stream.Finish("eof")
	encoded, err := json.Marshal(stream)
	if err != nil || strings.Contains(string(encoded), "-token") {
		t.Fatalf("unsafe capture: %s (%v)", encoded, err)
	}
	if len(stream.Frames) != 1 || !stream.Partial || stream.End != "eof" || stream.Terminal != "" ||
		!strings.Contains(string(encoded), "server_tool_use") {
		t.Fatalf("missing stream evidence: %s", encoded)
	}
}

func TestStreamRetainsTerminalAfterBudget(t *testing.T) {
	stream := NewStream(0)
	stream.Feed([]byte("data: " + strings.Repeat("a", maxFrameBytes+1)))
	stream.Feed([]byte("\n\ndata: [DONE]\n\n"))
	stream.Finish("eof")
	if !stream.Truncated || stream.Partial || stream.Terminal != "done" || stream.End != "complete" || len(stream.Frames) != 0 {
		t.Fatalf("incorrect terminal evidence: %+v", stream)
	}
}

func TestStreamProjectsPlaygroundEvents(t *testing.T) {
	stream := NewStream(4096)
	stream.Feed([]byte("event: route\ndata: {}\n\nevent: private-token\ndata: {}\n\nevent: error\ndata: {\"message\":\"private-token\"}\n\n"))
	stream.Finish("closed")
	encoded, err := json.Marshal(stream)
	if err != nil || strings.Contains(string(encoded), "private-token") {
		t.Fatalf("unsafe capture: %s (%v)", encoded, err)
	}
	if len(stream.Frames) != 3 || stream.Frames[0].Fields["event"] != "route" ||
		!stream.Frames[1].Omitted || stream.Last.Fields["event"] != "error" || stream.Terminal != "" {
		t.Fatalf("unexpected playground evidence: %s", encoded)
	}
	done := NewStream(0)
	done.Feed([]byte("event: done\ndata: {}\n\n"))
	done.Finish("eof")
	if done.Terminal != "done" || done.End != "complete" {
		t.Fatalf("missing Playground terminal marker: %+v", done)
	}
}

func TestStreamDoesNotAcceptForeignTerminal(t *testing.T) {
	for _, protocol := range []string{"chat", "messages"} {
		stream := NewStream(1024)
		stream.protocol = protocol
		stream.Feed([]byte("event: done\ndata: {}\n\n"))
		stream.Finish("eof")
		if stream.Terminal != "" || stream.End != "eof" {
			t.Fatalf("%s accepted Playground terminal: %+v", protocol, stream)
		}
	}
}

func TestSDKCaptureUnavailableEvidence(t *testing.T) {
	collector := &Collector{}
	ctx := WithCollector(context.Background(), collector)
	ctx = WithUpstreamOrigin(ctx, "https://user:private-token@example.com:443/private-path?key=private-query#private-fragment")
	record := BeginNative(ctx, nil)
	if evidence := string(collector.Evidence()); !strings.Contains(evidence, `"request_unavailable":true`) ||
		!strings.Contains(evidence, `"response_unavailable":true`) {
		t.Fatalf("missing evidence flags: %s", evidence)
	}
	record(429, "application/json", nil, "complete")
	if evidence := string(collector.Evidence()); !strings.Contains(evidence, `"status":429`) ||
		!strings.Contains(evidence, `"response_unavailable":true`) {
		t.Fatalf("status without SDK body should keep unavailable marker: %s", evidence)
	}

	record(0, "request", []byte(`{"stream":true,"secret":"private-token"}`), "complete")
	record(429, "application/json", []byte(`{"error":{"message":"private-token"}}`), "complete")
	if evidence := string(collector.Evidence()); strings.Contains(evidence, "unavailable") ||
		strings.Contains(evidence, "private-") || !strings.Contains(evidence, `"status":429`) ||
		!strings.Contains(evidence, `"upstream_origin":"https://example.com:443"`) || !strings.Contains(evidence, `"elapsed_ms":`) {
		t.Fatalf("unexpected evidence: %s", evidence)
	}
}

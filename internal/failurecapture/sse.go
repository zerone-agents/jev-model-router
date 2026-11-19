package failurecapture

import (
	"bytes"
	"encoding/json"
	"strings"
)

// Stream captures complete safe projections and tracks terminal evidence
// independently of the frame retention limit.
type Stream struct {
	Frames    []Projection `json:"frames,omitempty"`
	Last      *Projection  `json:"last_complete_frame,omitempty"`
	Terminal  string       `json:"terminal,omitempty"`
	End       string       `json:"termination,omitempty"`
	Partial   bool         `json:"partial,omitempty"`
	Truncated bool         `json:"truncated,omitempty"`

	event        string
	eventOmitted bool
	protocol     string
	line         []byte
	longLine     bool
	data         []byte
	dropped      bool
	seen         bool
	remaining    int
}

const maxFrameBytes = 64 << 10

func NewStream(budget int) *Stream {
	return &Stream{remaining: max(0, budget)}
}

// Feed accepts arbitrary network chunk boundaries.
func (s *Stream) Feed(chunk []byte) {
	for len(chunk) > 0 {
		end := bytes.IndexByte(chunk, '\n')
		part := chunk
		if end >= 0 {
			part = chunk[:end]
		}
		if s.longLine || len(s.line)+len(part) > maxFrameBytes {
			s.longLine = true
			s.dropped = true
			s.Truncated = true
			s.line = nil
		} else {
			s.line = append(s.line, part...)
		}
		if end < 0 {
			return
		}
		if !s.longLine {
			s.consumeLine(bytes.TrimSuffix(s.line, []byte("\r")))
		}
		s.longLine = false
		s.line = s.line[:0]
		chunk = chunk[end+1:]
	}
}

func (s *Stream) consumeLine(line []byte) {
	if len(line) == 0 {
		if s.seen || s.dropped {
			s.finishFrame()
		}
		s.event = ""
		s.eventOmitted = false
		return
	}
	field, value, _ := bytes.Cut(line, []byte(":"))
	value = bytes.TrimPrefix(value, []byte(" "))
	if string(field) == "event" {
		s.event = ""
		switch string(value) {
		case "route", "retry", "delta", "done", "error", "message_start", "message_stop",
			"message_delta", "content_block_start", "content_block_delta", "content_block_stop", "ping":
			s.event = string(value)
		}
		s.eventOmitted = s.event == ""
		return
	}
	if string(field) != "data" {
		return
	}
	s.seen = true
	if s.dropped || len(s.data)+len(value)+1 > maxFrameBytes {
		s.dropped = true
		s.Truncated = true
		return
	}
	s.data = append(s.data, value...)
	s.data = append(s.data, '\n')
}

func (s *Stream) finishFrame() {
	frame := Projection{Omitted: true}
	switch {
	case s.dropped:
	case strings.TrimSpace(string(s.data)) == "[DONE]":
		if s.protocol == "" || s.protocol == "chat" {
			s.Terminal = "done"
		}
		frame = Projection{Fields: map[string]any{"terminal": "done"}}
	default:
		frame = ProjectJSON(s.data)
		if frame.Fields["type"] == "message_stop" && (s.protocol == "" || s.protocol == "messages") {
			s.Terminal = "message_stop"
		}
	}
	if s.event != "" {
		if frame.Fields == nil {
			frame.Fields = make(map[string]any)
		}
		frame.Fields["event"] = s.event
		if s.seen && !s.dropped && !frame.Invalid && s.event == "done" && (s.protocol == "" || s.protocol == "playground") {
			s.Terminal = "done"
		}
	}
	frame.Omitted = frame.Omitted || s.eventOmitted
	s.Last = &frame
	encoded, _ := json.Marshal(frame)
	if charge := len(encoded) + 1; s.remaining >= charge {
		s.Frames = append(s.Frames, frame)
		s.remaining -= charge
	} else {
		s.Truncated = true
	}
	s.data = nil
	s.seen = false
	s.dropped = false
}

func (s *Stream) Finish(reason string) {
	s.Partial = s.seen || s.dropped || s.event != "" || s.eventOmitted || len(s.line) > 0
	switch reason {
	case "eof", "complete", "closed", "timeout", "canceled", "read_error", "write_error":
		s.End = reason
	default:
		s.End = "unknown"
	}
	if s.End == "eof" && s.Terminal != "" && !s.Partial {
		s.End = "complete"
	}
	s.line = nil
	s.data = nil
}

package routing

import (
	"context"
	"errors"
	"testing"
	"time"
)

type failedSink struct{}

func (failedSink) Append(context.Context, Record) error { return errors.New("SECRET") }
func TestRecorderDegraded(t *testing.T) {
	r := Recorder{Sink: failedSink{}, Timeout: time.Millisecond}
	r.Save(Record{})
	if !r.Degraded() {
		t.Fatal("silent failure")
	}
}

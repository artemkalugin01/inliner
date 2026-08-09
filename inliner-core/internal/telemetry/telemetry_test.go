package telemetry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAsyncRecorderWritesJSONLines(t *testing.T) {
	dir := t.TempDir()
	recorder, err := NewAsyncRecorder(dir)
	if err != nil {
		t.Fatalf("NewAsyncRecorder returned error: %v", err)
	}

	recorder.Record(Event{Kind: "completion_request", StateID: "1", Status: "ok", ProviderMs: 12})
	recorder.Close()

	data, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	var event Event
	if err := json.Unmarshal(data, &event); err != nil {
		t.Fatalf("Unmarshal returned error: %v; data=%q", err, data)
	}
	if event.Kind != "completion_request" || event.StateID != "1" || event.Status != "ok" || event.ProviderMs != 12 {
		t.Fatalf("event = %+v, want recorded event", event)
	}
	if event.Timestamp == "" {
		t.Fatal("Timestamp is empty")
	}
}

package protocol

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestTransportReadReadsLineDelimitedMessages(t *testing.T) {
	input := strings.NewReader("{\"kind\":\"greeting\",\"allowGitignore\":false}\n")
	transport := NewTransport(input, io.Discard)

	msg, err := transport.Read()
	if err != nil {
		t.Fatalf("Read returned error: %v", err)
	}
	if msg.Kind != "greeting" {
		t.Fatalf("Kind = %q, want %q", msg.Kind, "greeting")
	}
}

func TestTransportReadReturnsEOF(t *testing.T) {
	transport := NewTransport(strings.NewReader(""), io.Discard)

	_, err := transport.Read()
	if err != io.EOF {
		t.Fatalf("Read error = %v, want io.EOF", err)
	}
}

func TestTransportSendPrefixesJSONLine(t *testing.T) {
	var output bytes.Buffer
	transport := NewTransport(strings.NewReader(""), &output)

	err := transport.Send(ConnectionStatus{Kind: "connection_status", IsConnected: true})
	if err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	line := output.String()
	if !strings.HasPrefix(line, MessagePrefix) {
		t.Fatalf("output = %q, want prefix %q", line, MessagePrefix)
	}
	if !strings.HasSuffix(line, "\n") {
		t.Fatalf("output = %q, want trailing newline", line)
	}

	payload := strings.TrimSuffix(strings.TrimPrefix(line, MessagePrefix), "\n")
	var decoded ConnectionStatus
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		t.Fatalf("failed to decode payload %q: %v", payload, err)
	}
	if decoded.Kind != "connection_status" || !decoded.IsConnected {
		t.Fatalf("decoded = %+v, want connected status", decoded)
	}
}

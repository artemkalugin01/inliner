package protocol

import (
	"encoding/json"
	"testing"
)

func TestDecodeRawPreservesKindAndPayload(t *testing.T) {
	line := []byte(`{"kind":"state_update","newId":"42","updates":[]}`)

	msg, err := DecodeRaw(line)
	if err != nil {
		t.Fatalf("DecodeRaw returned error: %v", err)
	}

	if msg.Kind != "state_update" {
		t.Fatalf("Kind = %q, want %q", msg.Kind, "state_update")
	}
	if string(msg.Raw) != string(line) {
		t.Fatalf("Raw = %q, want %q", string(msg.Raw), string(line))
	}
}

func TestDecodeRawRejectsInvalidJSON(t *testing.T) {
	if _, err := DecodeRaw([]byte(`{"kind":`)); err == nil {
		t.Fatal("DecodeRaw returned nil error for invalid JSON")
	}
}

func TestAcceptUpdateJSONShape(t *testing.T) {
	message := AcceptUpdate{Kind: "accept_update", StateID: "7", Path: "/tmp/main.go", Text: "fmt.Println(name)"}

	payload, err := json.Marshal(message)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}

	want := `{"kind":"accept_update","stateId":"7","path":"/tmp/main.go","text":"fmt.Println(name)"}`
	if string(payload) != want {
		t.Fatalf("payload = %s, want %s", payload, want)
	}
}

func TestDismissUpdateJSONShape(t *testing.T) {
	message := DismissUpdate{Kind: "dismiss_update", StateID: "7", Path: "/tmp/main.go", Text: "fmt.Println(name)"}

	payload, err := json.Marshal(message)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}

	want := `{"kind":"dismiss_update","stateId":"7","path":"/tmp/main.go","text":"fmt.Println(name)"}`
	if string(payload) != want {
		t.Fatalf("payload = %s, want %s", payload, want)
	}
}

func TestHealthResponseJSONShape(t *testing.T) {
	message := HealthResponse{
		Kind:              "health_response",
		CoreVersion:       "0.1.0",
		Provider:          "ollama",
		OllamaBaseURL:     "http://localhost:11434",
		OllamaModel:       "model",
		OllamaTemperature: 0.25,
		OllamaNumPredict:  256,
		ProviderStatus:    "ok",
		ProviderReachable: true,
		RequestTimeout:    "5s",
		WindowBytes:       4096,
		OpenDocuments:     2,
		InFlightRequests:  1,
	}

	payload, err := json.Marshal(message)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}

	want := `{"kind":"health_response","coreVersion":"0.1.0","provider":"ollama","ollamaBaseUrl":"http://localhost:11434","ollamaModel":"model","ollamaTemperature":0.25,"ollamaNumPredict":256,"providerStatus":"ok","providerReachable":true,"requestTimeout":"5s","windowBytes":4096,"openDocuments":2,"inFlightRequests":1}`
	if string(payload) != want {
		t.Fatalf("payload = %s, want %s", payload, want)
	}
}

package protocol

import "encoding/json"

type RawMessage struct {
	Kind string `json:"kind"`
	Raw  []byte `json:"-"`
}

type Greeting struct {
	Kind           string `json:"kind"`
	AllowGitignore bool   `json:"allowGitignore"`
}

type StateUpdate struct {
	Kind    string   `json:"kind"`
	NewID   string   `json:"newId"`
	Updates []Update `json:"updates"`
}

type Update struct {
	Kind    string `json:"kind"`
	Path    string `json:"path"`
	Content string `json:"content,omitempty"`
	Offset  int    `json:"offset,omitempty"`
}

type Shutdown struct {
	Kind string `json:"kind"`
}

type HealthRequest struct {
	Kind string `json:"kind"`
}

type AcceptUpdate struct {
	Kind    string `json:"kind"`
	StateID string `json:"stateId"`
	Path    string `json:"path"`
	Text    string `json:"text"`
}

type DismissUpdate struct {
	Kind    string `json:"kind"`
	StateID string `json:"stateId"`
	Path    string `json:"path"`
	Text    string `json:"text"`
}

type ConnectionStatus struct {
	Kind        string  `json:"kind"`
	IsConnected bool    `json:"is_connected"`
	StatusText  *string `json:"status_text"`
}

type Response struct {
	Kind    string         `json:"kind"`
	StateID string         `json:"stateId"`
	Items   []ResponseItem `json:"items"`
}

type ResponseItem struct {
	Kind   string `json:"kind"`
	Text   string `json:"text,omitempty"`
	Verify string `json:"verify,omitempty"`
}

type Error struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

type HealthResponse struct {
	Kind              string  `json:"kind"`
	CoreVersion       string  `json:"coreVersion"`
	Provider          string  `json:"provider"`
	OllamaBaseURL     string  `json:"ollamaBaseUrl,omitempty"`
	OllamaModel       string  `json:"ollamaModel,omitempty"`
	OllamaTemperature float64 `json:"ollamaTemperature,omitempty"`
	OllamaNumPredict  int     `json:"ollamaNumPredict,omitempty"`
	ProviderStatus    string  `json:"providerStatus,omitempty"`
	ProviderReachable bool    `json:"providerReachable"`
	ProviderError     string  `json:"providerError,omitempty"`
	RequestTimeout    string  `json:"requestTimeout"`
	WindowBytes       int     `json:"windowBytes"`
	OpenDocuments     int     `json:"openDocuments"`
	InFlightRequests  int     `json:"inFlightRequests"`
}

func DecodeRaw(line []byte) (RawMessage, error) {
	var msg RawMessage
	if err := json.Unmarshal(line, &msg); err != nil {
		return RawMessage{}, err
	}
	msg.Raw = append([]byte(nil), line...)
	return msg, nil
}

package protocol

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

const MessagePrefix = "INLINER-MESSAGE "

type Transport struct {
	scanner *bufio.Scanner
	output  io.Writer
	mu      sync.Mutex
}

func NewTransport(input io.Reader, output io.Writer) *Transport {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	return &Transport{
		scanner: scanner,
		output:  output,
	}
}

func (t *Transport) Read() (RawMessage, error) {
	if !t.scanner.Scan() {
		if err := t.scanner.Err(); err != nil {
			return RawMessage{}, err
		}
		return RawMessage{}, io.EOF
	}

	return DecodeRaw(t.scanner.Bytes())
}

func (t *Transport) Send(message any) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return err
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	_, err = fmt.Fprintf(t.output, "%s%s\n", MessagePrefix, payload)
	return err
}

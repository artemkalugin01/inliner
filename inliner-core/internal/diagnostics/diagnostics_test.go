package diagnostics

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

func TestDefaultSocketPath(t *testing.T) {
	want := filepath.Join(os.TempDir(), SocketName)
	if got := DefaultSocketPath(); got != want {
		t.Fatalf("DefaultSocketPath() = %q, want %q", got, want)
	}
	if got := DefaultSocketPath(); got != want {
		t.Fatalf("DefaultSocketPath() changed to %q", got)
	}
}

func TestConnectWithoutServerReturnsNoop(t *testing.T) {
	publisher := Connect(filepath.Join(t.TempDir(), "missing.sock"))
	if publisher.Verbose() {
		t.Fatal("no-op publisher is verbose")
	}
	publisher.Publish(Event{Kind: KindRequestStarted})
	publisher.Close()
	publisher.Close()
}

func TestServerHandshakeMultipleClientsAndFormatting(t *testing.T) {
	path := shortSocketPath(t)
	var output lockedBuffer
	server, err := Listen(path, true, &output)
	if err != nil {
		t.Fatalf("Listen returned error: %v", err)
	}
	defer server.Close()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat returned error: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("socket permissions = %o, want 600", got)
	}

	one := Connect(path)
	two := Connect(path)
	if !one.Verbose() || !two.Verbose() {
		t.Fatal("server did not advertise verbose mode to both clients")
	}
	timestamp := time.Date(2026, time.August, 18, 12, 41, 3, 102000000, time.Local)
	one.Publish(Event{Kind: KindRequestStarted, Timestamp: timestamp, RequestID: "184", StateID: "state-1", FilePath: "/project/main.go"})
	one.Publish(Event{Kind: KindAwaitingModel, Timestamp: timestamp, RequestID: "184", ContextMilliseconds: 8})
	one.Publish(Event{Kind: KindPrompt, Timestamp: timestamp, RequestID: "184", Prompt: "complete this\nfunction"})
	two.Publish(Event{Kind: KindResult, Timestamp: timestamp, RequestID: "184", Status: StatusError, ContextMilliseconds: 8, ModelMilliseconds: 634, Empty: true, Error: "model unavailable"})
	one.Close()
	two.Close()

	wantLines := []string{
		"12:41:03.102 request 184 started file=/project/main.go",
		"12:41:03.102 request 184 awaiting model context=8ms",
		"12:41:03.102 request 184 prompt:\n----- BEGIN PROMPT -----\ncomplete this\nfunction\n----- END PROMPT -----",
		`12:41:03.102 request 184 error context=8ms model=634ms empty=true error="model unavailable"`,
	}
	waitForLines(t, &output, wantLines)
}

func TestNonVerboseServerSuppressesPrompt(t *testing.T) {
	path := shortSocketPath(t)
	var output lockedBuffer
	server, err := Listen(path, false, &output)
	if err != nil {
		t.Fatalf("Listen returned error: %v", err)
	}
	publisher := Connect(path)
	if publisher.Verbose() {
		t.Fatal("publisher unexpectedly verbose")
	}
	publisher.Publish(Event{Kind: KindPrompt, RequestID: "1", Prompt: "hidden"})
	publisher.Publish(Event{Kind: KindResult, RequestID: "1", Status: StatusOK})
	publisher.Close()
	waitForLines(t, &output, []string{" request 1 ok context=0ms model=0ms"})
	if strings.Contains(output.String(), "hidden") {
		t.Fatalf("non-verbose output contains prompt: %q", output.String())
	}
	if err := server.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("socket remains after Close: %v", err)
	}
}

func TestListenRemovesStaleSocket(t *testing.T) {
	path := shortSocketPath(t)
	stale, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatalf("ListenUnix returned error: %v", err)
	}
	stale.SetUnlinkOnClose(false)
	if err := stale.Close(); err != nil {
		t.Fatalf("stale Close returned error: %v", err)
	}
	server, err := Listen(path, false, &lockedBuffer{})
	if err != nil {
		t.Fatalf("Listen with stale socket returned error: %v", err)
	}
	if err := server.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
}

func TestPublisherDegradesAfterDisconnect(t *testing.T) {
	path := shortSocketPath(t)
	server, err := Listen(path, false, &lockedBuffer{})
	if err != nil {
		t.Fatalf("Listen returned error: %v", err)
	}
	publisher := Connect(path)
	if err := server.Close(); err != nil {
		t.Fatalf("server Close returned error: %v", err)
	}
	for i := 0; i < PublisherQueue*2; i++ {
		publisher.Publish(Event{Kind: KindResult, RequestID: fmt.Sprint(i), Status: StatusCancelled})
	}
	publisher.Close()
}

func TestListenDoesNotReplaceRegularFile(t *testing.T) {
	path := shortSocketPath(t)
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	if _, err := Listen(path, false, &lockedBuffer{}); err == nil {
		t.Fatal("Listen replaced a non-socket file")
	}
}

func shortSocketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp(os.TempDir(), "inliner-diag-")
	if err != nil {
		t.Fatalf("MkdirTemp returned error: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "d.sock")
}

func waitForLines(t *testing.T, output *lockedBuffer, lines []string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		text := output.String()
		found := true
		for _, line := range lines {
			if !strings.Contains(text, line) {
				found = false
				break
			}
		}
		if found {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("output = %q, missing one of %q", output.String(), lines)
}

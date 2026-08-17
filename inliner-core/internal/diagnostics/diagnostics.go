package diagnostics

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

const (
	SocketName     = "inliner-diagnostics.sock"
	PublisherQueue = 256
	writeTimeout   = 250 * time.Millisecond
)

type Kind string

const (
	KindRequestStarted Kind = "request_started"
	KindAwaitingModel  Kind = "awaiting_model"
	KindPrompt         Kind = "prompt"
	KindResult         Kind = "result"
)

type Status string

const (
	StatusOK         Status = "ok"
	StatusError      Status = "error"
	StatusCancelled  Status = "cancelled"
	StatusStale      Status = "stale"
	StatusSuppressed Status = "suppressed"
	StatusCached     Status = "cached"
)

type Event struct {
	Kind                Kind      `json:"kind"`
	Timestamp           time.Time `json:"timestamp"`
	RequestID           string    `json:"requestId"`
	StateID             string    `json:"stateId,omitempty"`
	FilePath            string    `json:"filePath,omitempty"`
	Status              Status    `json:"status,omitempty"`
	ContextMilliseconds int64     `json:"contextMilliseconds,omitempty"`
	ModelMilliseconds   int64     `json:"modelMilliseconds,omitempty"`
	Empty               bool      `json:"empty,omitempty"`
	Error               string    `json:"error,omitempty"`
	Prompt              string    `json:"prompt,omitempty"`
}

type Publisher interface {
	Publish(Event)
	Verbose() bool
	Close()
}

type noopPublisher struct{}

func (noopPublisher) Publish(Event) {}
func (noopPublisher) Verbose() bool { return false }
func (noopPublisher) Close()        {}

func Noop() Publisher {
	return noopPublisher{}
}

func DefaultSocketPath() string {
	return filepath.Join(os.TempDir(), SocketName)
}

func ConnectDefault() Publisher {
	return Connect(DefaultSocketPath())
}

// Connect returns a no-op publisher if the server is absent or its handshake fails.
func Connect(path string) Publisher {
	conn, err := net.DialTimeout("unix", path, 100*time.Millisecond)
	if err != nil {
		return Noop()
	}

	_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	var hello handshake
	if err := json.NewDecoder(conn).Decode(&hello); err != nil || hello.Type != "handshake" {
		_ = conn.Close()
		return Noop()
	}
	_ = conn.SetReadDeadline(time.Time{})

	p := &socketPublisher{
		conn:    conn,
		verbose: hello.Verbose,
		queue:   make(chan Event, PublisherQueue),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
	p.connected.Store(true)
	go p.run()
	return p
}

type socketPublisher struct {
	conn      net.Conn
	verbose   bool
	queue     chan Event
	stop      chan struct{}
	done      chan struct{}
	mu        sync.RWMutex
	closed    bool
	connected atomic.Bool
}

func (p *socketPublisher) Publish(event Event) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.closed || !p.connected.Load() {
		return
	}
	select {
	case p.queue <- event:
	default:
	}
}

func (p *socketPublisher) Verbose() bool {
	return p.verbose
}

func (p *socketPublisher) Close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	close(p.stop)
	p.mu.Unlock()
	<-p.done
}

func (p *socketPublisher) run() {
	defer close(p.done)
	defer p.conn.Close()
	encoder := json.NewEncoder(p.conn)
	for {
		select {
		case event := <-p.queue:
			if err := p.writeEvent(encoder, event); err != nil {
				p.connected.Store(false)
				return
			}
		case <-p.stop:
			for {
				select {
				case event := <-p.queue:
					if err := p.writeEvent(encoder, event); err != nil {
						p.connected.Store(false)
						return
					}
				default:
					p.connected.Store(false)
					return
				}
			}
		}
	}
}

func (p *socketPublisher) writeEvent(encoder *json.Encoder, event Event) error {
	if err := p.conn.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
		return err
	}
	return encoder.Encode(event)
}

type handshake struct {
	Type    string `json:"type"`
	Verbose bool   `json:"verbose"`
}

type Server struct {
	path       string
	verbose    bool
	output     io.Writer
	listener   net.Listener
	socketInfo os.FileInfo
	acceptDone chan struct{}
	closeOnce  sync.Once
	clientsMu  sync.Mutex
	clients    map[net.Conn]struct{}
	clientsWG  sync.WaitGroup
	outputMu   sync.Mutex
}

func ListenDefault(verbose bool, output io.Writer) (*Server, error) {
	return Listen(DefaultSocketPath(), verbose, output)
}

// Listen creates the socket and starts accepting clients before it returns.
func Listen(path string, verbose bool, output io.Writer) (*Server, error) {
	if output == nil {
		return nil, errors.New("diagnostics: nil output writer")
	}
	if err := removeStaleSocket(path); err != nil {
		return nil, err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("diagnostics: listen: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = listener.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("diagnostics: secure socket: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		_ = listener.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("diagnostics: stat socket: %w", err)
	}

	s := &Server{
		path:       path,
		verbose:    verbose,
		output:     output,
		listener:   listener,
		socketInfo: info,
		acceptDone: make(chan struct{}),
		clients:    make(map[net.Conn]struct{}),
	}
	go s.accept()
	return s, nil
}

func removeStaleSocket(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("diagnostics: inspect socket: %w", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("diagnostics: path exists and is not a socket: %s", path)
	}
	conn, dialErr := net.DialTimeout("unix", path, 100*time.Millisecond)
	if dialErr == nil {
		_ = conn.Close()
		return fmt.Errorf("diagnostics: socket already in use: %s", path)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("diagnostics: remove stale socket: %w", err)
	}
	return nil
}

func (s *Server) accept() {
	defer close(s.acceptDone)
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		s.clientsMu.Lock()
		s.clients[conn] = struct{}{}
		s.clientsWG.Add(1)
		s.clientsMu.Unlock()
		go s.serve(conn)
	}
}

func (s *Server) serve(conn net.Conn) {
	defer s.clientsWG.Done()
	defer func() {
		s.clientsMu.Lock()
		delete(s.clients, conn)
		s.clientsMu.Unlock()
		_ = conn.Close()
	}()
	if err := json.NewEncoder(conn).Encode(handshake{Type: "handshake", Verbose: s.verbose}); err != nil {
		return
	}
	decoder := json.NewDecoder(conn)
	for {
		var event Event
		if err := decoder.Decode(&event); err != nil {
			return
		}
		if event.Kind == KindPrompt && !s.verbose {
			continue
		}
		s.outputMu.Lock()
		_, _ = fmt.Fprintln(s.output, formatEvent(event))
		s.outputMu.Unlock()
	}
}

func formatEvent(event Event) string {
	timestamp := event.Timestamp
	if timestamp.IsZero() {
		timestamp = time.Now()
	}
	prefix := fmt.Sprintf("%s request %s", timestamp.Format("15:04:05.000"), event.RequestID)
	switch event.Kind {
	case KindRequestStarted:
		if event.FilePath != "" {
			return fmt.Sprintf("%s started file=%s", prefix, event.FilePath)
		}
		return prefix + " started"
	case KindAwaitingModel:
		return fmt.Sprintf("%s awaiting model context=%dms", prefix, event.ContextMilliseconds)
	case KindPrompt:
		return fmt.Sprintf("%s prompt:\n----- BEGIN PROMPT -----\n%s\n----- END PROMPT -----", prefix, event.Prompt)
	case KindResult:
		line := fmt.Sprintf("%s %s context=%dms model=%dms", prefix, event.Status, event.ContextMilliseconds, event.ModelMilliseconds)
		if event.Empty {
			line += " empty=true"
		}
		if event.Error != "" {
			line += fmt.Sprintf(" error=%q", event.Error)
		}
		return line
	default:
		return fmt.Sprintf("%s %s", prefix, event.Kind)
	}
}

func (s *Server) Close() error {
	var closeErr error
	s.closeOnce.Do(func() {
		closeErr = s.listener.Close()
		<-s.acceptDone
		s.clientsMu.Lock()
		for conn := range s.clients {
			_ = conn.Close()
		}
		s.clientsMu.Unlock()
		s.clientsWG.Wait()
		if info, err := os.Stat(s.path); err == nil && os.SameFile(s.socketInfo, info) {
			if err := os.Remove(s.path); err != nil {
				closeErr = err
			}
		}
	})
	if errors.Is(closeErr, net.ErrClosed) {
		return nil
	}
	return closeErr
}

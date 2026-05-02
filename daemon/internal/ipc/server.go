package ipc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sort"
	"sync"
	"syscall"
	"time"
)

const (
	maxFrameBytes        = 2 * 1024 * 1024
	maxActiveConnections = 16
	readTimeout          = 10 * time.Second
	writeTimeout         = 10 * time.Second
)

// Handler is a function that processes a JSON-RPC method call.
// It receives raw params and returns a result or an error.
type Handler func(params *json.RawMessage) (interface{}, *RPCError)

// Server is a JSON-RPC 2.0 server over a Unix domain socket.
type Server struct {
	socketPath string
	listener   net.Listener
	handlers   map[string]Handler
	mu         sync.RWMutex
	done       chan struct{}
	stopOnce   sync.Once
	wg         sync.WaitGroup
	activeConn chan struct{}
}

// NewServer creates a new IPC server bound to the given socket path.
func NewServer(socketPath string) *Server {
	return &Server{
		socketPath: socketPath,
		handlers:   make(map[string]Handler),
		done:       make(chan struct{}),
		activeConn: make(chan struct{}, maxActiveConnections),
	}
}

// Register adds a handler for the given JSON-RPC method name.
func (s *Server) Register(method string, handler Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[method] = handler
}

func (s *Server) Methods() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	methods := make([]string, 0, len(s.handlers))
	for method := range s.handlers {
		methods = append(methods, method)
	}
	sort.Strings(methods)
	return methods
}

// Start begins listening on the Unix domain socket and accepting connections.
func (s *Server) Start() error {
	// Remove stale socket file if it exists.
	if err := removeStaleSocket(s.socketPath); err != nil {
		return err
	}

	ln, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return err
	}
	s.listener = ln

	// daemonctl talks to this socket through su, so direct socket access stays
	// root-only. Peer credentials are still checked for defense in depth.
	if err := os.Chmod(s.socketPath, 0600); err != nil {
		log.Printf("ipc: warning: chmod socket: %v", err)
	}

	s.wg.Add(1)
	go s.acceptLoop()
	log.Printf("ipc: listening on %s", s.socketPath)
	return nil
}

// Stop gracefully shuts down the server: stops accepting new connections
// and waits for in-flight requests to finish.
func (s *Server) Stop() {
	s.stopOnce.Do(func() {
		close(s.done)
		if s.listener != nil {
			s.listener.Close()
		}
		s.wg.Wait()
		if err := removeStaleSocket(s.socketPath); err != nil {
			log.Printf("ipc: warning: remove socket: %v", err)
		}
		log.Printf("ipc: server stopped")
	})
}

func removeStaleSocket(path string) error {
	st, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if st.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("refuse to remove non-socket: %s", path)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *Server) acceptLoop() {
	defer s.wg.Done()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.done:
				return
			default:
				log.Printf("ipc: accept error: %v", err)
				continue
			}
		}
		select {
		case s.activeConn <- struct{}{}:
		default:
			log.Printf("ipc: rejecting connection: active connection limit reached")
			conn.Close()
			continue
		}
		s.wg.Add(1)
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer s.wg.Done()
	defer func() { <-s.activeConn }()
	defer conn.Close()

	if err := authorizePeer(conn); err != nil {
		log.Printf("ipc: rejected peer: %v", err)
		return
	}

	reader := bufio.NewReader(conn)

	if err := conn.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
		log.Printf("ipc: set read deadline: %v", err)
		return
	}
	line, err := readFrame(reader)
	if err != nil && err != io.EOF {
		log.Printf("ipc: read error: %v", err)
		return
	}
	if len(line) == 0 {
		return
	}

	resp := s.processRequest(line)
	respBytes, err := json.Marshal(resp)
	if err != nil {
		log.Printf("ipc: marshal response error: %v", err)
		return
	}
	respBytes = append(respBytes, '\n')
	if err := conn.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
		log.Printf("ipc: set write deadline: %v", err)
		return
	}
	if _, err := conn.Write(respBytes); err != nil {
		log.Printf("ipc: write error: %v", err)
	}
}

func authorizePeer(conn net.Conn) error {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return os.ErrPermission
	}
	raw, err := unixConn.SyscallConn()
	if err != nil {
		return err
	}
	var cred *syscall.Ucred
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		cred, credErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return err
	}
	if credErr != nil {
		return credErr
	}
	if cred == nil || cred.Uid != 0 {
		return os.ErrPermission
	}
	return nil
}

func readFrame(reader *bufio.Reader) ([]byte, error) {
	var frame []byte
	for {
		chunk, isPrefix, err := reader.ReadLine()
		if err != nil {
			if err == io.EOF && len(frame) > 0 {
				return frame, io.EOF
			}
			return nil, err
		}
		if len(frame)+len(chunk) > maxFrameBytes {
			return nil, io.ErrShortBuffer
		}
		frame = append(frame, chunk...)
		if !isPrefix {
			return frame, nil
		}
	}
}

func (s *Server) processRequest(data []byte) *Response {
	var req Request
	if err := json.Unmarshal(data, &req); err != nil {
		return NewErrorResponse(0, CodeParseError, "parse error: "+err.Error(), nil)
	}

	if rpcErr := req.Validate(); rpcErr != nil {
		return NewErrorResponse(req.ID, rpcErr.Code, rpcErr.Message, nil)
	}

	s.mu.RLock()
	handler, ok := s.handlers[req.Method]
	s.mu.RUnlock()

	if !ok {
		data := map[string]interface{}{
			"requestedMethod":  req.Method,
			"supportedMethods": SupportedMethods(),
		}
		if replacement := replacedMethodHint(req.Method); replacement != "" {
			data["replacement"] = replacement
		}
		return NewErrorResponse(req.ID, CodeMethodNotFound,
			"method not found: "+req.Method, data)
	}

	result, rpcErr := handler(req.Params)
	if rpcErr != nil {
		return NewErrorResponse(req.ID, rpcErr.Code, rpcErr.Message, rpcErr.Data)
	}
	return NewResponse(req.ID, result)
}

func replacedMethodHint(method string) string {
	switch method {
	case "config.import":
		return "Use config-import for full daemon config import, or profile.importNodes for Paste URI/node imports."
	case "network.reset":
		return "Use backend.reset."
	case "node.test":
		return "Use diagnostics.testNodes."
	case "self.check":
		return "Use self-check."
	case "status":
		return "Use backend.status."
	case "start":
		return "Use backend.start."
	case "stop":
		return "Use backend.stop."
	case "reload":
		return "Use backend.restart."
	case "health":
		return "Use diagnostics.health."
	case "subscription-fetch":
		return "Use subscription.preview or subscription.refresh."
	default:
		return ""
	}
}

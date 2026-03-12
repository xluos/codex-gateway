package oauth

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
)

type CallbackResult struct {
	Code  string
	State string
	Err   string
}

type CallbackServer struct {
	addr   string
	path   string
	server *http.Server
	ln     net.Listener
	mu     sync.RWMutex
}

func NewCallbackServer(addr, path string) *CallbackServer {
	return &CallbackServer{addr: addr, path: path}
}

func (s *CallbackServer) Start(ctx context.Context) (<-chan CallbackResult, error) {
	resultCh := make(chan CallbackResult, 1)
	mux := http.NewServeMux()
	mux.HandleFunc(s.path, func(w http.ResponseWriter, r *http.Request) {
		result := CallbackResult{
			Code:  r.URL.Query().Get("code"),
			State: r.URL.Query().Get("state"),
			Err:   r.URL.Query().Get("error"),
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OAuth login completed. You can close this window."))
		select {
		case resultCh <- result:
		default:
		}
		go s.Close(context.Background())
	})

	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.ln = ln
	s.server = &http.Server{Handler: mux}
	s.mu.Unlock()

	go func() {
		<-ctx.Done()
		_ = s.Close(context.Background())
	}()
	go func() {
		_ = s.server.Serve(ln)
	}()
	return resultCh, nil
}

func (s *CallbackServer) Address() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.ln == nil {
		return s.addr
	}
	return s.ln.Addr().String()
}

func (s *CallbackServer) RedirectURI() string {
	return fmt.Sprintf("http://%s%s", s.Address(), s.path)
}

func (s *CallbackServer) Close(ctx context.Context) error {
	s.mu.RLock()
	server := s.server
	s.mu.RUnlock()
	if server == nil {
		return nil
	}
	return server.Shutdown(ctx)
}

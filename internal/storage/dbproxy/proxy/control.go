package proxy

import (
	"bufio"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/steveyegge/beads/internal/storage/dbproxy/identity"
)

const (
	maxIdentRequestBytes       = 256
	identDeadline              = 2 * time.Second
	maxConcurrentIdentRequests = 8
	maxControlAcceptErrors     = 3
	controlAcceptRetryDelay    = 10 * time.Millisecond
)

type controlServer struct {
	listener net.Listener
	secret   string
	done     chan struct{}
	once     sync.Once
	reply    func() identity.IdentReply
	errs     chan error
	slots    chan struct{}
}

func startControl(rootDir string, reply func() identity.IdentReply) (*controlServer, error) {
	secret, err := identity.ReadSecret(rootDir)
	if err != nil {
		return nil, fmt.Errorf("proxy: read control secret: %w", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("proxy: listen control: %w", err)
	}
	s := &controlServer{
		listener: ln,
		secret:   secret,
		done:     make(chan struct{}),
		reply:    reply,
		errs:     make(chan error, 1),
		slots:    make(chan struct{}, maxConcurrentIdentRequests),
	}
	go s.acceptLoop()
	return s, nil
}

func (s *controlServer) Port() int {
	return s.listener.Addr().(*net.TCPAddr).Port
}

func (s *controlServer) Errors() <-chan error {
	return s.errs
}

func (s *controlServer) Close() error {
	var err error
	s.once.Do(func() {
		err = s.listener.Close()
		if errors.Is(err, net.ErrClosed) {
			err = nil
		}
		<-s.done
	})
	return err
}

func (s *controlServer) acceptLoop() {
	defer close(s.done)
	consecutiveErrors := 0
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			consecutiveErrors++
			if consecutiveErrors >= maxControlAcceptErrors {
				acceptErr := fmt.Errorf("proxy: control accept failed %d times: %w", consecutiveErrors, err)
				select {
				case s.errs <- acceptErr:
				default:
				}
				_ = s.listener.Close()
				return
			}
			time.Sleep(time.Duration(consecutiveErrors) * controlAcceptRetryDelay)
			continue
		}
		consecutiveErrors = 0
		select {
		case s.slots <- struct{}{}:
			go func() {
				defer func() { <-s.slots }()
				s.handle(conn)
			}()
		default:
			_ = conn.Close()
		}
	}
}

func (s *controlServer) handle(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	if err := conn.SetDeadline(time.Now().Add(identDeadline)); err != nil {
		return
	}
	line, err := bufio.NewReader(io.LimitReader(conn, maxIdentRequestBytes+1)).ReadString('\n')
	if err != nil || len(line) > maxIdentRequestBytes || !strings.HasSuffix(line, "\n") {
		return
	}
	parts := strings.Split(strings.TrimSuffix(line, "\n"), " ")
	if len(parts) != 3 || parts[0] != "IDENT" {
		return
	}
	if subtle.ConstantTimeCompare([]byte(parts[1]), []byte(s.secret)) != 1 {
		return
	}
	reply := s.reply()
	reply.ControlPort = s.Port()
	signed, err := identity.SignIdentReply(reply, s.secret, parts[2])
	if err != nil {
		return
	}
	_ = writeIdentReply(conn, signed)
}

func writeIdentReply(w io.Writer, reply identity.IdentReply) error {
	return json.NewEncoder(w).Encode(reply)
}

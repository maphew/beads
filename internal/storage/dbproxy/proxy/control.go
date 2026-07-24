package proxy

import (
	"bufio"
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
	maxIdentRequestBytes = 256
	identDeadline        = 2 * time.Second
)

type controlServer struct {
	listener net.Listener
	secret   string
	done     chan struct{}
	once     sync.Once
	reply    func() identity.IdentReply
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
	}
	go s.acceptLoop()
	return s, nil
}

func (s *controlServer) Port() int {
	return s.listener.Addr().(*net.TCPAddr).Port
}

func (s *controlServer) Close() error {
	var err error
	s.once.Do(func() {
		err = s.listener.Close()
		<-s.done
	})
	return err
}

func (s *controlServer) acceptLoop() {
	defer close(s.done)
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		go s.handle(conn)
	}
}

func (s *controlServer) handle(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	if err := conn.SetDeadline(time.Now().Add(identDeadline)); err != nil {
		return
	}
	line, err := bufio.NewReader(io.LimitReader(conn, maxIdentRequestBytes+1)).ReadString('\n')
	if err != nil || len(line) > maxIdentRequestBytes || !strings.HasPrefix(line, "IDENT ") {
		return
	}
	if line != "IDENT "+s.secret+"\n" {
		return
	}
	_ = writeIdentReply(conn, s.reply())
}

func writeIdentReply(w io.Writer, reply identity.IdentReply) error {
	return json.NewEncoder(w).Encode(reply)
}

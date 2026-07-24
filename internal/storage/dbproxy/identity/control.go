package identity

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"
)

const maxIdentReplyBytes = 4096

// ErrIdentRefused reports that a control listener closed the connection
// without replying to an identity request.
var ErrIdentRefused = errors.New("identity: request refused")

// IdentReply is the authenticated identity published by a managed proxy.
type IdentReply struct {
	Schema      int    `json:"schema"`
	Role        string `json:"role"`
	RootID      string `json:"root_id"`
	UpstreamID  string `json:"upstream_id"`
	PID         int    `json:"pid"`
	Birth       string `json:"birth"`
	DataPort    int    `json:"data_port"`
	ControlPort int    `json:"control_port"`
}

// Identify authenticates to a proxy control listener and returns its identity.
func Identify(host string, controlPort int, secret string, timeout time.Duration) (*IdentReply, error) {
	addr := net.JoinHostPort(host, strconv.Itoa(controlPort))
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, fmt.Errorf("identity: dial control listener: %w", err)
	}
	defer func() { _ = conn.Close() }()

	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return nil, fmt.Errorf("identity: set control deadline: %w", err)
	}
	if _, err := io.WriteString(conn, "IDENT "+secret+"\n"); err != nil {
		return nil, fmt.Errorf("identity: write request: %w", err)
	}

	line, err := bufio.NewReader(io.LimitReader(conn, maxIdentReplyBytes+1)).ReadString('\n')
	if errors.Is(err, io.EOF) && len(line) == 0 {
		return nil, ErrIdentRefused
	}
	if err != nil {
		return nil, fmt.Errorf("identity: read reply: %w", err)
	}
	if len(line) > maxIdentReplyBytes {
		return nil, errors.New("identity: oversized reply")
	}

	var reply IdentReply
	if err := json.Unmarshal([]byte(line), &reply); err != nil {
		return nil, fmt.Errorf("identity: decode reply: %w", err)
	}
	return &reply, nil
}

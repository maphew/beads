//go:build windows

package procid

import (
	"fmt"
	"os"
	"strconv"

	"golang.org/x/sys/windows"
)

// Handle keeps a Windows process handle open to prevent PID reuse.
type Handle struct {
	process windows.Handle
	token   Token
}

func Capture(pid int) (Token, error) {
	process, err := openProcess(pid)
	if err != nil {
		return "", fmt.Errorf("procid: open process %d: %w", pid, err)
	}
	defer windows.CloseHandle(process)
	return tokenForProcess(process)
}

func Verify(pid int, tok Token) (bool, error) {
	process, err := openProcess(pid)
	if err != nil {
		if err == windows.ERROR_INVALID_PARAMETER {
			return false, nil
		}
		return false, fmt.Errorf("procid: open process %d: %w", pid, err)
	}
	defer windows.CloseHandle(process)
	current, err := tokenForProcess(process)
	if err != nil {
		return false, err
	}
	return current == tok, nil
}

func Open(pid int, tok Token) (*Handle, error) {
	process, err := openProcess(pid)
	if err != nil {
		return nil, fmt.Errorf("procid: open process %d: %w", pid, err)
	}
	current, err := tokenForProcess(process)
	if err != nil || current != tok {
		_ = windows.CloseHandle(process)
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("procid: process %d does not match token", pid)
	}
	return &Handle{process: process, token: tok}, nil
}

func (h *Handle) Signal(os.Signal) error {
	if err := h.verify(); err != nil {
		return err
	}
	if err := windows.TerminateProcess(h.process, 1); err != nil {
		return fmt.Errorf("procid: terminate process: %w", err)
	}
	return nil
}

func (h *Handle) Kill() error { return h.Signal(os.Kill) }

func (h *Handle) Close() error {
	if h.process == 0 {
		return nil
	}
	err := windows.CloseHandle(h.process)
	h.process = 0
	if err != nil {
		return fmt.Errorf("procid: close process handle: %w", err)
	}
	return nil
}

func (h *Handle) verify() error {
	current, err := tokenForProcess(h.process)
	if err != nil {
		return err
	}
	if current != h.token {
		return fmt.Errorf("procid: process no longer matches token")
	}
	return nil
}

func openProcess(pid int) (windows.Handle, error) {
	return windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
}

func tokenForProcess(process windows.Handle) (Token, error) {
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(process, &created, &exited, &kernel, &user); err != nil {
		return "", fmt.Errorf("procid: get process times: %w", err)
	}
	value := uint64(created.HighDateTime)<<32 | uint64(created.LowDateTime)
	return Token("windows-v1:" + strconv.FormatUint(value, 10)), nil
}

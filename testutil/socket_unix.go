//go:build !windows

package testutil

import (
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// DummyUnixSocket represents a dummy Unix socket for testing socket connections.
type DummyUnixSocket struct {
	Dir      string // Directory containing the socket
	Path     string // Full path to the socket file
	listener net.Listener
	tmpDir   string
	closed   atomic.Bool
}

// StartDummyUnixSocket creates a dummy Unix socket that accepts connections
// and immediately responds with test data. This is used to verify that database
// drivers correctly use socket connections.
//
// The socketName parameter is the name of the socket file to create within
// the temporary directory. For MySQL, this is typically "mysql.sock".
// For PostgreSQL, this is typically ".s.PGSQL.<port>".
//
// Returns a DummyUnixSocket which must be closed by calling Close().
func StartDummyUnixSocket(t *testing.T, dirPrefix, socketName string) *DummyUnixSocket {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", dirPrefix)
	if err != nil {
		t.Fatal(err)
	}

	socketPath := filepath.Join(tmpDir, socketName)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatal(err)
	}

	sock := &DummyUnixSocket{
		Dir:      tmpDir,
		Path:     socketPath,
		listener: listener,
		tmpDir:   tmpDir,
	}

	go sock.acceptLoop()

	return sock
}

func (s *DummyUnixSocket) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		// Respond with garbage data to trigger a protocol error (not "connection refused")
		conn.Write([]byte("dummy socket response\n"))
		conn.Close()
	}
}

// Close shuts down the socket and cleans up temporary files.
func (s *DummyUnixSocket) Close() {
	if s.closed.Swap(true) {
		return
	}
	s.listener.Close()
	os.RemoveAll(s.tmpDir)
}

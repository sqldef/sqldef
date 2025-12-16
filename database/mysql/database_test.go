//go:build !windows

package mysql

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sqldef/sqldef/v3/database"
)

func TestUnixSocketConnection(t *testing.T) {
	socketPath, cleanup := startDummyUnixSocket(t, "mysql-socket-test", "mysql.sock")
	defer cleanup()

	config := database.Config{
		DbName:   "testdb",
		User:     "testuser",
		Password: "testpass",
		Socket:   socketPath,
	}

	db, err := NewDatabase(config)
	if err != nil {
		t.Fatalf("NewDatabase failed: %v", err)
	}
	defer db.Close()

	err = db.DB().Ping()
	if err == nil {
		t.Fatal("expected connection to fail with protocol error")
	}

	// "connection refused" means socket path was not used
	if strings.Contains(err.Error(), "connection refused") {
		t.Errorf("expected socket to be used, got: %v", err)
	}
}

func startDummyUnixSocket(t *testing.T, dirPrefix, socketName string) (socketPath string, cleanup func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", dirPrefix)
	if err != nil {
		t.Fatal(err)
	}

	socketPath = filepath.Join(tmpDir, socketName)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatal(err)
	}

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.Write([]byte("this is a test socket!\n"))
			conn.Close()
		}
	}()

	cleanup = func() {
		listener.Close()
		os.RemoveAll(tmpDir)
	}
	return socketPath, cleanup
}

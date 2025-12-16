//go:build !windows

package postgres

import (
	"strings"
	"testing"

	"github.com/sqldef/sqldef/v3/database"
	"github.com/sqldef/sqldef/v3/testutil"
)

func TestUnixSocketConnection(t *testing.T) {
	// PostgreSQL expects socket files named .s.PGSQL.<port> in the directory
	sock := testutil.StartDummyUnixSocket(t, "postgres-socket-test", ".s.PGSQL.5432")
	defer sock.Close()

	config := database.Config{
		DbName:   "testdb",
		User:     "testuser",
		Password: "testpass",
		Socket:   sock.Dir,
		Port:     5432,
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

	// "connection refused" means socket path was not used (fell back to TCP).
	// Any other error (e.g., protocol error) indicates the socket was used.
	if strings.Contains(err.Error(), "connection refused") {
		t.Errorf("expected socket to be used, got: %v", err)
	}
}

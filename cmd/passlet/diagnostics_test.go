package main

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"passlet.dev/passlet/internal/passlet"
)

func TestDoctorRejectsUnsafeSocketDirectory(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "pl-doctor-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	t.Setenv("HOME", dir)
	t.Setenv("PASSLET_ENDPOINT", "")
	listener, err := passlet.ListenUnix(passlet.DefaultSocket())
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"status":"ready"}`)) })}
	go server.Serve(listener)
	defer server.Close()
	if err := os.Chmod(filepath.Dir(passlet.DefaultSocket()), 0755); err != nil {
		t.Fatal(err)
	}
	output, err := invokeCLICommand(t, "", func() error {
		return diagnosticsForOS(context.Background(), nil, "darwin")
	})
	if err == nil || !strings.Contains(output, "Socket directory: unsafe") {
		t.Fatalf("unsafe directory: %q %v", output, err)
	}
}

func TestDiagnosticArgumentValidation(t *testing.T) {
	for _, args := range [][]string{{"--unknown"}, {"extra"}} {
		if err := diagnostics(context.Background(), args); err == nil {
			t.Fatal("invalid arguments accepted")
		}
	}
	if err := auditCommand([]string{"extra"}); err == nil {
		t.Fatal("audit accepted extra arguments")
	}
}

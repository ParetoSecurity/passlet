package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"passlet.dev/passlet/internal/passlet"
	"passlet.dev/passlet/testutil/testkeys"
)

func TestSSHAgentCLIValidation(t *testing.T) {
	approverTestPaths(t)
	for _, args := range [][]string{{"ssh-agent", "extra"}, {"ssh-agent", "--unknown"}, {"ssh-agent", "--endpoint", "invalid"}} {
		if _, err := invokeCLI(t, "", args...); err == nil {
			t.Fatal(args)
		}
	}
	t.Setenv("PASSLET_ENDPOINT", "")
	testkeys.Write(t, os.Getenv("HOME"))
	if err := os.WriteFile(filepath.Join(passlet.StateDir(), "connection.json"), []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := sshAgent(context.Background(), nil); err == nil {
		t.Fatal("invalid pairing config accepted")
	}
	t.Setenv("PASSLET_ENDPOINT", "unix://"+passlet.DefaultSocket())
	listener, err := passlet.ListenUnix(passlet.SSHAgentSocket())
	if err != nil {
		t.Fatal(err)
	}
	if err := sshAgent(context.Background(), nil); err == nil {
		t.Fatal("occupied socket accepted")
	}
	listener.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sshAgent(ctx, nil); err != nil {
		t.Fatal(err)
	}
}

func TestSSHAgentApproverDisabledByDefault(t *testing.T) {
	_, helper := approverTestPaths(t)
	t.Setenv("PASSLET_ENDPOINT", "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- approver(ctx, []string{"--helper", helper}) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		c, err := net.Dial("unix", passlet.DefaultSocket())
		if err == nil {
			c.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Approver socket did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	client, closeClient, err := passlet.EndpointClient("unix://" + passlet.DefaultSocket())
	if err != nil {
		t.Fatal(err)
	}
	defer closeClient()
	target, _ := passlet.EndpointURL("unix://"+passlet.DefaultSocket(), "/v1/ssh-agent")
	response, err := client.Post(target, "application/octet-stream", bytes.NewReader([]byte{11}))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatal("unconfigured forwarding accepted", response.StatusCode)
	}
	cancel()
	if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestSSHAgentSocketActivation(t *testing.T) {
	dir, _ := approverTestPaths(t)
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(dir, "activation.sock"), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	client, err := net.Dial("unix", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	conn, err := listener.AcceptUnix()
	if err != nil {
		t.Fatal(err)
	}
	file, err := conn.File()
	conn.Close()
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	previous := os.Stdin
	os.Stdin = file
	defer func() { os.Stdin = previous }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- sshAgent(ctx, []string{"--stdio", "--endpoint", "unix://" + filepath.Join(dir, "missing.sock")})
	}()
	client.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := client.Write([]byte{0, 0, 0, 1, 11}); err != nil {
		t.Fatal(err)
	}
	var response [5]byte
	if _, err := io.ReadFull(client, response[:]); err != nil || response != [5]byte{0, 0, 0, 1, 5} {
		t.Fatal(response, err)
	}
	client.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	file.Close()
	if err := sshAgent(ctx, []string{"--stdio", "--endpoint", "unix://" + filepath.Join(dir, "missing.sock")}); err == nil {
		t.Fatal("invalid activation socket accepted")
	}
}

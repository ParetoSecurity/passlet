package passlet

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"
)

type Approval interface {
	Approve(context.Context, string) (CredentialSession, error)
}
type CredentialSession interface {
	Credentials() Credentials
	Store(Credentials) error
	Close()
}
type NativeHelper struct {
	Path     string
	Provider string
}
type helperSession struct {
	cmd         *exec.Cmd
	in          io.WriteCloser
	scan        *bufio.Scanner
	credentials Credentials
	once        sync.Once
	done        chan struct{}
}
type helperReply struct {
	Status      int          `json:"status,omitempty"`
	OK          bool         `json:"ok"`
	Error       string       `json:"error"`
	Credentials *Credentials `json:"credentials,omitempty"`
}

func (h NativeHelper) Approve(ctx context.Context, prompt string) (CredentialSession, error) {
	args := []string{}
	if h.Provider != "" {
		args = append(args, "--provider", h.Provider)
	}
	cmd := exec.Command(h.Path, args...)
	in, err := cmd.StdinPipe()
	if err != nil { // coverage-ignore: Fresh command; failure requires process-wide file-descriptor exhaustion.
		return nil, errors.New("helper_unavailable")
	}
	out, err := cmd.StdoutPipe()
	if err != nil { // coverage-ignore: Fresh command; failure requires process-wide file-descriptor exhaustion.
		return nil, errors.New("helper_unavailable")
	}
	if err = cmd.Start(); err != nil {
		return nil, errors.New("helper_unavailable")
	}
	s := &helperSession{cmd: cmd, in: in, scan: bufio.NewScanner(out), done: make(chan struct{})}
	s.scan.Buffer(make([]byte, 4096), 65536)
	go func() {
		select {
		case <-ctx.Done():
			s.Close()
		case <-s.done:
		}
	}()
	if err = s.send(map[string]any{"op": "approve", "prompt": prompt}); err != nil { // coverage-ignore: Helper must die during the first pipe write; deterministic closed-pipe send failure is tested separately.
		s.Close()
		return nil, err
	}
	reply, err := s.reply()
	if err != nil {
		s.Close()
		return nil, err
	}
	if reply.Credentials == nil || reply.Credentials.Validate() != nil || reply.Credentials.Provider != h.Provider {
		s.Close()
		return nil, errors.New("invalid_credentials")
	}
	s.credentials = *reply.Credentials
	return s, nil
}
func (s *helperSession) Credentials() Credentials { return s.credentials }
func (s *helperSession) send(v any) error {
	if json.NewEncoder(s.in).Encode(v) != nil {
		return errors.New("helper_unavailable")
	}
	return nil
}
func (s *helperSession) reply() (helperReply, error) {
	var r helperReply
	if !s.scan.Scan() || json.Unmarshal(s.scan.Bytes(), &r) != nil {
		return r, errors.New("helper_unavailable")
	}
	if !r.OK {
		switch r.Error {
		case "approval_denied", "approval_cancelled", "keychain_unavailable", "credentials_missing":
			return r, errors.New(r.Error)
		default:
			return r, errors.New("helper_failed")
		}
	}
	return r, nil
}
func (s *helperSession) Store(c Credentials) error {
	if err := s.send(map[string]any{"op": "store", "credentials": c}); err != nil {
		return err
	}
	if _, err := s.reply(); err != nil {
		return err
	}
	s.credentials = c
	return nil
}
func (s *helperSession) Close() {
	s.once.Do(func() {
		// EOF tells the helper to invalidate its LAContext immediately. Kill is a
		// fallback if the GUI session or helper is unresponsive.
		_ = s.in.Close()
		exited := make(chan struct{})
		go func() { _ = s.cmd.Wait(); close(exited) }()
		select {
		case <-exited:
		case <-time.After(time.Second):
			_ = s.cmd.Process.Kill()
			<-exited
		}
		close(s.done)
	})
}
func ImportCredentials(ctx context.Context, path string, c Credentials) error {
	if err := c.Account.Validate(); c.Provider != "cloudflare" && err != nil {
		return err
	}
	if err := c.Validate(); err != nil {
		return err
	}
	args := []string{"--import"}
	if c.Provider != "" {
		args = append(args, "--provider", c.Provider)
	}
	cmd := exec.CommandContext(ctx, path, args...)
	in, err := cmd.StdinPipe()
	if err != nil { // coverage-ignore: Fresh command; failure requires process-wide file-descriptor exhaustion.
		return err
	}
	out, err := cmd.StdoutPipe()
	if err != nil { // coverage-ignore: Fresh command; failure requires process-wide file-descriptor exhaustion.
		return err
	}
	if err = cmd.Start(); err != nil {
		return errors.New("helper_unavailable")
	}
	if err = json.NewEncoder(in).Encode(map[string]any{"credentials": c}); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return errors.New("helper_unavailable")
	}
	_ = in.Close()
	var reply helperReply
	err = json.NewDecoder(io.LimitReader(out, 65536)).Decode(&reply)
	wait := cmd.Wait()
	if err != nil || wait != nil || !reply.OK {
		if reply.Status != 0 {
			return fmt.Errorf("keychain_import_failed_osstatus_%d", reply.Status)
		}
		return errors.New("keychain_import_failed")
	}
	return nil
}

// Reset requires native approval and deletes only Passlet's own credential item.
func (h NativeHelper) Reset(ctx context.Context) (AccountInfo, error) {
	provider := "GitHub"
	if h.Provider == "cloudflare" {
		provider = "Cloudflare"
	}
	session, err := h.Approve(ctx, "remove your Passlet "+provider+" credentials")
	if err != nil {
		return AccountInfo{}, err
	}
	defer session.Close()
	s := session.(*helperSession)
	account := s.Credentials().Account
	if err := s.send(map[string]any{"op": "delete"}); err != nil { // coverage-ignore: Requires helper death between approval and delete; closed-pipe send handling is tested separately.
		return AccountInfo{}, err
	}
	if _, err := s.reply(); err != nil {
		return AccountInfo{}, err
	}
	return account, nil
}

// Confirm asks for user presence without accessing provider credentials.
func (h NativeHelper) Confirm(ctx context.Context, prompt string) error {
	cmd := exec.CommandContext(ctx, h.Path)
	data, _ := json.Marshal(map[string]string{"op": "confirm", "prompt": prompt})
	in, err := cmd.StdinPipe()
	if err != nil { // coverage-ignore: Fresh command; pipe failure requires file-descriptor exhaustion.
		return err
	}
	out, err := cmd.StdoutPipe()
	if err != nil { // coverage-ignore: Fresh command; pipe failure requires file-descriptor exhaustion.
		return err
	}
	if err = cmd.Start(); err != nil {
		return errors.New("helper_unavailable")
	}
	defer func() { _ = in.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	if _, err = in.Write(append(data, '\n')); err != nil { // coverage-ignore: Requires helper death during the first pipe write.
		return errors.New("helper_unavailable")
	}
	scan := bufio.NewScanner(out)
	scan.Buffer(make([]byte, 4096), 65536)
	if !scan.Scan() {
		return errors.New("helper_unavailable")
	}
	var reply helperReply
	if json.Unmarshal(scan.Bytes(), &reply) != nil || !reply.OK {
		return errors.New("pairing approval denied or cancelled")
	}
	return nil
}

package passlet

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const InvitationLifetime = 10 * time.Minute

type pairedAgent struct {
	Name      string `json:"name"`
	PublicKey string `json:"public_key"`
}
type pairingState struct {
	Agents     map[string]pairedAgent `json:"agents"`
	Invitation string                 `json:"invitation,omitempty"`
	Expires    time.Time              `json:"expires,omitempty"`
}

// A process lock also serializes the CLI's invitation/revocation with service approvals.
func withPairingState(update bool, fn func(*pairingState) error) error {
	dir := StateDir()
	if err := PrivateDir(dir); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(dir, "pairing.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil { // coverage-ignore: Requires an OS locking failure on the successfully opened private regular file.
		return err
	}
	defer func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) }()
	path := filepath.Join(dir, "pairings.json")
	s := pairingState{Agents: map[string]pairedAgent{}}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil && (len(data) > 1024*1024 || strictJSON(data, &s) != nil || s.Agents == nil) {
		return errors.New("invalid saved pairings")
	}
	if err := fn(&s); err != nil {
		return err
	}
	if !update {
		return nil
	}
	data, _ = json.Marshal(s)
	return writePairingKey(path, append(data, '\n'), true)
}

func CreateInvitation() (string, error) {
	private, err := LoadOrCreatePairingKey(filepath.Join(StateDir(), "approver.pem"), filepath.Join(StateDir(), "approver.pub"))
	if err != nil {
		return "", err
	}
	secret := make([]byte, 32)
	_, _ = rand.Read(secret)
	hash := sha256.Sum256(secret)
	err = withPairingState(true, func(s *pairingState) error {
		s.Invitation = hex.EncodeToString(hash[:])
		s.Expires = time.Now().Add(InvitationLifetime)
		return nil
	})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(private.Public().(ed25519.PublicKey)) + "." + base64.RawURLEncoding.EncodeToString(secret), nil
}

func PairingAgents() (map[string]string, error) {
	agents := map[string]string{}
	err := withPairingState(false, func(s *pairingState) error {
		for id, a := range s.Agents {
			agents[id] = a.Name
		}
		return nil
	})
	return agents, err
}

func RevokeAgent(name string) error {
	return withPairingState(true, func(s *pairingState) error {
		for id, a := range s.Agents {
			if a.Name == name {
				delete(s.Agents, id)
				return nil
			}
		}
		return errors.New("no paired Agent with that name")
	})
}

func decodeInvitation(code string) (ed25519.PublicKey, []byte, error) {
	parts := strings.Split(code, ".")
	if len(parts) != 2 {
		return nil, nil, errors.New("invalid pairing code")
	}
	public, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || len(public) != 32 {
		return nil, nil, errors.New("invalid pairing code")
	}
	secret, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(secret) != 32 {
		return nil, nil, errors.New("invalid pairing code")
	}
	return ed25519.PublicKey(public), secret, nil
}

type enrollmentRequest struct {
	Secret string `json:"secret"`
	Name   string `json:"name"`
}

func (p *Pairing) enroll(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != "POST" || r.Header.Get("Origin") != "" {
		apiError(w, 400, "invalid pairing request")
		return
	}
	key, err := peerKey(*r.TLS)
	if err != nil {
		apiError(w, 403, "invalid Agent identity")
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 2048))
	var req enrollmentRequest
	if err != nil || strictJSON(data, &req) != nil || req.Name == "" || len(req.Name) > 100 || req.Name != safeLabel(req.Name) || strings.TrimSpace(req.Name) != req.Name {
		apiError(w, 400, "invalid pairing request")
		return
	}
	secret, err := base64.RawURLEncoding.DecodeString(req.Secret)
	if err != nil || len(secret) != 32 {
		apiError(w, 403, "invalid or expired pairing code")
		return
	}
	select {
	case p.enrollmentBusy <- struct{}{}:
		defer func() { <-p.enrollmentBusy }()
	default:
		apiError(w, 409, "pairing approval already in progress")
		return
	}
	hash := sha256.Sum256(secret)
	err = withPairingState(true, func(s *pairingState) error {
		if !time.Now().Before(s.Expires) || subtle.ConstantTimeCompare([]byte(s.Invitation), []byte(hex.EncodeToString(hash[:]))) != 1 {
			return errors.New("invalid or expired pairing code")
		}
		s.Invitation = ""
		s.Expires = time.Time{}
		return nil
	})
	if err != nil {
		apiError(w, 403, "invalid or expired pairing code")
		return
	}
	_ = http.NewResponseController(w).SetReadDeadline(time.Time{})
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()
	prompt := fmt.Sprintf("pair Agent %s.\n\nOnly approve if you started this pairing", req.Name)
	if err := p.confirm(ctx, prompt); err != nil {
		apiError(w, 403, "pairing was not approved; create a new pairing code")
		return
	}
	if ctx.Err() != nil {
		apiError(w, 408, "pairing cancelled")
		return
	}
	err = withPairingState(true, func(s *pairingState) error {
		for id, a := range s.Agents {
			if a.Name == req.Name && id != keyID(key) {
				return errors.New("agent name already paired; revoke it before replacing it")
			}
		}
		s.Agents[keyID(key)] = pairedAgent{Name: req.Name, PublicKey: base64.RawURLEncoding.EncodeToString(key)}
		return nil
	})
	if err != nil {
		apiError(w, 409, "could not save pairing; check for an existing Agent with that name")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"paired":true}`)
}

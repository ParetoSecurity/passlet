package passlet

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"time"
)

type Recorder interface{ Record(Event) error }
type Minter interface {
	Refresh(context.Context, Credentials) (Credentials, error)
	Mint(context.Context, Credentials, Request) (Grant, error)
}
type Server struct {
	Approval Approval
	GitHub   Minter
	Audit    Recorder
	Identity func(context.Context, string) (Caller, error)
	busy     chan struct{}
	Deadline time.Duration
}

func NewServer(approval Approval, g Minter, a Recorder) *Server {
	return &Server{Approval: approval, GitHub: g, Audit: a, Identity: WhoIs, busy: make(chan struct{}, 1), Deadline: ServerDeadline}
}
func apiError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": code})
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Path != "/v1/grants" {
		apiError(w, 404, "not_found")
		return
	}
	if r.Method != "POST" {
		apiError(w, 405, "method_not_allowed")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.Deadline)
	defer cancel()
	caller, err := s.Identity(ctx, r.RemoteAddr)
	if err != nil {
		apiError(w, 403, "identity_unavailable")
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 8192))
	if err != nil {
		apiError(w, 400, "invalid_request")
		return
	}
	// Body deadline must not become an approval deadline through net/http
	// background disconnect detection after the request body is consumed.
	http.NewResponseController(w).SetReadDeadline(time.Time{})
	var requested Request
	if strictJSON(data, &requested) != nil || requested.Validate() != nil {
		apiError(w, 400, "invalid_request")
		return
	}
	id := make([]byte, 16)
	if _, err = rand.Read(id); err != nil {
		apiError(w, 500, "internal_error")
		return
	}
	event := Event{RequestID: hex.EncodeToString(id), Caller: caller, Request: requested, Approver: "human", Outcome: "requested"}
	if s.Audit.Record(event) != nil {
		apiError(w, 503, "audit_unavailable")
		return
	}
	select {
	case s.busy <- struct{}{}:
		defer func() { <-s.busy }()
	default:
		event.Outcome = "busy"
		s.Audit.Record(event)
		apiError(w, 409, "approver_busy")
		return
	}
	fail := func(code string, status int) {
		if ctx.Err() != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				code = "approval_timeout"
				status = 504
			} else {
				code = "request_cancelled"
				status = 499
			}
		}
		if event.Decision == "" {
			switch code {
			case "approval_denied":
				event.Decision = "denied"
			case "approval_cancelled", "approval_timeout", "request_cancelled":
				event.Decision = "cancelled"
			}
		}
		event.Outcome = code
		s.Audit.Record(event)
		apiError(w, status, code)
	}
	session, err := s.Approval.Approve(ctx, requested.Prompt(caller))
	if err != nil {
		fail(err.Error(), 403)
		return
	}
	defer session.Close()
	if ctx.Err() != nil {
		fail("request_cancelled", 499)
		return
	}
	event.Decision = "approved"
	event.Outcome = "approved"
	if s.Audit.Record(event) != nil {
		fail("audit_unavailable", 503)
		return
	}
	c := session.Credentials()
	updated, err := s.GitHub.Refresh(ctx, c)
	if err != nil {
		fail(err.Error(), 502)
		return
	}
	if updated != c {
		if err = session.Store(updated); err != nil {
			fail("credential_rotation_not_saved", 503)
			return
		}
		c = updated
	}
	grant, err := s.GitHub.Mint(ctx, c, requested)
	if err != nil {
		fail(err.Error(), 502)
		return
	}
	grant.ID = event.RequestID
	event.ExpiresAt = &grant.ExpiresAt
	event.GrantedPermissions = grant.Permissions
	event.GrantedRepositories = grant.Repositories
	event.GitHubUser = grant.User
	event.Outcome = "minted"
	if s.Audit.Record(event) != nil {
		fail("audit_unavailable", 503)
		return
	}
	if ctx.Err() != nil {
		fail("request_cancelled", 499)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(grant)
	// A successful write is not proof that the remote application received it.
	if err != nil {
		event.Outcome = "delivery_failed"
	} else {
		event.Outcome = "delivery_written_unconfirmed"
	}
	if s.Audit.Record(event) != nil {
		log.Printf("audit_delivery_write_failed request_id=%s", event.RequestID)
	}
}

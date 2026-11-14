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
	VerifyIdentity(context.Context, Credentials, string) error
	Refresh(context.Context, Credentials) (Credentials, error)
	Mint(context.Context, Credentials, Request) (Grant, error)
}
type Server struct {
	Approval           Approval
	CloudflareApproval Approval
	Cloudflare         interface {
		Verify(context.Context, string) (Credentials, error)
		Mint(context.Context, Credentials, Request) (Grant, error)
	}
	GitHub   Minter
	Audit    Recorder
	Identity func(context.Context, string) (Caller, error)
	busy     chan struct{}
	Deadline time.Duration
}

type callerKey struct{}

func connectionIdentity(ctx context.Context, _ string) (Caller, error) {
	caller, ok := ctx.Value(callerKey{}).(Caller)
	if !ok {
		return Caller{}, errors.New("agent_authentication_required")
	}
	return caller, nil
}

func NewServer(approval Approval, g Minter, a Recorder) *Server {
	return &Server{Approval: approval, GitHub: g, Audit: a, Identity: connectionIdentity, busy: make(chan struct{}, 1), Deadline: ServerDeadline}
}
func apiError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Header.Get("Origin") != "" {
		apiError(w, 403, "browser_requests_not_supported")
		return
	}
	if r.URL.Path == "/v1/health" && r.Method == "GET" {
		if _, err := s.Identity(r.Context(), r.RemoteAddr); err != nil {
			apiError(w, 403, "identity_unavailable")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"ready"}`)
		return
	}
	doctor := r.URL.Path == "/v1/doctor"
	if r.URL.Path != "/v1/grants" && !doctor {
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
	_ = http.NewResponseController(w).SetReadDeadline(time.Time{})
	var requested Request
	invalid := strictJSON(data, &requested) != nil || requested.Validate() != nil
	if doctor {
		var diagnostic doctorRequest
		invalid = strictJSON(data, &diagnostic) != nil || (diagnostic.Provider != "" && diagnostic.Provider != "cloudflare")
		requested = Request{Provider: diagnostic.Provider}
	}
	if invalid {
		apiError(w, 400, "invalid_request")
		return
	}
	id := make([]byte, 16)
	if _, err = rand.Read(id); err != nil { // coverage-ignore: crypto/rand.Read never returns an error on the pinned Go version; entropy failure terminates the process.
		apiError(w, 500, "internal_error")
		return
	}
	event := Event{RequestID: hex.EncodeToString(id), Caller: caller, Request: requested, Approver: "human", Outcome: "requested"}
	if doctor {
		event.Operation = "doctor"
	}
	if s.Audit.Record(event) != nil {
		apiError(w, 503, "audit_unavailable")
		return
	}
	select {
	case s.busy <- struct{}{}:
		defer func() { <-s.busy }()
	default:
		event.Outcome = "busy"
		_ = s.Audit.Record(event)
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
		_ = s.Audit.Record(event)
		apiError(w, status, code)
	}
	prompt := requested.Prompt(caller)
	if doctor {
		prompt = "verify stored credentials for " + caller.displayName()
		if requested.Provider == "cloudflare" {
			prompt = "verify stored Cloudflare credentials for " + caller.displayName()
		}
	}
	approval := s.Approval
	if requested.Provider == "cloudflare" {
		if s.CloudflareApproval == nil || s.Cloudflare == nil {
			fail("provider_unavailable", 503)
			return
		}
		approval = s.CloudflareApproval
	}
	session, err := approval.Approve(ctx, prompt)
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
	if doctor && requested.Provider != "cloudflare" && c.Account.Validate() != nil {
		fail("invalid_protected_account_identity", 502)
		return
	}
	if c.Provider != requested.Provider {
		fail("invalid_credentials", 502)
		return
	}
	if requested.Provider != "cloudflare" {
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
	}
	var response any
	if doctor && requested.Provider == "cloudflare" {
		verified, err := s.Cloudflare.Verify(ctx, c.AccessToken)
		if err != nil || c.Validate() != nil || verified.CloudflareTokenID != c.CloudflareTokenID {
			fail("cloudflare_credentials_verification_failed", 502)
			return
		}
		response = DoctorResult{Provider: "cloudflare", CloudflareTokenID: verified.CloudflareTokenID}
		event.CloudflareTokenID = verified.CloudflareTokenID
		event.Outcome = "verified"
	} else if doctor {
		if err := s.GitHub.VerifyIdentity(ctx, c, c.Account.Login); err != nil {
			fail("github_identity_verification_failed", 502)
			return
		}
		response = DoctorResult{Login: c.Account.Login}
		event.GitHubUser = c.Account.Login
		event.Outcome = "verified"
	} else {
		var grant Grant
		var err error
		if requested.Provider == "cloudflare" {
			grant, err = s.Cloudflare.Mint(ctx, c, requested)
		} else {
			grant, err = s.GitHub.Mint(ctx, c, requested)
		}
		if err != nil {
			fail(err.Error(), 502)
			return
		}
		grant.ID = event.RequestID
		event.ExpiresAt = &grant.ExpiresAt
		event.GrantedOrganization = grant.Organization
		event.GrantedAccount = grant.Account
		event.GrantedPermissions = grant.Permissions
		event.GrantedRepositories = grant.Repositories
		if requested.Provider == "cloudflare" {
			event.CloudflareTokenID = grant.CloudflareTokenID
			event.GrantedZones = grant.Zones
		} else {
			event.GitHubUser = grant.User
		}
		event.Outcome = "minted"
		response = grant
	}
	if s.Audit.Record(event) != nil {
		fail("audit_unavailable", 503)
		return
	}
	if ctx.Err() != nil {
		fail("request_cancelled", 499)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(response)
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

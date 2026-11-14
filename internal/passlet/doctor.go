package passlet

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

type doctorRequest struct {
	Provider string `json:"provider,omitempty"`
}

type DoctorResult struct {
	Provider          string `json:"provider,omitempty"`
	Login             string `json:"login,omitempty"`
	CloudflareTokenID string `json:"cloudflare_token_id,omitempty"`
}

func Doctor(ctx context.Context, endpoint, provider string) (DoctorResult, error) {
	if provider != "" && provider != "cloudflare" {
		return DoctorResult{}, errors.New("invalid_provider")
	}
	client, close, err := EndpointClient(endpoint)
	if err != nil {
		return DoctorResult{}, err
	}
	defer close()
	target, _ := EndpointURL(endpoint, "/v1/doctor")
	data, _ := json.Marshal(doctorRequest{Provider: provider})
	req, err := http.NewRequestWithContext(ctx, "POST", target, bytes.NewReader(data))
	if err != nil {
		return DoctorResult{}, errors.New("invalid_request")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return DoctorResult{}, authenticationFailure(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var reply struct {
		DoctorResult
		Error string `json:"error"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&reply) != nil {
		return DoctorResult{}, errors.New("invalid_doctor_response")
	}
	if resp.StatusCode != http.StatusOK {
		switch reply.Error {
		case "provider_unavailable", "cloudflare_credentials_verification_failed", "refresh_failed", "invalid_credentials", "helper_failed", "request_cancelled", "approval_denied", "approval_cancelled", "approval_timeout", "approver_busy", "identity_unavailable", "credentials_missing", "audit_unavailable", "reauthorization_required", "invalid_protected_account_identity", "credential_rotation_not_saved", "github_identity_verification_failed", "helper_unavailable", "keychain_unavailable":
			return DoctorResult{}, errors.New(reply.Error)
		case "not_found":
			return DoctorResult{}, errors.New("doctor_requires_updated_approver")
		default:
			return DoctorResult{}, errors.New("doctor_failed")
		}
	}
	if reply.Provider != provider || reply.Error != "" || (provider == "cloudflare" && (!cloudflareID.MatchString(reply.CloudflareTokenID) || reply.Login != "")) || (provider == "" && (!githubHandle.MatchString(reply.Login) || len(reply.Login) > 39 || reply.CloudflareTokenID != "")) {
		return DoctorResult{}, errors.New("invalid_doctor_response")
	}
	return reply.DoctorResult, nil
}

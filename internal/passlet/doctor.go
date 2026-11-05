package passlet

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

type DoctorResult struct {
	Login string `json:"login"`
}

func Doctor(ctx context.Context, endpoint string) (DoctorResult, error) {
	client, close, err := EndpointClient(endpoint)
	if err != nil {
		return DoctorResult{}, err
	}
	defer close()
	target, _ := EndpointURL(endpoint, "/v1/doctor")
	req, err := http.NewRequestWithContext(ctx, "POST", target, strings.NewReader("{}"))
	if err != nil {
		return DoctorResult{}, errors.New("invalid_request")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return DoctorResult{}, errors.New("approver_unavailable_or_connection_lost")
	}
	defer func() { _ = resp.Body.Close() }()
	var reply struct {
		Login string `json:"login"`
		Error string `json:"error"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&reply) != nil {
		return DoctorResult{}, errors.New("invalid_doctor_response")
	}
	if resp.StatusCode != http.StatusOK {
		switch reply.Error {
		case "refresh_failed", "invalid_credentials", "helper_failed", "request_cancelled", "approval_denied", "approval_cancelled", "approval_timeout", "approver_busy", "identity_unavailable", "credentials_missing", "audit_unavailable", "reauthorization_required", "invalid_protected_account_identity", "credential_rotation_not_saved", "github_identity_verification_failed", "helper_unavailable", "keychain_unavailable":
			return DoctorResult{}, errors.New(reply.Error)
		case "not_found":
			return DoctorResult{}, errors.New("doctor_requires_updated_approver")
		default:
			return DoctorResult{}, errors.New("doctor_failed")
		}
	}
	if !githubHandle.MatchString(reply.Login) || len(reply.Login) > 39 || reply.Error != "" {
		return DoctorResult{}, errors.New("invalid_doctor_response")
	}
	return DoctorResult{Login: reply.Login}, nil
}

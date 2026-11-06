package passlet

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/pelletier/go-toml/v2"
)

type authenticationError string

func (e authenticationError) Error() string { return string(e) }

func ParsePublicKey(value string) (ed25519.PublicKey, error) {
	block, rest := pem.Decode([]byte(value))
	if block == nil || block.Type != "PUBLIC KEY" || len(bytes.TrimSpace(rest)) != 0 {
		return nil, authenticationError("invalid_agent_public_key")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	key, ok := parsed.(ed25519.PublicKey)
	if err != nil || !ok {
		return nil, authenticationError("invalid_agent_public_key")
	}
	return key, nil
}

func ReadPrivateKey(path string) (ed25519.PrivateKey, error) {
	if !filepath.IsAbs(path) {
		return nil, authenticationError("agent_private_key_requires_absolute_path")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, authenticationError("agent_private_key_unavailable")
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil { // coverage-ignore: Requires filesystem failure after opening the key.
		return nil, authenticationError("agent_private_key_unavailable")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || !ok || stat.Uid != uint32(os.Getuid()) {
		return nil, authenticationError("agent_private_key_requires_owner_and_mode_0600")
	}
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil { // coverage-ignore: Requires filesystem failure after opening and checking the key.
		return nil, authenticationError("agent_private_key_unavailable")
	}
	block, rest := pem.Decode(data)
	if len(data) > 4096 || block == nil || block.Type != "PRIVATE KEY" || len(bytes.TrimSpace(rest)) != 0 {
		return nil, authenticationError("invalid_agent_private_key")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	key, ok := parsed.(ed25519.PrivateKey)
	if err != nil || !ok {
		return nil, authenticationError("invalid_agent_private_key")
	}
	return key, nil
}

func keyID(key ed25519.PublicKey) string {
	digest := sha256.Sum256(key)
	return hex.EncodeToString(digest[:])
}

type Pairing struct {
	certificate tls.Certificate
	agents      map[string]string
}

func certificateFor(private ed25519.PrivateKey) (tls.Certificate, error) {
	if len(private) != ed25519.PrivateKeySize {
		return tls.Certificate{}, authenticationError("invalid_pairing_private_key")
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(1, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, private.Public(), private)
	if err != nil { // coverage-ignore: Ed25519 key and certificate template are validated above.
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: private}, nil
}

func peerKey(state tls.ConnectionState) (ed25519.PublicKey, error) {
	if len(state.PeerCertificates) != 1 {
		return nil, authenticationError("invalid_pairing_certificate")
	}
	cert := state.PeerCertificates[0]
	key, ok := cert.PublicKey.(ed25519.PublicKey)
	if !ok || time.Now().Before(cert.NotBefore) || !time.Now().Before(cert.NotAfter) {
		return nil, authenticationError("invalid_pairing_certificate")
	}
	return key, nil
}

func NewPairing(private ed25519.PrivateKey, agents map[string]string) (*Pairing, error) {
	certificate, err := certificateFor(private)
	if err != nil {
		return nil, err
	}
	p := &Pairing{certificate: certificate, agents: make(map[string]string)}
	for name, value := range agents {
		if name == "" || len(name) > 100 || name != safeLabel(name) || name != strings.TrimSpace(name) {
			return nil, authenticationError("invalid_agent_name")
		}
		key, err := ParsePublicKey(value)
		if err != nil {
			return nil, err
		}
		id := keyID(key)
		if _, exists := p.agents[id]; exists {
			return nil, authenticationError("duplicate_agent_public_key")
		}
		p.agents[id] = name
	}
	return p, nil
}

func (p *Pairing) ServerConfig() *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS13,
		// Refresh the self-signed certificate so a long-running service never needs an expiry restart.
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			cert, err := certificateFor(p.certificate.PrivateKey.(ed25519.PrivateKey))
			return &cert, err
		},
		ClientAuth:             tls.RequireAnyClientCert,
		SessionTicketsDisabled: true,
		VerifyConnection: func(state tls.ConnectionState) error {
			key, err := peerKey(state)
			if err != nil {
				return err
			}
			if _, ok := p.agents[keyID(key)]; !ok {
				return authenticationError("agent_not_trusted")
			}
			return nil
		},
	}
}

func (p *Pairing) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil {
			apiError(w, 403, "agent_authentication_required")
			return
		}
		key, err := peerKey(*r.TLS)
		if err != nil {
			apiError(w, 403, "agent_authentication_failed")
			return
		}
		agent, ok := p.agents[keyID(key)]
		if !ok {
			apiError(w, 403, "agent_not_trusted")
			return
		}
		caller := Caller{Transport: "paired", Name: agent, KeyID: keyID(key)}
		ctx := context.WithValue(r.Context(), callerKey{}, caller)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func pairedTLS(private ed25519.PrivateKey, approver ed25519.PublicKey) (*tls.Config, error) {
	certificate, err := certificateFor(private)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{certificate},
		// Trust the declared Ed25519 key, not public certificate authorities or DNS.
		InsecureSkipVerify: true,
		VerifyConnection: func(state tls.ConnectionState) error {
			key, err := peerKey(state)
			if err != nil {
				return err
			}
			if !bytes.Equal(key, approver) {
				return authenticationError("approver_identity_mismatch")
			}
			return nil
		},
	}, nil
}

func configuredAgentTLS() (*tls.Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, authenticationError("agent_configuration_unavailable")
	}
	file, err := os.Open(filepath.Join(home, ".config", "passlet", "agent.toml"))
	if err != nil {
		return nil, authenticationError("agent_configuration_missing")
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, 8193))
	var config struct {
		PrivateKeyFile    string `toml:"private_key_file"`
		ApproverPublicKey string `toml:"approver_public_key"`
	}
	decoder := toml.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err != nil || len(data) > 8192 || decoder.Decode(&config) != nil {
		return nil, authenticationError("invalid_agent_configuration")
	}
	if config.PrivateKeyFile == "" {
		config.PrivateKeyFile = filepath.Join(StateDir(), "agent.pem")
	}
	private, err := LoadOrCreatePairingKey(config.PrivateKeyFile, filepath.Join(StateDir(), "agent.pub"))
	if err != nil {
		return nil, err
	}
	public, err := ParsePublicKey(config.ApproverPublicKey)
	if err != nil {
		return nil, err
	}
	return pairedTLS(private, public)
}

func authenticationFailure(err error) error {
	var auth authenticationError
	if errors.As(err, &auth) {
		return auth
	}
	return errors.New("approver_unavailable_or_connection_lost")
}

// PairingProblem returns safe guidance without exposing key material or TLS internals.
func PairingProblem(err error) string {
	var auth authenticationError
	if !errors.As(err, &auth) {
		var remote *net.OpError
		if errors.As(err, &remote) && remote.Op == "remote error" {
			return "The Approver rejected pairing authentication. Check that it trusts this Agent's public key."
		}
		return ""
	}
	switch auth {
	case "agent_configuration_unavailable", "agent_configuration_missing":
		return "Agent pairing is not configured. Configure ~/.config/passlet/agent.toml before connecting."
	case "pairing_key_creation_failed", "invalid_pairing_key_path":
		return "Cannot create the pairing private key. Check its configured path and directory permissions."
	case "pairing_public_key_export_failed":
		return "Cannot export the pairing public key. Check permissions in ~/.local/share/passlet."
	case "invalid_agent_configuration":
		return "Agent pairing configuration is invalid. Check ~/.config/passlet/agent.toml."
	case "agent_private_key_requires_absolute_path", "agent_private_key_unavailable":
		return "Cannot read the pairing private key. Check private_key_file and provision the key on this machine."
	case "agent_private_key_requires_owner_and_mode_0600":
		return "The pairing private key must be a regular file owned by you with mode 0600."
	case "invalid_agent_private_key", "invalid_pairing_private_key":
		return "The pairing private key must be an Ed25519 key in PKCS#8 PEM format."
	case "invalid_agent_public_key":
		return "The pairing public key must be an Ed25519 key in PEM format."
	case "approver_identity_mismatch":
		return "The Approver public key does not match your pairing configuration. Verify its identity before changing the trusted key."
	default:
		return "Pairing authentication failed. Check the trusted keys on both machines."
	}
}

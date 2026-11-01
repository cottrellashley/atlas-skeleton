package fakes

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/jwt"
)

// Persona is who a development token is for.
type Persona struct {
	Subject string
	Name    string
	Groups  []string
}

// Issuer is a fake of Entra ID: it signs RS256 tokens for named personas, so
// that the real verifier runs against real signed tokens in every local run
// and test (§6.7.4). Its HTTP handler answers on loopback only.
type Issuer struct {
	Issuer   string
	Audience string
	Personas map[string]Persona
	key      *rsa.PrivateKey
	kid      string
}

// NewIssuer returns an issuer with a fresh signing key.
func NewIssuer(issuer, audience string, personas map[string]Persona) (*Issuer, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	return &Issuer{Issuer: issuer, Audience: audience, Personas: personas, key: key, kid: "dev-1"}, nil
}

// Verifier verifies the issuer's tokens.
func (d *Issuer) Verifier() *jwt.Verifier {
	return &jwt.Verifier{Issuer: d.Issuer, Audiences: []string{d.Audience}, Keys: jwt.StaticKeys{d.kid: &d.key.PublicKey}}
}

// Token signs a token for a persona, valid for an hour.
func (d *Issuer) Token(persona string) (string, error) {
	p, ok := d.Personas[persona]
	if !ok {
		return "", fmt.Errorf("unknown persona %q", persona)
	}
	token, _, err := d.sign(p, time.Hour)
	return token, err
}

func (d *Issuer) sign(p Persona, ttl time.Duration) (string, int64, error) {
	now := time.Now()
	exp := now.Add(ttl).Unix()
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": d.kid, "typ": "JWT"})
	claims, _ := json.Marshal(map[string]any{
		"iss": d.Issuer, "aud": d.Audience, "sub": p.Subject, "oid": p.Subject, "preferred_username": p.Name,
		"groups": p.Groups, "iat": now.Unix(), "nbf": now.Unix(), "exp": exp,
	})
	enc := base64.RawURLEncoding
	input := enc.EncodeToString(header) + "." + enc.EncodeToString(claims)
	sum := sha256.Sum256([]byte(input))
	sig, err := rsa.SignPKCS1v15(rand.Reader, d.key, crypto.SHA256, sum[:])
	if err != nil {
		return "", 0, err
	}
	return input + "." + enc.EncodeToString(sig), exp, nil
}

// Handler answers POST {"persona": "pm"} with a token, on loopback only.
func (d *Issuer) Handler(w http.ResponseWriter, r *http.Request) {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		http.Error(w, "development tokens are issued on loopback only", http.StatusForbidden)
		return
	}
	var req struct {
		Persona string `json:"persona"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req)
	p, ok := d.Personas[req.Persona]
	if !ok {
		http.Error(w, "unknown persona", http.StatusNotFound)
		return
	}
	token, exp, err := d.sign(p, time.Hour)
	if err != nil {
		http.Error(w, "could not sign", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{"access_token": token, "expires_at": exp})
}

package ports

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"time"
)

// ErrCallRefused is what a service answers to a call it must not act on.
var ErrCallRefused = errors.New("the call's token is unsigned, expired or for another address")

func (c Call) signed() []byte {
	c.Signature = nil
	b, _ := json.Marshal(c)
	return b
}

// SignCall signs a decided call. Only the front door holds the key.
func SignCall(key ed25519.PrivateKey, c Call) Call {
	c.Signature = ed25519.Sign(key, c.signed())
	return c
}

// VerifyCall is what every service does before acting: the signature, the
// address it was decided for, and the deadline (ATLAS-GATEWAY-002).
func VerifyCall(pub ed25519.PublicKey, c Call, address string, now time.Time) error {
	if len(c.Signature) == 0 || !ed25519.Verify(pub, c.signed(), c.Signature) || c.Address != address || now.After(c.Deadline) {
		return ErrCallRefused
	}
	return nil
}

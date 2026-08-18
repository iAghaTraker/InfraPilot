package identity

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"sync"
	"time"

	"github.com/iAghaTraker/InfraPilot/internal/errors"
)

const challengeLifetime = 2 * time.Minute

type challengeRecord struct {
	deviceID string
	value    []byte
	expires  time.Time
}
type Authenticator struct {
	repository *Repository
	mu         sync.Mutex
	challenges map[string]challengeRecord
}

func NewAuthenticator(repository *Repository) *Authenticator {
	return &Authenticator{repository: repository, challenges: make(map[string]challengeRecord)}
}

func (a *Authenticator) Challenge(ctx context.Context, deviceID string, now time.Time) (string, []byte, error) {
	device, err := a.repository.Get(ctx, deviceID)
	if err != nil {
		return "", nil, err
	}
	if device.Status != "active" {
		return "", nil, errors.New(errors.KindPermission, "identity.Challenge", "device identity is revoked")
	}
	idRaw, err := NewChallenge()
	if err != nil {
		return "", nil, err
	}
	value, err := NewChallenge()
	if err != nil {
		return "", nil, err
	}
	id := base64.RawURLEncoding.EncodeToString(idRaw)
	a.mu.Lock()
	a.challenges[id] = challengeRecord{deviceID: deviceID, value: value, expires: now.Add(challengeLifetime)}
	a.mu.Unlock()
	return id, append([]byte(nil), value...), nil
}

func (a *Authenticator) Verify(ctx context.Context, challengeID string, signature []byte, now time.Time) error {
	a.mu.Lock()
	record, ok := a.challenges[challengeID]
	delete(a.challenges, challengeID)
	a.mu.Unlock()
	if !ok || !now.Before(record.expires) {
		return errors.New(errors.KindPermission, "identity.Verify", "authentication challenge is invalid or expired")
	}
	device, err := a.repository.Get(ctx, record.deviceID)
	if err != nil {
		return err
	}
	if device.Status != "active" {
		return errors.New(errors.KindPermission, "identity.Verify", "device identity is revoked")
	}
	if !ed25519.Verify(device.PublicKey, record.value, signature) {
		return errors.New(errors.KindPermission, "identity.Verify", "authentication signature is invalid")
	}
	return nil
}

type DangerousActionAuthorizer interface {
	Authorize(context.Context, string, []byte) error
}

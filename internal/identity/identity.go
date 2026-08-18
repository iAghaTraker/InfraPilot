// Package identity contains local device identity and authentication primitives.
package identity

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/iAghaTraker/InfraPilot/internal/errors"
	"github.com/iAghaTraker/InfraPilot/internal/storage"
)

const tokenLifetime = 10 * time.Minute

type Identity struct {
	DeviceID   string
	PublicKey  ed25519.PublicKey
	privateKey ed25519.PrivateKey
}
type tokenEnvelope struct {
	Version   int    `json:"v"`
	DeviceID  string `json:"d"`
	PublicKey string `json:"p"`
	Nonce     string `json:"n"`
	ExpiresAt int64  `json:"e"`
	Signature string `json:"s"`
}
type Device struct {
	DeviceID  string
	PublicKey ed25519.PublicKey
	CreatedAt time.Time
	Status    string
}

func paths(dir string) (string, string, error) {
	if dir == "" || !filepath.IsAbs(dir) {
		return "", "", errors.New(errors.KindValidation, "identity.paths", "identity directory must be absolute")
	}
	return filepath.Join(dir, "device.key"), filepath.Join(dir, "device.json"), nil
}

func Load(dir string) (*Identity, error) {
	kp, mp, err := paths(dir)
	if err != nil {
		return nil, err
	}
	key, err := os.ReadFile(kp)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errors.New(errors.KindNotFound, "identity.Load", "no local device identity exists")
		}
		return nil, errors.Wrap(errors.KindPermission, "identity.Load", "cannot read private key", err)
	}
	if len(key) != ed25519.PrivateKeySize {
		return nil, errors.New(errors.KindStorage, "identity.Load", "stored private key is invalid")
	}
	var meta struct {
		DeviceID string `json:"device_id"`
	}
	data, err := os.ReadFile(mp)
	if err != nil {
		return nil, errors.Wrap(errors.KindStorage, "identity.Load", "cannot read device metadata", err)
	}
	if json.Unmarshal(data, &meta) != nil || meta.DeviceID == "" {
		return nil, errors.New(errors.KindStorage, "identity.Load", "device metadata is invalid")
	}
	priv := ed25519.PrivateKey(append([]byte(nil), key...))
	pub := append(ed25519.PublicKey(nil), priv.Public().(ed25519.PublicKey)...)
	return &Identity{DeviceID: meta.DeviceID, PublicKey: pub, privateKey: priv}, nil
}

func Create(dir string) (*Identity, string, error) {
	if _, err := Load(dir); err == nil {
		return nil, "", errors.New(errors.KindValidation, "identity.Create", "device identity already exists")
	} else if !errors.IsKind(err, errors.KindNotFound) {
		return nil, "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, "", errors.Wrap(errors.KindPermission, "identity.Create", "cannot create identity directory", err)
	}
	kp, mp, err := paths(dir)
	if err != nil {
		return nil, "", err
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, "", errors.Wrap(errors.KindInternal, "identity.Create", "cannot generate device key", err)
	}
	idb := make([]byte, 16)
	if _, err := rand.Read(idb); err != nil {
		return nil, "", err
	}
	id := base64.RawURLEncoding.EncodeToString(idb)
	f, err := os.OpenFile(kp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, "", errors.Wrap(errors.KindPermission, "identity.Create", "cannot create private key file", err)
	}
	if _, err = f.Write(priv); err != nil {
		_ = f.Close()
		_ = os.Remove(kp)
		return nil, "", err
	}
	if err = f.Close(); err != nil {
		return nil, "", err
	}
	meta, _ := json.Marshal(struct {
		DeviceID  string `json:"device_id"`
		CreatedAt string `json:"created_at"`
	}{id, time.Now().UTC().Format(time.RFC3339Nano)})
	if err = os.WriteFile(mp, meta, 0o600); err != nil {
		_ = os.Remove(kp)
		return nil, "", err
	}
	i := &Identity{DeviceID: id, PublicKey: pub, privateKey: priv}
	tok, err := i.PairingToken(time.Now().UTC())
	return i, tok, err
}

// Delete removes only the local identity files. The containing data directory
// and any remote/trusted-device records are left untouched.
func Delete(dir string) (string, error) {
	i, err := Load(dir)
	if err != nil {
		return "", err
	}
	kp, mp, err := paths(dir)
	if err != nil {
		return "", err
	}
	if err := os.Remove(kp); err != nil {
		return "", errors.Wrap(errors.KindPermission, "identity.Delete", "cannot remove private key", err)
	}
	if err := os.Remove(mp); err != nil {
		return "", errors.Wrap(errors.KindPermission, "identity.Delete", "cannot remove device metadata", err)
	}
	return i.DeviceID, nil
}

func (i *Identity) PairingToken(now time.Time) (string, error) {
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	e := tokenEnvelope{Version: 1, DeviceID: i.DeviceID, PublicKey: base64.RawURLEncoding.EncodeToString(i.PublicKey), Nonce: base64.RawURLEncoding.EncodeToString(nonce), ExpiresAt: now.Add(tokenLifetime).Unix()}
	payload, _ := json.Marshal(e)
	e.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(i.privateKey, payload))
	raw, _ := json.Marshal(e)
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func parseToken(token string, now time.Time) (tokenEnvelope, ed25519.PublicKey, error) {
	var e tokenEnvelope
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) > 4096 || json.Unmarshal(raw, &e) != nil || e.Version != 1 || e.DeviceID == "" || e.ExpiresAt < now.Unix() {
		return e, nil, errors.New(errors.KindValidation, "identity.parseToken", "pairing token is invalid or expired")
	}
	pb, err := base64.RawURLEncoding.DecodeString(e.PublicKey)
	if err != nil || len(pb) != ed25519.PublicKeySize {
		return e, nil, errors.New(errors.KindValidation, "identity.parseToken", "pairing token public key is invalid")
	}
	sig, err := base64.RawURLEncoding.DecodeString(e.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return e, nil, errors.New(errors.KindValidation, "identity.parseToken", "pairing token signature is invalid")
	}
	pub := ed25519.PublicKey(pb)
	e.Signature = ""
	payload, _ := json.Marshal(e)
	if !ed25519.Verify(pub, payload, sig) {
		return e, nil, errors.New(errors.KindValidation, "identity.parseToken", "pairing token signature is invalid")
	}
	return e, pub, nil
}

type Repository struct{ db *storage.DB }

func NewRepository(db *storage.DB) *Repository { return &Repository{db: db} }
func (r *Repository) Replace(ctx context.Context, token string, now time.Time) (Device, error) {
	e, pub, err := parseToken(token, now)
	if err != nil {
		r.audit(ctx, "pairing_rejected", "", false, "invalid or expired token", now)
		return Device{}, err
	}
	h := sha256.Sum256([]byte(token))
	tx, err := r.db.SQL().BeginTx(ctx, nil)
	if err != nil {
		return Device{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var x string
	if tx.QueryRowContext(ctx, "SELECT status FROM pairing_tokens WHERE token_hash=?", h[:]).Scan(&x) == nil {
		return Device{}, errors.New(errors.KindValidation, "identity.Replace", "pairing token has already been used")
	}
	if tx.QueryRowContext(ctx, "SELECT device_id FROM device_identities WHERE device_id=?", e.DeviceID).Scan(&x) == nil {
		return Device{}, errors.New(errors.KindValidation, "identity.Replace", "device identity is already registered")
	}
	if tx.QueryRowContext(ctx, "SELECT device_id FROM device_identities WHERE public_key=?", []byte(pub)).Scan(&x) == nil {
		return Device{}, errors.New(errors.KindValidation, "identity.Replace", "public key is already registered")
	}
	stamp := now.UTC().Format(time.RFC3339Nano)
	if _, err = tx.ExecContext(ctx, "INSERT INTO device_identities(device_id,public_key,created_at,status) VALUES(?,?,?,?)", e.DeviceID, []byte(pub), stamp, "active"); err != nil {
		return Device{}, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO pairing_tokens(token_id,token_hash,device_id,created_at,expires_at,used_at,status) VALUES(?,?,?,?,?,?,?)", e.DeviceID, h[:], e.DeviceID, stamp, time.Unix(e.ExpiresAt, 0).UTC().Format(time.RFC3339Nano), stamp, "used"); err != nil {
		return Device{}, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO security_audit(event_type,device_id,success,reason,occurred_at) VALUES(?,?,?,?,?)", "pairing_succeeded", e.DeviceID, 1, "", stamp); err != nil {
		return Device{}, err
	}
	if err = tx.Commit(); err != nil {
		return Device{}, err
	}
	return Device{DeviceID: e.DeviceID, PublicKey: pub, CreatedAt: now, Status: "active"}, nil
}

func (r *Repository) audit(ctx context.Context, event, deviceID string, success bool, reason string, now time.Time) {
	ok := 0
	if success {
		ok = 1
	}
	_, _ = r.db.SQL().ExecContext(ctx, "INSERT INTO security_audit(event_type,device_id,success,reason,occurred_at) VALUES(?,?,?,?,?)", event, deviceID, ok, reason, now.UTC().Format(time.RFC3339Nano))
}
func (r *Repository) List(ctx context.Context) ([]Device, error) {
	rows, err := r.db.SQL().QueryContext(ctx, "SELECT device_id,public_key,created_at,status FROM device_identities ORDER BY created_at")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Device
	for rows.Next() {
		var d Device
		var p []byte
		var stamp string
		if err := rows.Scan(&d.DeviceID, &p, &stamp, &d.Status); err != nil {
			return nil, err
		}
		d.PublicKey = ed25519.PublicKey(p)
		d.CreatedAt, _ = time.Parse(time.RFC3339Nano, stamp)
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *Repository) Get(ctx context.Context, id string) (Device, error) {
	var d Device
	var p []byte
	var stamp string
	err := r.db.SQL().QueryRowContext(ctx, "SELECT device_id,public_key,created_at,status FROM device_identities WHERE device_id=?", id).Scan(&d.DeviceID, &p, &stamp, &d.Status)
	if err != nil {
		return Device{}, errors.New(errors.KindNotFound, "identity.Get", "device identity was not found")
	}
	d.PublicKey = ed25519.PublicKey(p)
	d.CreatedAt, _ = time.Parse(time.RFC3339Nano, stamp)
	return d, nil
}
func (r *Repository) Revoke(ctx context.Context, id string, now time.Time) error {
	res, err := r.db.SQL().ExecContext(ctx, "UPDATE device_identities SET status='revoked' WHERE device_id=? AND status='active'", id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errors.New(errors.KindNotFound, "identity.Revoke", "active device identity was not found")
	}
	_, err = r.db.SQL().ExecContext(ctx, "INSERT INTO security_audit(event_type,device_id,success,reason,occurred_at) VALUES(?,?,?,?,?)", "device_revoked", id, 1, "", now.UTC().Format(time.RFC3339Nano))
	return err
}

func VerifySignature(pub ed25519.PublicKey, challenge, signature []byte) bool {
	return len(pub) == ed25519.PublicKeySize && ed25519.Verify(pub, challenge, signature)
}
func (i *Identity) Sign(challenge []byte) ([]byte, error) {
	if len(i.privateKey) != ed25519.PrivateKeySize {
		return nil, errors.New(errors.KindPermission, "identity.Sign", "private key is unavailable")
	}
	return ed25519.Sign(i.privateKey, challenge), nil
}
func NewChallenge() ([]byte, error)                { b := make([]byte, 32); _, err := rand.Read(b); return b, err }
func DecodeChallenge(value string) ([]byte, error) { return base64.RawURLEncoding.DecodeString(value) }
func EncodeSignature(value []byte) string          { return base64.RawURLEncoding.EncodeToString(value) }

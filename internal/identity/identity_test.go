package identity

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iAghaTraker/InfraPilot/internal/storage"
)

func testRepository(t *testing.T) *Repository {
	t.Helper()
	dir := t.TempDir()
	db, err := storage.Open(t.Context(), storage.Options{Path: filepath.Join(dir, "test.db"), BusyTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewRepository(db)
}

func TestCreatePersistsSecureIdentity(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "identity")
	created, token, err := Create(dir)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if created.DeviceID != loaded.DeviceID || !bytes.Equal(created.PublicKey, loaded.PublicKey) {
		t.Fatal("persisted identity changed")
	}
	info, err := os.Stat(filepath.Join(dir, "device.key"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("private key mode=%#o", info.Mode().Perm())
	}
	if token == "" {
		t.Fatal("empty pairing token")
	}
	if strings.Contains(token, base64.RawURLEncoding.EncodeToString(created.privateKey)) {
		t.Fatal("token contains private key")
	}
	if _, _, err := Create(dir); err == nil {
		t.Fatal("duplicate identity creation succeeded")
	}
}

func TestSignatureProofOfPossession(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "identity")
	id, _, err := Create(dir)
	if err != nil {
		t.Fatal(err)
	}
	challenge, err := NewChallenge()
	if err != nil {
		t.Fatal(err)
	}
	sig, err := id.Sign(challenge)
	if err != nil {
		t.Fatal(err)
	}
	if !VerifySignature(id.PublicKey, challenge, sig) {
		t.Fatal("valid signature rejected")
	}
	modified := append([]byte(nil), challenge...)
	modified[0] ^= 1
	if VerifySignature(id.PublicKey, modified, sig) {
		t.Fatal("modified challenge accepted")
	}
	sig[0] ^= 1
	if VerifySignature(id.PublicKey, challenge, sig) {
		t.Fatal("modified signature accepted")
	}
	wrongPub, _, _ := ed25519.GenerateKey(rand.Reader)
	if VerifySignature(wrongPub, challenge, sig) {
		t.Fatal("wrong public key accepted")
	}
}

func TestPairingLifecycle(t *testing.T) {
	repo := testRepository(t)
	dir := filepath.Join(t.TempDir(), "identity")
	id, _, err := Create(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	token, err := id.PairingToken(now)
	if err != nil {
		t.Fatal(err)
	}
	device, err := repo.Replace(t.Context(), token, now)
	if err != nil {
		t.Fatal(err)
	}
	if device.DeviceID != id.DeviceID {
		t.Fatal("wrong paired device")
	}
	if _, err := repo.Replace(t.Context(), token, now); err == nil {
		t.Fatal("used token accepted")
	}
	devices, err := repo.List(t.Context())
	if err != nil || len(devices) != 1 {
		t.Fatalf("devices=%v err=%v", devices, err)
	}
	if err := repo.Revoke(t.Context(), id.DeviceID, now); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(t.Context(), id.DeviceID)
	if err != nil || got.Status != "revoked" {
		t.Fatalf("device=%v err=%v", got, err)
	}
}

func TestPairingRejectsExpiredInvalidAndDuplicateKeys(t *testing.T) {
	repo := testRepository(t)
	dir := filepath.Join(t.TempDir(), "identity")
	id, _, _ := Create(dir)
	now := time.Now().UTC()
	expired, _ := id.PairingToken(now.Add(-tokenLifetime - time.Second))
	if _, err := repo.Replace(t.Context(), expired, now); err == nil {
		t.Fatal("expired token accepted")
	}
	if _, err := repo.Replace(t.Context(), "unguessable-is-not-valid", now); err == nil {
		t.Fatal("invalid token accepted")
	}
	token, _ := id.PairingToken(now)
	if _, err := repo.Replace(t.Context(), token, now); err != nil {
		t.Fatal(err)
	}
	id.DeviceID = "another-device"
	duplicate, _ := id.PairingToken(now)
	if _, err := repo.Replace(t.Context(), duplicate, now); err == nil || !strings.Contains(err.Error(), "public key") {
		t.Fatalf("duplicate key error=%v", err)
	}
}

func TestTokensUseFreshRandomness(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "identity")
	id, _, _ := Create(dir)
	now := time.Now().UTC()
	a, _ := id.PairingToken(now)
	b, _ := id.PairingToken(now)
	if a == b {
		t.Fatal("pairing tokens repeated")
	}
}

func TestChallengeIsOneTimeAndRejectsRevokedDevice(t *testing.T) {
	repo := testRepository(t)
	dir := filepath.Join(t.TempDir(), "identity")
	id, _, _ := Create(dir)
	now := time.Now().UTC()
	token, _ := id.PairingToken(now)
	if _, err := repo.Replace(t.Context(), token, now); err != nil {
		t.Fatal(err)
	}
	auth := NewAuthenticator(repo)
	challengeID, challenge, err := auth.Challenge(t.Context(), id.DeviceID, now)
	if err != nil {
		t.Fatal(err)
	}
	sig, _ := id.Sign(challenge)
	if err := auth.Verify(t.Context(), challengeID, sig, now); err != nil {
		t.Fatal(err)
	}
	if err := auth.Verify(t.Context(), challengeID, sig, now); err == nil {
		t.Fatal("replayed challenge accepted")
	}
	challengeID, challenge, _ = auth.Challenge(t.Context(), id.DeviceID, now)
	sig, _ = id.Sign(challenge)
	if err := repo.Revoke(t.Context(), id.DeviceID, now); err != nil {
		t.Fatal(err)
	}
	if err := auth.Verify(t.Context(), challengeID, sig, now); err == nil {
		t.Fatal("revoked device authenticated")
	}
}

func TestAuditContainsNoRawToken(t *testing.T) {
	repo := testRepository(t)
	dir := filepath.Join(t.TempDir(), "identity")
	id, _, _ := Create(dir)
	now := time.Now().UTC()
	token, _ := id.PairingToken(now)
	if _, err := repo.Replace(t.Context(), token, now); err != nil {
		t.Fatal(err)
	}
	var audit string
	if err := repo.db.SQL().QueryRowContext(t.Context(), "SELECT event_type||COALESCE(reason,'') FROM security_audit LIMIT 1").Scan(&audit); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(audit, token) {
		t.Fatal("audit leaked raw token")
	}
	var stored []byte
	if err := repo.db.SQL().QueryRowContext(t.Context(), "SELECT token_hash FROM pairing_tokens LIMIT 1").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(stored, []byte(token)) {
		t.Fatal("database stored raw token")
	}
}

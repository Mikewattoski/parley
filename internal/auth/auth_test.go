package auth

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

const (
	secretA = "0123456789abcdef0123456789abcdef"
	secretB = "fedcba9876543210fedcba9876543210"
)

func newTestService(t *testing.T, secret string) *Service {
	t.Helper()
	svc, err := NewService(secret)
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func TestPasswordHashing(t *testing.T) {
	old := BcryptCost
	BcryptCost = bcrypt.MinCost
	defer func() { BcryptCost = old }()

	hash, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if hash == "correct horse" {
		t.Fatal("password was stored in clear text")
	}
	if !CheckPassword(hash, "correct horse") {
		t.Error("correct password rejected")
	}
	if CheckPassword(hash, "wrong horse") {
		t.Error("wrong password accepted")
	}
}

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name    string
		pw      string
		wantErr bool
	}{
		{"too short", "short", true},
		{"minimum length", "12345678", false},
		{"typical", "correct horse battery", false},
		{"over bcrypt's 72-byte limit", strings.Repeat("a", 73), true},
		{"exactly 72 bytes", strings.Repeat("a", 72), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidatePassword(tc.pw); (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestIssueAndVerify(t *testing.T) {
	svc := newTestService(t, secretA)
	tok, err := svc.Issue(42, "alice", AudienceAPI, time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	id, err := svc.Verify(tok, AudienceAPI)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if id.UserID != 42 || id.Username != "alice" {
		t.Errorf("identity = %+v", id)
	}

	// a token for one audience must not work for the other
	if _, err := svc.Verify(tok, AudienceWS); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("wrong audience: err = %v, want ErrInvalidToken", err)
	}
}

func TestExpiredTokenIsRejected(t *testing.T) {
	past := newTestService(t, secretA)
	past.now = func() time.Time { return time.Now().Add(-time.Hour) }
	tok, err := past.Issue(1, "alice", AudienceAPI, time.Minute) // expired 59 minutes ago
	if err != nil {
		t.Fatal(err)
	}
	if _, err := newTestService(t, secretA).Verify(tok, AudienceAPI); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("err = %v, want ErrInvalidToken", err)
	}
}

func TestTokenSignedWithAnotherSecretIsRejected(t *testing.T) {
	tok, err := newTestService(t, secretA).Issue(1, "alice", AudienceAPI, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := newTestService(t, secretB).Verify(tok, AudienceAPI); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("err = %v, want ErrInvalidToken", err)
	}
}

func TestUnsignedTokenIsRejected(t *testing.T) {
	claims := Claims{
		Username: "alice",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "1",
			Audience:  jwt.ClaimStrings{AudienceAPI},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := newTestService(t, secretA).Verify(tok, AudienceAPI); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("alg=none token accepted: err = %v", err)
	}
}

func TestShortSecretIsRejected(t *testing.T) {
	if _, err := NewService("too-short"); err == nil {
		t.Fatal("expected an error for a short secret")
	}
}

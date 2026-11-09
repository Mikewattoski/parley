// Package auth handles password hashing and signed tokens.
package auth

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

const (
	// AudienceAPI tokens authorise REST calls; AudienceWS tickets only open a WebSocket.
	AudienceAPI = "api"
	AudienceWS  = "ws"

	APITokenTTL = 24 * time.Hour
	WSTicketTTL = 30 * time.Second

	MinPasswordLen   = 8
	maxPasswordBytes = 72 // bcrypt ignores everything after 72 bytes
	minSecretLen     = 32
)

var (
	ErrInvalidToken = errors.New("invalid or expired token")

	// BcryptCost is a variable so tests can lower it.
	BcryptCost = bcrypt.DefaultCost

	dummyHash = mustHash("parley-dummy-password")
)

func mustHash(pw string) string {
	h, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		panic(err)
	}
	return string(h)
}

func ValidatePassword(pw string) error {
	if len(pw) < MinPasswordLen {
		return fmt.Errorf("password must be at least %d characters", MinPasswordLen)
	}
	if len(pw) > maxPasswordBytes {
		return fmt.Errorf("password must be at most %d bytes", maxPasswordBytes)
	}
	return nil
}

func HashPassword(pw string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(pw), BcryptCost)
	if err != nil {
		return "", err
	}
	return string(h), nil
}

func CheckPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

// BurnPasswordCheck spends the time of a real check. Login calls it for
// unknown usernames so response timing does not reveal which users exist.
func BurnPasswordCheck(pw string) {
	_ = bcrypt.CompareHashAndPassword([]byte(dummyHash), []byte(pw))
}

type Claims struct {
	Username string `json:"username"`
	jwt.RegisteredClaims
}

// Identity is who a verified token belongs to.
type Identity struct {
	UserID   int64
	Username string
}

type Service struct {
	secret []byte
	now    func() time.Time
}

func NewService(secret string) (*Service, error) {
	if len(secret) < minSecretLen {
		return nil, fmt.Errorf("secret must be at least %d characters", minSecretLen)
	}
	return &Service{secret: []byte(secret), now: time.Now}, nil
}

// Issue signs a token for the given audience.
func (s *Service) Issue(userID int64, username, audience string, ttl time.Duration) (string, error) {
	now := s.now()
	claims := Claims{
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.FormatInt(userID, 10),
			Audience:  jwt.ClaimStrings{audience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
}

// Verify checks signature, algorithm, expiry and audience.
func (s *Service) Verify(token, audience string) (Identity, error) {
	var claims Claims
	_, err := jwt.ParseWithClaims(token, &claims,
		func(*jwt.Token) (any, error) { return s.secret, nil },
		jwt.WithValidMethods([]string{"HS256"}), // rejects "alg: none" and algorithm confusion
		jwt.WithAudience(audience),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(s.now),
	)
	if err != nil {
		return Identity{}, ErrInvalidToken
	}
	id, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil || claims.Username == "" {
		return Identity{}, ErrInvalidToken
	}
	return Identity{UserID: id, Username: claims.Username}, nil
}

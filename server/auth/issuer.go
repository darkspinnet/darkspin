package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// JWTIssuer creates short-lived launch credentials accepted by JWTVerifier.
type JWTIssuer struct {
	secret   []byte
	issuer   string
	audience string
	now      func() time.Time
}

// NewJWTIssuer constructs an HS256 launch-token issuer.
func NewJWTIssuer(secret []byte, issuer, audience string) (*JWTIssuer, error) {
	if len(secret) < 32 {
		return nil, errors.New("create JWT issuer: secret must contain at least 32 bytes")
	}
	if strings.TrimSpace(issuer) == "" {
		return nil, errors.New("create JWT issuer: issuer is empty")
	}
	if strings.TrimSpace(audience) == "" {
		return nil, errors.New("create JWT issuer: audience is empty")
	}
	return &JWTIssuer{
		secret: append([]byte(nil), secret...), issuer: issuer, audience: audience, now: time.Now,
	}, nil
}

// Issue creates a token carrying the requested local account identity.
func (i *JWTIssuer) Issue(identity string, lifetime time.Duration) (string, error) {
	identity = strings.TrimSpace(identity)
	if identity == "" {
		return "", errors.New("issue JWT: identity is empty")
	}
	if lifetime <= 0 {
		return "", errors.New("issue JWT: lifetime is invalid")
	}
	header, err := json.Marshal(jwtHeader{Algorithm: "HS256", Type: "JWT"})
	if err != nil {
		return "", fmt.Errorf("headerMarshal: %w", err)
	}
	now := i.now()
	claims, err := json.Marshal(map[string]any{
		"iss": i.issuer, "aud": i.audience, "sub": identity, "email": identity,
		"nbf": now.Unix(), "exp": now.Add(lifetime).Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("claimsMarshal: %w", err)
	}
	encodedHeader := base64.RawURLEncoding.EncodeToString(header)
	encodedClaims := base64.RawURLEncoding.EncodeToString(claims)
	signingInput := encodedHeader + "." + encodedClaims
	mac := hmac.New(sha256.New, i.secret)
	_, err = mac.Write([]byte(signingInput))
	if err != nil {
		return "", fmt.Errorf("signatureHash: %w", err)
	}
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return signingInput + "." + signature, nil
}

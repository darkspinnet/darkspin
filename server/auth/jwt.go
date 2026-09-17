// Package auth validates external credentials used to establish Dark Spin sessions.
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

const maximumJWTLength = 16 * 1024

// JWTVerifier validates short-lived HS256 launch credentials issued by a
// trusted website or account service.
type JWTVerifier struct {
	secret   []byte
	issuer   string
	audience string
	now      func() time.Time
}

type jwtHeader struct {
	Algorithm string `json:"alg"`
	Type      string `json:"typ"`
}

type jwtClaims struct {
	Issuer    string          `json:"iss"`
	Audience  json.RawMessage `json:"aud"`
	Subject   string          `json:"sub"`
	Email     string          `json:"email"`
	ExpiresAt int64           `json:"exp"`
	NotBefore int64           `json:"nbf"`
}

// NewJWTVerifier constructs a verifier. Issuer and audience are mandatory so
// a valid token created for another service cannot be replayed against Dark Spin.
func NewJWTVerifier(secret []byte, issuer, audience string) (*JWTVerifier, error) {
	if len(secret) < 32 {
		return nil, errors.New("create JWT verifier: secret must contain at least 32 bytes")
	}
	if issuer == "" {
		return nil, errors.New("create JWT verifier: issuer is empty")
	}
	if audience == "" {
		return nil, errors.New("create JWT verifier: audience is empty")
	}
	secretCopy := append([]byte(nil), secret...)
	return &JWTVerifier{secret: secretCopy, issuer: issuer, audience: audience, now: time.Now}, nil
}

// Verify validates a JWT and returns the account login name carried by its email
// claim, falling back to subject for issuers that use the email as subject.
func (v *JWTVerifier) Verify(token string) (string, error) {
	if v == nil {
		return "", errors.New("JWT authentication is disabled")
	}
	if len(token) == 0 || len(token) > maximumJWTLength {
		return "", errors.New("JWT length is invalid")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", errors.New("JWT must contain three segments")
	}

	headerContents, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", fmt.Errorf("headerDecode: %w", err)
	}
	header := jwtHeader{}
	err = json.Unmarshal(headerContents, &header)
	if err != nil {
		return "", fmt.Errorf("headerParse: %w", err)
	}
	if header.Algorithm != "HS256" {
		return "", errors.New("JWT algorithm is not HS256")
	}
	if header.Type != "" && !strings.EqualFold(header.Type, "JWT") {
		return "", errors.New("JWT type is invalid")
	}

	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return "", fmt.Errorf("signatureDecode: %w", err)
	}
	mac := hmac.New(sha256.New, v.secret)
	_, err = mac.Write([]byte(parts[0] + "." + parts[1]))
	if err != nil {
		return "", fmt.Errorf("signatureHash: %w", err)
	}
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return "", errors.New("JWT signature is invalid")
	}

	claimContents, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("claimsDecode: %w", err)
	}
	claims := jwtClaims{}
	err = json.Unmarshal(claimContents, &claims)
	if err != nil {
		return "", fmt.Errorf("claimsParse: %w", err)
	}
	if claims.Issuer != v.issuer {
		return "", errors.New("JWT issuer is invalid")
	}
	audience, err := claimAudience(claims.Audience)
	if err != nil {
		return "", fmt.Errorf("audienceParse: %w", err)
	}
	if !containsAudience(audience, v.audience) {
		return "", errors.New("JWT audience is invalid")
	}
	now := v.now().Unix()
	if claims.ExpiresAt == 0 || now >= claims.ExpiresAt {
		return "", errors.New("JWT has expired")
	}
	if claims.NotBefore != 0 && now < claims.NotBefore {
		return "", errors.New("JWT is not active")
	}
	loginName := strings.TrimSpace(claims.Email)
	if loginName == "" {
		loginName = strings.TrimSpace(claims.Subject)
	}
	if loginName == "" {
		return "", errors.New("JWT has no account identity")
	}
	return loginName, nil
}

func claimAudience(contents json.RawMessage) ([]string, error) {
	if len(contents) == 0 {
		return nil, nil
	}
	value := ""
	err := json.Unmarshal(contents, &value)
	if err == nil {
		return []string{value}, nil
	}
	values := []string{}
	err = json.Unmarshal(contents, &values)
	if err != nil {
		return nil, fmt.Errorf("audienceDecode: %w", err)
	}
	return values, nil
}

func containsAudience(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

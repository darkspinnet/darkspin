package auth

import (
	"net"
	"time"
)

type credentialAttempt struct {
	count     int
	expiresAt time.Time
}

// Limit the socket peer, never caller-controlled forwarding headers. Dedicated
// brokers also limit loopback clients because those may arrive through a proxy.
func (e *LocalBroker) allowCredentialAttempt(remoteAddress string) bool {
	host, port, err := net.SplitHostPort(remoteAddress)
	if err != nil || port == "" {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	source := ip.String()
	e.mu.Lock()
	defer e.mu.Unlock()
	now := e.now()
	for key, entry := range e.attempts {
		if !now.Before(entry.expiresAt) {
			delete(e.attempts, key)
		}
	}
	entry, isFound := e.attempts[source]
	if !isFound && len(e.attempts) >= 4096 || entry.count >= 30 {
		return false
	}
	if !isFound {
		entry.expiresAt = now.Add(time.Minute)
	}
	entry.count++
	if e.attempts == nil {
		e.attempts = make(map[string]credentialAttempt)
	}
	e.attempts[source] = entry
	return true
}

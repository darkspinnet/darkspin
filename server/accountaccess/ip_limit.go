package accountaccess

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
)

// ProtectedStatus is the first status requiring an IP limit for non-loopback
// network access. Empty limits lock those peers out until an operator sets one.
const ProtectedStatus int64 = 250

type peerContextKey struct{}

type peerContext struct {
	address   netip.Addr
	isTrusted bool
}

// WithPeer must receive a socket peer, never a forwarded header or advertised
// client endpoint. It overwrites any earlier trusted or peer context.
func WithPeer(ctx context.Context, rawAddress string) context.Context {
	if ctx == nil {
		return nil
	}
	address, err := netip.ParseAddr(rawAddress)
	if err != nil {
		host, port, splitErr := net.SplitHostPort(rawAddress)
		if splitErr == nil && port != "" {
			address, err = netip.ParseAddr(host)
		}
		if err != nil {
			address = netip.Addr{}
		}
	}
	if address.IsValid() {
		address = address.WithZone("").Unmap()
	}
	return context.WithValue(ctx, peerContextKey{}, peerContext{address: address})
}

// WithTrustedLocal is reserved for explicit in-process operator paths. Loopback
// peers bypass only the IP limit; they still require normal account authorization.
func WithTrustedLocal(ctx context.Context) context.Context {
	if ctx == nil {
		return nil
	}
	return context.WithValue(ctx, peerContextKey{}, peerContext{isTrusted: true})
}

// NormalizeIPLimit accepts one numeric IP address or CIDR, never DNS names or
// lists. Mapped IPv4 prefixes normalize to the same family as observed peers.
func NormalizeIPLimit(rawLimit string) (string, error) {
	limit := strings.TrimSpace(rawLimit)
	if limit == "" {
		return "", nil
	}
	if len(limit) > 128 {
		return "", errors.New("IP limit exceeds 128 bytes")
	}
	address, addressErr := netip.ParseAddr(limit)
	if addressErr == nil {
		if address.Zone() != "" {
			return "", errors.New("IP limit must not contain an interface zone")
		}
		return address.Unmap().String(), nil
	}
	prefix, err := netip.ParsePrefix(limit)
	if err != nil {
		return "", errors.New("IP limit must be one numeric IP address or CIDR")
	}
	if prefix.Addr().Is4In6() {
		if prefix.Bits() < 96 {
			return "", errors.New("mapped IPv4 CIDR must use at least 96 prefix bits")
		}
		prefix = netip.PrefixFrom(prefix.Addr().Unmap(), prefix.Bits()-96)
	}
	return prefix.Masked().String(), nil
}

func CheckPeer(ctx context.Context, access Access) error {
	if access.Status < ProtectedStatus {
		return nil
	}
	if ctx == nil {
		return fmt.Errorf("ipContext: %w", ErrAccessDenied)
	}
	peer, isPresent := ctx.Value(peerContextKey{}).(peerContext)
	if isPresent && peer.isTrusted {
		return nil
	}
	if isPresent && peer.address.IsLoopback() {
		return nil
	}
	if !isPresent || !peer.address.IsValid() || access.IPLimit == "" {
		return fmt.Errorf("ipRequired: %w", ErrAccessDenied)
	}
	limit, err := NormalizeIPLimit(access.IPLimit)
	if err != nil || limit == "" {
		return fmt.Errorf("ipLimit: %w", ErrAccessDenied)
	}
	address, addressErr := netip.ParseAddr(limit)
	if addressErr == nil {
		if address != peer.address {
			return fmt.Errorf("ipAddress: %w", ErrAccessDenied)
		}
		return nil
	}
	prefix, err := netip.ParsePrefix(limit)
	if err != nil || !prefix.Contains(peer.address) {
		return fmt.Errorf("ipNetwork: %w", ErrAccessDenied)
	}
	return nil
}

type SetIPLimitRequest struct {
	UserID    int64
	LoginName string
	IPLimit   string
	Reason    string
}

type IPLimitChange struct {
	Before Access
	After  Access
}

type IPLimitStore interface {
	SetIPLimit(context.Context, SetIPLimitRequest) (IPLimitChange, error)
}

func ValidateIPLimitRequest(req SetIPLimitRequest) error {
	err := ValidateStatusRequest(SetStatusRequest{UserID: req.UserID, LoginName: req.LoginName, Reason: req.Reason})
	if err != nil {
		return fmt.Errorf("limitIdentity: %w", err)
	}
	return nil
}

func (e *Service) SetIPLimit(ctx context.Context, req SetIPLimitRequest) (IPLimitChange, error) {
	if ctx == nil {
		return IPLimitChange{}, errors.New("nil context")
	}
	err := ValidateIPLimitRequest(req)
	if err != nil {
		return IPLimitChange{}, fmt.Errorf("limitValidate: %w", err)
	}
	limit, err := NormalizeIPLimit(req.IPLimit)
	if err != nil {
		return IPLimitChange{}, fmt.Errorf("limitParse: %w", err)
	}
	req.IPLimit = limit
	store, isSupported := e.store.(IPLimitStore)
	if !isSupported {
		return IPLimitChange{}, errors.New("account IP limit storage unavailable")
	}
	change, err := store.SetIPLimit(ctx, req)
	if err != nil {
		return IPLimitChange{}, fmt.Errorf("limitSave: %w", err)
	}
	return change, nil
}

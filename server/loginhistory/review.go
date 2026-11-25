package loginhistory

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/darkspinnet/darkspin/server/accountaccess"
)

type ReviewRequest struct {
	UserID                 int64
	Range, Reason, AfterIP string
	Until                  time.Time
	Limit                  int
}

type Window struct {
	UserID       int64
	Since, Until time.Time
	AfterIP      string
	Limit        int
}

// Entry describes a retained address span, not individual login events.
type Entry struct {
	IPAddress               string
	FirstSeenAt, LastSeenAt time.Time
	LoginCount              uint64
}

type Page struct {
	UserID       int64
	Range        string
	Since, Until time.Time
	Entries      []Entry
	NextIP       string
}

type ReviewStore interface {
	Read(context.Context, Window) ([]Entry, error)
}

type ReviewAccess interface {
	Authorize(context.Context, int64, int64, bool) (accountaccess.Access, error)
	Read(context.Context, int64) (accountaccess.Access, error)
	RecordLoginHistoryReview(context.Context, accountaccess.Actor, int64, string, string) error
}

type Reviewer struct {
	store  ReviewStore
	access ReviewAccess
}

func NewReviewer(store ReviewStore, access ReviewAccess) *Reviewer {
	return &Reviewer{store: store, access: access}
}

func (e *Reviewer) Review(ctx context.Context, actor accountaccess.Actor, req ReviewRequest) (Page, error) {
	if ctx == nil || e == nil || e.store == nil || e.access == nil {
		return Page{}, errors.New("login review unavailable")
	}
	err := accountaccess.ValidateLoginHistoryReview(actor, req.UserID, req.Range, req.Reason)
	if err != nil {
		return Page{}, fmt.Errorf("reviewValidate: %w", err)
	}
	if req.Limit <= 0 || req.Limit > 100 {
		return Page{}, fmt.Errorf("reviewLimit: %w", accountaccess.ErrInvalidRequest)
	}
	if req.AfterIP != "" {
		address, parseErr := netip.ParseAddr(req.AfterIP)
		if parseErr != nil || address.WithZone("").Unmap().String() != req.AfterIP {
			return Page{}, fmt.Errorf("reviewCursor: %w", accountaccess.ErrInvalidRequest)
		}
	}
	now := time.Now().UTC()
	if req.Until.IsZero() {
		req.Until = now
	}
	if req.Until.After(now) || req.Until.Before(now.Add(-365*24*time.Hour)) {
		return Page{}, fmt.Errorf("reviewUntil: %w", accountaccess.ErrInvalidRequest)
	}
	duration := time.Hour
	switch req.Range {
	case "day":
		duration = 24 * time.Hour
	case "week":
		duration = 7 * 24 * time.Hour
	case "month":
		duration = 30 * 24 * time.Hour
	case "year":
		duration = 365 * 24 * time.Hour
	}
	access, err := e.access.Authorize(ctx, actor.UserID, actor.Revision, true)
	if err != nil {
		return Page{}, fmt.Errorf("reviewAuthorize: %w", err)
	}
	if access.UserID != actor.UserID {
		return Page{}, fmt.Errorf("reviewActor: %w", accountaccess.ErrAccessDenied)
	}
	target, err := e.access.Read(ctx, req.UserID)
	if err != nil {
		return Page{}, fmt.Errorf("reviewTarget: %w", err)
	}
	if target.UserID != req.UserID || target.Revision <= 0 || target.Status < 0 {
		return Page{}, fmt.Errorf("reviewActive: %w", accountaccess.ErrAccessDenied)
	}
	window := Window{UserID: req.UserID, Since: req.Until.Add(-duration), Until: req.Until, AfterIP: req.AfterIP, Limit: req.Limit}
	entries, err := e.store.Read(ctx, window)
	if err != nil {
		return Page{}, fmt.Errorf("reviewRead: %w", err)
	}
	page := Page{UserID: req.UserID, Range: req.Range, Since: window.Since, Until: window.Until, Entries: entries}
	if len(entries) > req.Limit {
		page.Entries = entries[:req.Limit]
		page.NextIP = page.Entries[len(page.Entries)-1].IPAddress
	}
	// The audit transaction rechecks current persisted actor and target state.
	// Prepared addresses must never escape when authorization or audit fails.
	err = e.access.RecordLoginHistoryReview(ctx, actor, req.UserID, req.Range, req.Reason)
	if err != nil {
		return Page{}, fmt.Errorf("reviewAudit: %w", err)
	}
	return page, nil
}

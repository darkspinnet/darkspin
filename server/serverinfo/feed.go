package serverinfo

import (
	"context"
	"sort"
	"time"
)

// Announcements returns a detached newest-first feed of unexpired results.
func (e *Service) Announcements(ctx context.Context) []Announcement {
	if ctx == nil || ctx.Err() != nil {
		return nil
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	e.pruneAnnouncements(time.Now().UTC())
	return append([]Announcement(nil), e.announcements...)
}

func (e *Service) recordAnnouncement(announcement Announcement) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	e.announcements = append(e.announcements, announcement)
	sort.Slice(e.announcements, func(first, second int) bool {
		return e.announcements[first].CreatedAt.After(e.announcements[second].CreatedAt)
	})
	e.pruneAnnouncements(time.Now().UTC())
}

// Caller owns mutex. Keep storage bounded even when nobody polls the feed.
func (e *Service) pruneAnnouncements(now time.Time) {
	announcements := make([]Announcement, 0, min(20, len(e.announcements)))
	for _, announcement := range e.announcements {
		if !announcement.ExpiresAt.After(now) {
			continue
		}
		announcements = append(announcements, announcement)
		if len(announcements) == 20 {
			break
		}
	}
	e.announcements = announcements
}

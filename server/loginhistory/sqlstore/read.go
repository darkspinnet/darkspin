package sqlstore

import (
	"context"
	"fmt"
	"time"

	"github.com/darkspinnet/darkspin/server/loginhistory"
)

type loginRow struct {
	IPAddress   string `db:"ip_address"`
	FirstSeenAt int64  `db:"first_seen_at"`
	LastSeenAt  int64  `db:"last_seen_at"`
	LoginCount  uint64 `db:"login_count"`
}

func (e *Store) Read(ctx context.Context, req loginhistory.Window) ([]loginhistory.Entry, error) {
	rows := make([]loginRow, 0, req.Limit+1)
	// Span overlap cannot establish an individual login inside this range.
	err := e.db.SelectContext(ctx, &rows, e.db.Rebind(`SELECT h.ip_address, h.first_seen_at, h.last_seen_at, h.login_count
		FROM user_login_ip h JOIN "user" u ON u.id = h.user_id
		WHERE h.user_id = ? AND u.trmt_id = 0 AND h.first_seen_at <= ? AND h.last_seen_at >= ? AND h.ip_address > ?
		ORDER BY h.ip_address LIMIT ?`), req.UserID, req.Until.Unix(), req.Since.Unix(), req.AfterIP, req.Limit+1)
	if err != nil {
		return nil, fmt.Errorf("historyRead: %w", err)
	}
	entries := make([]loginhistory.Entry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, loginhistory.Entry{IPAddress: row.IPAddress, FirstSeenAt: time.Unix(row.FirstSeenAt, 0).UTC(), LastSeenAt: time.Unix(row.LastSeenAt, 0).UTC(), LoginCount: row.LoginCount})
	}
	return entries, nil
}

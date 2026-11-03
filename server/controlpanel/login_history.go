package controlpanel

import (
	"context"
	"net/http"
	"time"

	"github.com/darkspinnet/darkspin/server/accountaccess"
	"github.com/darkspinnet/darkspin/server/loginhistory"
)

type LoginHistoryReviewer interface {
	Review(context.Context, accountaccess.Actor, loginhistory.ReviewRequest) (loginhistory.Page, error)
}

type loginHistoryEntryDTO struct {
	IPAddress   string    `json:"ip_address"`
	FirstSeenAt time.Time `json:"first_seen_at"`
	LastSeenAt  time.Time `json:"last_seen_at"`
	LoginCount  uint64    `json:"login_count"`
}

type loginHistoryDTO struct {
	UserID     int64                  `json:"user_id"`
	Range      string                 `json:"range"`
	RangeStart time.Time              `json:"range_start"`
	Until      time.Time              `json:"until"`
	Entries    []loginHistoryEntryDTO `json:"entries"`
	NextIP     string                 `json:"next_ip"`
}

func (e *Service) reviewLoginHistory(writer http.ResponseWriter, req *http.Request, actor accountaccess.Actor) {
	if !e.config.IsDedicated || e.config.LoginHistoryReviewer == nil {
		writeError(writer, http.StatusNotFound, "Login history review is unavailable.")
		return
	}
	command := struct {
		UserID  int64     `json:"user_id"`
		Range   string    `json:"range"`
		Reason  string    `json:"reason"`
		AfterIP string    `json:"after_ip"`
		Until   time.Time `json:"until"`
		Limit   int       `json:"limit"`
	}{}
	err := decodeControlRequest(writer, req, &command)
	if err != nil {
		writeError(writer, http.StatusBadRequest, "Invalid login history request.")
		return
	}
	ctx, cancel := context.WithTimeout(req.Context(), 5*time.Second)
	defer cancel()
	page, err := e.config.LoginHistoryReviewer.Review(ctx, actor, loginhistory.ReviewRequest{
		UserID: command.UserID, Range: command.Range, Reason: command.Reason,
		AfterIP: command.AfterIP, Until: command.Until, Limit: command.Limit,
	})
	if err != nil {
		writeOperationError(writer, err)
		return
	}
	response := loginHistoryDTO{UserID: page.UserID, Range: page.Range, RangeStart: page.Since.UTC(), Until: page.Until.UTC(), NextIP: page.NextIP, Entries: make([]loginHistoryEntryDTO, 0, len(page.Entries))}
	for _, entry := range page.Entries {
		response.Entries = append(response.Entries, loginHistoryEntryDTO{IPAddress: entry.IPAddress, FirstSeenAt: entry.FirstSeenAt.UTC(), LastSeenAt: entry.LastSeenAt.UTC(), LoginCount: entry.LoginCount})
	}
	writeJSON(writer, http.StatusOK, response)
}

package controlpanel

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/darkspinnet/darkspin/server/accountaccess"
	"github.com/darkspinnet/darkspin/server/serverinfo"
)

type Information interface {
	Identity(context.Context) (serverinfo.Snapshot, error)
	SetIdentity(context.Context, accountaccess.Actor, serverinfo.SetIdentityRequest) (serverinfo.Snapshot, error)
	Announce(context.Context, accountaccess.Actor, string) (serverinfo.Announcement, error)
	Announcements(context.Context) []serverinfo.Announcement
}

type identityDTO struct {
	Revision    string `json:"revision"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Welcome     string `json:"welcome"`
	Rules       string `json:"rules"`
	ContactURL  string `json:"contact_url"`
}

type announcementDTO struct {
	ID                string    `json:"id"`
	Message           string    `json:"message"`
	CreatedAt         time.Time `json:"created_at"`
	ExpiresAt         time.Time `json:"expires_at"`
	Delivered         int       `json:"delivered"`
	IsDeliveryPartial bool      `json:"is_delivery_partial"`
	IsAuditRecorded   bool      `json:"is_audit_recorded"`
	Outcome           string    `json:"outcome"`
}

// Players receive the message feed without administrator-only delivery/audit details.
type playerAnnouncementDTO struct {
	ID        string    `json:"id"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

func identityResponse(snapshot serverinfo.Snapshot) identityDTO {
	return identityDTO{Revision: snapshot.Revision, Name: snapshot.Identity.Name,
		Description: snapshot.Identity.Description, Welcome: snapshot.Identity.Welcome,
		Rules: snapshot.Identity.Rules, ContactURL: snapshot.Identity.ContactURL}
}

func announcementResponse(announcement serverinfo.Announcement) announcementDTO {
	return announcementDTO{ID: announcement.ID, Message: announcement.Message,
		CreatedAt: announcement.CreatedAt, ExpiresAt: announcement.ExpiresAt,
		Delivered: announcement.Delivered, IsDeliveryPartial: announcement.IsDeliveryPartial,
		IsAuditRecorded: announcement.IsAuditRecorded, Outcome: announcement.Outcome}
}

func (e *Service) serverIdentity(writer http.ResponseWriter, req *http.Request) {
	if e.config.Information == nil {
		writeError(writer, http.StatusNotFound, "Server identity is unavailable.")
		return
	}
	snapshot, err := e.config.Information.Identity(req.Context())
	if err != nil {
		writeError(writer, http.StatusServiceUnavailable, "Server identity could not be read.")
		return
	}
	writeJSON(writer, http.StatusOK, identityResponse(snapshot))
}

func (e *Service) announcements(writer http.ResponseWriter, req *http.Request) {
	if e.config.Information == nil {
		writeError(writer, http.StatusNotFound, "Announcements are unavailable.")
		return
	}
	announcements := e.config.Information.Announcements(req.Context())
	responses := make([]playerAnnouncementDTO, 0, len(announcements))
	for _, announcement := range announcements {
		responses = append(responses, playerAnnouncementDTO{
			ID: announcement.ID, Message: announcement.Message, CreatedAt: announcement.CreatedAt, ExpiresAt: announcement.ExpiresAt,
		})
	}
	writeJSON(writer, http.StatusOK, struct {
		Announcements []playerAnnouncementDTO `json:"announcements"`
	}{Announcements: responses})
}

func (e *Service) setIdentity(writer http.ResponseWriter, req *http.Request, actor accountaccess.Actor) {
	if e.config.Information == nil {
		writeError(writer, http.StatusNotFound, "Server identity editing is unavailable.")
		return
	}
	command := identityDTO{}
	err := decodeBoundedControlRequest(writer, req, &command, 32*1024)
	if err != nil {
		writeError(writer, http.StatusBadRequest, "Invalid server identity.")
		return
	}
	snapshot, err := e.config.Information.SetIdentity(req.Context(), actor, serverinfo.SetIdentityRequest{
		ExpectedRevision: command.Revision, Identity: serverinfo.Identity{
			Name: command.Name, Description: command.Description, Welcome: command.Welcome,
			Rules: command.Rules, ContactURL: command.ContactURL,
		},
	})
	if err != nil {
		writeInformationError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, identityResponse(snapshot))
}

func (e *Service) announce(writer http.ResponseWriter, req *http.Request, actor accountaccess.Actor) {
	if e.config.Information == nil {
		writeError(writer, http.StatusNotFound, "Announcements are unavailable.")
		return
	}
	command := struct {
		Message string `json:"message"`
	}{}
	err := decodeControlRequest(writer, req, &command)
	if err != nil {
		writeError(writer, http.StatusBadRequest, "Invalid announcement.")
		return
	}
	announcement, err := e.config.Information.Announce(req.Context(), actor, command.Message)
	if announcement.ID != "" {
		// Accepted broadcasts return measured delivery outcomes, including total
		// failure and incomplete auditing. Never discard a partial delivery count.
		writeJSON(writer, http.StatusOK, announcementResponse(announcement))
		return
	}
	if err != nil {
		writeInformationError(writer, err)
		return
	}
	writeError(writer, http.StatusServiceUnavailable, "Announcement acceptance is unavailable.")
}

func writeInformationError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, serverinfo.ErrConflict):
		writeErrorCode(writer, http.StatusConflict, "conflict", "Server configuration changed. Reload before saving.")
	case errors.Is(err, serverinfo.ErrInvalidIdentity), errors.Is(err, serverinfo.ErrInvalidAnnouncement):
		writeError(writer, http.StatusBadRequest, "Check the server information and message limits.")
	case errors.Is(err, serverinfo.ErrRateLimited):
		writer.Header().Set("Retry-After", "10")
		writeError(writer, http.StatusTooManyRequests, "Wait ten seconds between announcements.")
	case errors.Is(err, serverinfo.ErrAuditIncomplete):
		writeErrorCode(writer, http.StatusServiceUnavailable, "outcome_unknown", "The configuration was saved but its audit outcome could not be recorded. Reload before retrying.")
	default:
		writeOperationError(writer, err)
	}
}

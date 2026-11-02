package chat

import (
	"context"
	"errors"
	"fmt"
)

var ErrNPCScaleUnavailable = errors.New("npc scale unavailable")

type NPCScaleRequest struct {
	Sender     Participant
	GameID     uint32
	Multiplier float32
	Radius     float32
	NounFilter string
	IsReset    bool
}

type NPCScalePreview struct {
	ObjectID    uint32  `json:"object_id"`
	NounName    string  `json:"noun_name"`
	Multiplier  float32 `json:"multiplier"`
	ObjectScale float32 `json:"object_scale"`
}

type NPCScalePreviewer interface {
	PreviewNPCScale(context.Context, NPCScaleRequest) ([]NPCScalePreview, error)
}

func (s *Service) UseNPCScalePreviewer(previewer NPCScalePreviewer) {
	s.npcScalePreviewer = previewer
}

func (s *Service) PreviewNPCScale(ctx context.Context, req NPCScaleRequest) ([]NPCScalePreview, error) {
	if req.Sender.ID == 0 || req.Sender.Name == "" || req.GameID == 0 {
		return nil, ErrSenderNotMember
	}
	if s.npcScalePreviewer == nil {
		return nil, ErrNPCScaleUnavailable
	}
	members, err := s.directory.Members(ctx, Target{Scope: ScopeGame, ID: uint64(req.GameID)})
	if err != nil {
		return nil, fmt.Errorf("scaleAudience: %w", err)
	}
	for _, member := range members {
		if member.ID != req.Sender.ID {
			continue
		}
		previews, err := s.npcScalePreviewer.PreviewNPCScale(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("scalePreview: %w", err)
		}
		return previews, nil
	}
	return nil, ErrSenderNotMember
}

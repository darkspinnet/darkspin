package game

import (
	"errors"
	"math"

	"github.com/darkspinnet/darkspin/server/sporenet"
)

// RecipientPartRequest carries the recipient and mission context at award time.
type RecipientPartRequest struct {
	Progression      uint32
	MissionStage     uint32
	ParticipantCount uint32
	IsChain          bool
}

// FinalizeCampaignRecipientPart applies the native recipient floor after item
// selection. It consumes no randomness and preserves the selected descriptor.
func (e *GameplayJoin) FinalizeCampaignRecipientPart(
	part sporenet.Part, req RecipientPartRequest,
) (sporenet.Part, error) {
	if e == nil || e.partCatalog == nil {
		return sporenet.Part{}, errors.New("recipient part catalog unavailable")
	}
	return e.partCatalog.finalizeCampaignRecipientPart(part, req), nil
}

func (e *PartCatalog) finalizeCampaignRecipientPart(
	part sporenet.Part, req RecipientPartRequest,
) sporenet.Part {
	if e == nil || e.tuning == nil {
		return part
	}
	// Native subtraction and comparison are signed. Keep the imported signed
	// gap intact, including a negative authored gap, without unsigned underflow.
	floorStage := int64(req.Progression) - int64(e.tuning.SidekickProgressionGap)
	if !e.tuning.IsSidekickingForced &&
		(!req.IsChain || req.ParticipantCount <= 1 || int64(req.MissionStage) > floorStage) {
		return part
	}
	stage := uint32(max(int64(1), min(floorStage, int64(math.MaxUint32))))
	floor := e.CampaignItemLevel(stage, part.Rarity, false, 1)
	part.Level = max(part.Level, uint16(min(floor, uint32(math.MaxUint16))))
	return part
}

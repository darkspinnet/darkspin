package gameplay

import (
	"context"
	"fmt"

	"github.com/darkspinnet/darkspin/server/chat"
	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/raknet"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

func (e Lifecycle) PreviewNPCScale(ctx context.Context, req chat.NPCScaleRequest) ([]chat.NPCScalePreview, error) {
	if e.npcScalePreviewer == nil {
		return nil, chat.ErrNPCScaleUnavailable
	}
	previews, err := e.npcScalePreviewer.PreviewNPCScale(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("scaleGameplay: %w", err)
	}
	return previews, nil
}

func (e *gameplaySessionRegistry) PreviewNPCScale(ctx context.Context, req chat.NPCScaleRequest) ([]chat.NPCScalePreview, error) {
	err := ctx.Err()
	if err != nil {
		return nil, fmt.Errorf("scaleContext: %w", err)
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	for _, member := range e.sessions {
		if member.binding.UserID != uint64(req.Sender.ID) || member.binding.GameID != req.GameID ||
			!member.stage.IsDungeon() || !member.dungeonSetup.IsCommitted() || member.isZoneTerminal() || member.zone == nil {
			continue
		}
		previews, err := member.zone.NPCs().PreviewScales(zonenpc.ScalePreviewRequest{
			Position: game.Vec3(member.playerPosition), Radius: req.Radius,
			Multiplier: req.Multiplier, NounFilter: req.NounFilter, IsReset: req.IsReset,
		})
		if err != nil {
			return nil, fmt.Errorf("scaleApply: %w", err)
		}
		packets := make([][]byte, 0, len(previews))
		results := make([]chat.NPCScalePreview, 0, len(previews))
		for _, preview := range previews {
			// cObject field 8 owns the rendered runtime scale (GetScale reads
			// object+16 in build 103). BodyScale's attribute cache is separate.
			packet, err := raknet.MarshalApplication(raknet.ObjectUpdateContractMessage{
				ObjectID: preview.Plan.ObjectID,
				Object:   raknet.ObjectReflection{Scale: &preview.ObjectScale},
			})
			if err != nil {
				return nil, fmt.Errorf("scaleMarshal: %w", err)
			}
			packets = append(packets, packet)
			results = append(results, chat.NPCScalePreview{ObjectID: preview.Plan.ObjectID,
				NounName: preview.Plan.NounName, Multiplier: preview.Plan.ScalePreviewMultiplier, ObjectScale: preview.ObjectScale})
		}
		for key, peer := range e.sessions {
			if peer.zone == member.zone && peer.stage.IsDungeon() {
				peer.queueCampaignPackets(packets)
				e.sessions[key] = peer
			}
		}
		return results, nil
	}
	return nil, chat.ErrNPCScaleUnavailable
}

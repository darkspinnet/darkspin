package gameplay

// Each observer owns one receipt for an accepted feedback operation. Prepared
// replacements and resource repairs share it; unrelated feedback never does.
// The gameplay registry lock owns all access to the receipt.
type mutationFeedbackReceipt struct {
	isCommitted bool
}

func mutationFeedbackResourceRepair(frame mutationDamageFeedback) mutationDamageFeedback {
	frame.packets = nil
	frame.hits = nil
	frame.isPrepared = false
	frame.areEffectsAllowed = false
	targets := make([]mutationFeedbackTarget, 0, len(frame.targets))
	for _, target := range frame.targets {
		actor, hit, isFound := mutationFeedbackActor(frame.world, target.objectID)
		if !isFound || actor.objectID != hit.objectID {
			continue
		}
		targets = append(targets, actor)
	}
	frame.targets = targets
	return frame
}

// A resource refresh can replace an in-flight batch under a newer queue ID.
// Acknowledging the earlier batch must retire that operation's structural
// presentation in its retained replacement as well as the acknowledged ID.
func (e *gameplayPeerSession) commitMutationFeedbackReceipts(frames []mutationDamageFeedback) {
	for _, frame := range frames {
		if frame.receipt == nil || e.zone != frame.world ||
			e.transportGeneration != frame.transportGeneration {
			continue
		}
		frame.receipt.isCommitted = true
		for index := range e.pendingPacketBatches {
			batch := &e.pendingPacketBatches[index]
			pending := batch.mutationFeedback
			if pending == nil || pending.receipt != frame.receipt {
				continue
			}
			next := mutationFeedbackResourceRepair(*pending)
			batch.mutationFeedback = &next
			// The next preparation reconstructs fresh resource deltas. Do not
			// leave an already prepared copy of the structural bytes queued.
			batch.packets = nil
		}
	}
}

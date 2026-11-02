package gameplay

import zonehorde "github.com/darkspinnet/darkspin/server/zone/horde"

// HordePolicySource supplies operator choices when a world is prepared.
// Active worlds keep their original policy for all waves and co-op members.
type HordePolicySource interface {
	HordePolicy() zonehorde.Policy
}

func (e campaignPreparation) hordePolicy() zonehorde.Policy {
	if e.hordePolicySource == nil {
		return zonehorde.DefaultPolicy()
	}
	return e.hordePolicySource.HordePolicy()
}

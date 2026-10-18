package horde

import "time"

// Post-activation admission is current server policy; native pacing is unresolved.
const InitialAdmissionDelay = time.Duration(0)

const (
	FirstClearSupportActivationDelay = 15 * time.Second
	ReplaySupportActivationDelay     = 2 * time.Second
)

// SupportActivationDelay is the wait before Lua chunk 62 calls
// ActivateHordeSpawn. Ordinary HordeTrigger_OnEnterPlayer markers use a
// separate callback and do not inherit this support-script timing.
func SupportActivationDelay(isAnyUnbeaten bool) time.Duration {
	if isAnyUnbeaten {
		return FirstClearSupportActivationDelay
	}
	return ReplaySupportActivationDelay
}

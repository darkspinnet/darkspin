// Package emote owns the allowlist of optional Spore creature expressions.
package emote

// SporeState returns a feature-owned state name, never an arbitrary asset ID.
// Fang registers these states against verified, retained Spore animation clips.
func SporeState(name string) (string, bool) {
	switch name {
	case "happy", "laugh", "scared", "swoon", "roar", "backflip", "flex", "yawn":
		return "mod_spore_" + name, true
	default:
		return "", false
	}
}

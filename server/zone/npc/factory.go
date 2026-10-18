package npc

import "strings"

// All three authored AIDefinitions use SentiosPipeDeath and retain dead graphics.
func IsFactoryPipe(nounName string) bool {
	return strings.EqualFold(nounName, "DEST_prefab_citadel_factorypipe.Noun") ||
		strings.EqualFold(nounName, "DEST_prefab_citadel_factorypipe_plasma.Noun") ||
		strings.EqualFold(nounName, "DEST_prefab_citadel_factorypipe_smoke.Noun")
}

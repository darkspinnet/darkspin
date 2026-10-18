package game

import (
	"strings"

	"github.com/darkspinnet/darkspin/server/util"
)

// NounType is the authored noun category from the native 43-entry enum at
// build 103 VA 0x116A1C8 (127: 0x11713E8). It is separate from director
// spawn kinds, creature types, and NonPlayerClass NPC types. Zero is unknown.
type NounType uint32

// NounTypeForAsset resolves a .Noun reference to its DBPF instance (filename
// stem), then reads the authored category. It does not hash a category label
// or infer one from the noun's spelling. Zero means category unavailable.
func (e CampaignDirector) NounTypeForAsset(nounName string) NounType {
	nounStem := strings.TrimSuffix(strings.ToLower(nounName), ".noun")
	return e.NounTypesByInstance[util.HashID(nounStem)]
}

const (
	NounTypeCreature             NounType = 0x06c27d00
	NounTypeVehicle              NounType = 0x78bddf27
	NounTypeObstacle             NounType = 0x02adb47a
	NounTypeSpawnPoint           NounType = 0xd9eaf104
	NounTypePathPoint            NounType = 0x9d99f2fa
	NounTypeTrigger              NounType = 0x3ce46113
	NounTypePointLight           NounType = 0x93555dcb
	NounTypeSpotLight            NounType = 0xbcd9673b
	NounTypeLineLight            NounType = 0x3d58111d
	NounTypeParallelLight        NounType = 0x8b1018a4
	NounTypeHemisphereLight      NounType = 0xe615afdb
	NounTypeAnimator             NounType = 0xf90527d6
	NounTypeAnimated             NounType = 0xf3051e56
	NounTypeGraphicsControl      NounType = 0xe9a24895
	NounTypeMaterial             NounType = 0xe6640542
	NounTypeFlora                NounType = 0x5bdcec35
	NounTypeLevelshopObject      NounType = 0xa12b2c18
	NounTypeTerrain              NounType = 0x151db008
	NounTypeWeapon               NounType = 0xe810d505
	NounTypeBuilding             NounType = 0xc710b6e9
	NounTypeHandle               NounType = 0xadb0a86b
	NounTypeHealthOrb            NounType = 0x75b43ff2
	NounTypeManaOrb              NounType = 0xf402465f
	NounTypeResurrectOrb         NounType = 0xc035aaad
	NounTypeMovie                NounType = 0x4927fa7f
	NounTypeLoot                 NounType = 0x292fea33
	NounTypePlaceableEffect      NounType = 0x383a0a75
	NounTypeLuaJob               NounType = 0xa2908a12
	NounTypeAbilityObject        NounType = 0x485fc991
	NounTypeLevelExitPoint       NounType = 0x087e8047
	NounTypeDecal                NounType = 0x4d7784b8
	NounTypeWater                NounType = 0x9e3c3dfa
	NounTypeGrass                NounType = 0xfd3d2ed9
	NounTypeDoor                 NounType = 0x6fedae4d
	NounTypeCrystal              NounType = 0xcd482419
	NounTypeInteractable         NounType = 0x0977af61
	NounTypeProjectile           NounType = 0x253f6f5c
	NounTypeDestructibleOrnament NounType = 0x0013fbb4
	NounTypeMapCamera            NounType = 0xfbfb36d0
	NounTypeOccluder             NounType = 0x071fd3d4
	NounTypeSplineCamera         NounType = 0x6cb99fff
	NounTypeSplineCameraNode     NounType = 0xfd487097
	NounTypeBossPortal           NounType = 0xc1b461bc
)

func (e CampaignDirector) IsProjectileNoun(nounName string) bool {
	nounStem := strings.TrimSuffix(strings.ToLower(nounName), ".noun")
	return e.NounProjectilesByInstance[util.HashID(nounStem)]
}

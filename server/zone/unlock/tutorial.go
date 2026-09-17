package unlock

import "time"

const PlayerObjectID = uint32(1)
const SageObjectID = uint32(17)

const AbilitySecondChunkID int64 = 930
const AbilitySecondSHA256 = "07c3a923a272fc2313dd00876044398597fd1107e833037cd0e2cf2b7e57ec64"
const AbilitySecondCallback = "nTutorial_IntroAbilitySecond.main"
const AbilitySecondDelay = time.Second

const SecondCreatureChunkID int64 = 215
const SecondCreatureSHA256 = "e98b1e855e64904689451bd79635c3487a7a20e29f1b943885c9648b61f8e30a"
const SecondCreatureCallback = "nTutorial_IntroSecondCreatureUnlock.main"
const SecondCreatureDelay = 500 * time.Millisecond

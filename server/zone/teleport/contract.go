package teleport

import (
	"time"

	"github.com/darkspinnet/darkspin/server/sim"
)

const (
	EntrantRole     sim.Role = "player"
	SecurityRole    sim.Role = "teleporter"
	ModifierGUID             = "0x502f1932"
	ModifierGUIDID  uint32   = 0x502f1932
	SecurityChunkID int64    = 144
	SecuritySHA256           = "c2e727c6cf4c13708fa315321fb51f0402a3a4a9763eaf2f8d782c1eca3267f0"
	ModifierChunkID int64    = 349
	ModifierSHA256           = "9417dfe9163dbb857dc1b423d4e127170a6ba9e128f583d648fe7f258a1c1cd1"
)

var Deadline = [...]time.Duration{500 * time.Millisecond, time.Second}

type Simulation struct {
	Activation sim.Program
	Entry      sim.Program
	Teleport   sim.Program
}

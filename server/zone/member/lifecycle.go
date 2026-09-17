package member

// Stage tracks one connected member's progress into a zone. It is
// connection-local admission state, not authoritative world state.
type Stage uint8

const (
	StageAwaitingStart Stage = iota
	StageChainVoteSent
	StageChainCountdownSent
	StageResumePending
	StagePreparing
	StageDungeon
)

func (e Stage) IsDungeon() bool {
	return e == StageDungeon
}

func (e Stage) CanSendChainVote() bool {
	return e == StageAwaitingStart
}

func (e *Stage) MarkChainVoteSent() bool {
	if e == nil || *e != StageAwaitingStart {
		return false
	}
	*e = StageChainVoteSent
	return true
}

func (e *Stage) MarkChainCountdownSent() bool {
	if e == nil || *e != StageChainVoteSent {
		return false
	}
	*e = StageChainCountdownSent
	return true
}

func (e *Stage) AwaitResume() {
	if e == nil {
		return
	}
	*e = StageResumePending
}

func (e Stage) CanResumePrepare() bool {
	return e == StageResumePending
}

func (e *Stage) BeginPrepare(isFallback bool) bool {
	if e == nil {
		return false
	}
	if *e == StagePreparing {
		return !isFallback
	}
	if *e != StageChainVoteSent && *e != StageChainCountdownSent &&
		*e != StageResumePending {
		return false
	}
	*e = StagePreparing
	return true
}

func (e *Stage) EnterDungeon() {
	if e == nil {
		return
	}
	*e = StageDungeon
}

type setupStage uint8

const (
	setupIdle setupStage = iota
	setupReserved
	setupCommitted
)

// Setup guards one member's dungeon snapshot transaction. Generation prevents
// a delayed producer from committing or rolling back a newer reservation.
type Setup struct {
	generation uint64
	stage      setupStage
}

func (e *Setup) Reserve() (uint64, bool) {
	if e == nil || e.stage != setupIdle {
		return 0, false
	}
	e.generation++
	e.stage = setupReserved
	return e.generation, true
}

func (e *Setup) Commit(generation uint64) bool {
	if e == nil || !e.IsReserved(generation) {
		return false
	}
	e.stage = setupCommitted
	return true
}

func (e *Setup) Rollback(generation uint64) bool {
	if e == nil || !e.IsReserved(generation) {
		return false
	}
	e.stage = setupIdle
	return true
}

func (e Setup) CanReserve() bool {
	return e.stage == setupIdle
}

func (e Setup) IsReserved(generation uint64) bool {
	return e.stage == setupReserved && e.generation == generation
}

func (e Setup) IsCommitted() bool {
	return e.stage == setupCommitted
}

func (e *Setup) Reset() {
	if e == nil {
		return
	}
	*e = Setup{}
}

func (e Setup) IsCommittedGeneration(generation uint64) bool {
	return e.stage == setupCommitted && e.generation == generation
}

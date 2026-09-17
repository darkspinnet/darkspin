package sim

import (
	"errors"
	"fmt"
	"time"
)

const FirstAggroStimulusMask uint32 = 0x20
const FirstAggroStimulusDuration = 10 * time.Second
const AggroFlagTrigger uint32 = 0x2
const AggroFlagSpawn uint32 = 0x4

// AggroInsertCommand is an authority-resolved request to add one target to an
// AI blackboard. Hostility and server authority are supplied by the world
// owner after object/team resolution; clients cannot assert them directly.
type AggroInsertCommand struct {
	SourceRole      Role
	TargetRole      Role
	Flags           uint32
	IsAuthoritative bool
	IsHostile       bool
}

type AggroInsertResult struct {
	IsAccepted       bool
	IsFirstAggro     bool
	StimulusMask     uint32
	StimulusDuration time.Duration
}

// AggroState retains the engine-owned one-shot first-aggro state for one AI
// object. Lua presentation begins only after EvaluateFirstAggro consumes the
// pending stimulus on an AI evaluation.
type AggroState struct {
	sourceRole             Role
	targets                map[Role]struct{}
	pendingFirstTargetRole Role
	agentState             uint8
	isFirstAggroConsumed   bool
}

func NewAggroState(sourceRole Role) (*AggroState, error) {
	if sourceRole == "" {
		return nil, errors.New("source role missing")
	}
	return &AggroState{sourceRole: sourceRole, targets: make(map[Role]struct{})}, nil
}

func (s *AggroState) SetAgentState(agentState uint8) {
	if s == nil {
		return
	}
	s.agentState = agentState
}

func (s *AggroState) IgnoreFirstAggro() {
	if s == nil {
		return
	}
	s.isFirstAggroConsumed = true
	s.pendingFirstTargetRole = ""
}

func (s *AggroState) Insert(command AggroInsertCommand) (AggroInsertResult, error) {
	if s == nil || s.targets == nil {
		return AggroInsertResult{}, errors.New("nil aggro state")
	}
	if command.SourceRole != s.sourceRole || command.TargetRole == "" ||
		command.TargetRole == command.SourceRole {
		return AggroInsertResult{}, fmt.Errorf("roles rejected: %s/%s", command.SourceRole, command.TargetRole)
	}
	if command.Flags == 0 {
		return AggroInsertResult{}, errors.New("aggro flags missing")
	}
	if !command.IsAuthoritative || !command.IsHostile || s.agentState == 2 {
		return AggroInsertResult{}, nil
	}
	if _, isFound := s.targets[command.TargetRole]; isFound {
		return AggroInsertResult{}, nil
	}
	isEmpty := len(s.targets) == 0
	s.targets[command.TargetRole] = struct{}{}
	result := AggroInsertResult{IsAccepted: true}
	if !isEmpty || s.isFirstAggroConsumed || command.Flags&(AggroFlagTrigger|AggroFlagSpawn) == 0 {
		return result, nil
	}
	s.isFirstAggroConsumed = true
	s.pendingFirstTargetRole = command.TargetRole
	result.IsFirstAggro = true
	result.StimulusMask = FirstAggroStimulusMask
	result.StimulusDuration = FirstAggroStimulusDuration
	return result, nil
}

func (s *AggroState) EvaluateFirstAggro() (FirstAggroCommand, bool) {
	if s == nil || s.pendingFirstTargetRole == "" {
		return FirstAggroCommand{}, false
	}
	command := FirstAggroCommand{
		ActorRole:  s.pendingFirstTargetRole,
		TargetRole: s.sourceRole,
	}
	s.pendingFirstTargetRole = ""
	return command, true
}

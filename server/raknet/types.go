package raknet

// PacketID identifies a Game application message.
type PacketID uint8

const (
	HelloPlayerRequest    PacketID = 0x7f
	HelloPlayer           PacketID = 0x80
	ReconnectPlayer       PacketID = 0x81
	Connected             PacketID = 0x82
	Goodbye               PacketID = 0x83
	PlayerJoined          PacketID = 0x84
	PartyMergeComplete    PacketID = 0x85
	PlayerDeparted        PacketID = 0x86
	VoteKickStarted       PacketID = 0x87
	PlayerStatusUpdate    PacketID = 0x88
	GameAborted           PacketID = 0x89
	GameStatePacket       PacketID = 0x8a
	DirectorState         PacketID = 0x8b
	ObjectCreate          PacketID = 0x8c
	ObjectUpdate          PacketID = 0x8d
	ObjectDelete          PacketID = 0x8e
	ObjectJump            PacketID = 0x8f
	ObjectTeleport        PacketID = 0x90
	ObjectPlayerMove      PacketID = 0x91
	ForcePhysicsUpdate    PacketID = 0x92
	PhysicsChanged        PacketID = 0x93
	LocomotionUpdate      PacketID = 0x94
	LocomotionUnreliable  PacketID = 0x95
	AttributeDataUpdate   PacketID = 0x96
	CombatantDataUpdate   PacketID = 0x97
	InteractableUpdate    PacketID = 0x98
	AgentBlackboardUpdate PacketID = 0x99
	LootDataUpdate        PacketID = 0x9a
	ServerEvent           PacketID = 0x9b
	ActionCommandMsgs     PacketID = 0x9c
	// The retail client message-name table confirms 0x9d-0xa0. The older C++
	// reference skips 0x9d and shifts these IDs, which is not wire-compatible.
	PlayerDamage          PacketID = 0x9d
	LootSpawned           PacketID = 0x9e
	LootAcquired          PacketID = 0x9f
	SystemMessage         PacketID = 0xa0
	LabsPlayerUpdate      PacketID = 0xa1
	ModifierCreated       PacketID = 0xa2
	ModifierUpdated       PacketID = 0xa3
	ModifierDeleted       PacketID = 0xa4
	SetAnimationState     PacketID = 0xa5
	SetObjectGFXState     PacketID = 0xa6
	PlayerCharacterDeploy PacketID = 0xa7
	ActionCommandResponse PacketID = 0xa8
	ChainVoteMsgs         PacketID = 0xa9
	ChainLevelResultsMsgs PacketID = 0xaa
	ChainCashOutMsgs      PacketID = 0xab
	ChainPlayerMsgs       PacketID = 0xac
	ChainGameMsgs         PacketID = 0xad
	ChainGameOverMsgs     PacketID = 0xae
	QuickGameMsgs         PacketID = 0xaf
	GamePrepareForStart   PacketID = 0xb0
	GameStart             PacketID = 0xb1
	CheatMessage          PacketID = 0xb2
	ArenaPlayerMsgs       PacketID = 0xb3
	ArenaLobbyMsgs        PacketID = 0xb4
	ArenaGameMsgs         PacketID = 0xb5
	ArenaResultsMsgs      PacketID = 0xb6
	ObjectivesInit        PacketID = 0xb7
	ObjectiveUpdated      PacketID = 0xb8
	ObjectivesComplete    PacketID = 0xb9
	CombatEvent           PacketID = 0xba
	JuggernautPlayerMsgs  PacketID = 0xbb
	JuggernautLobbyMsgs   PacketID = 0xbc
	JuggernautGameMsgs    PacketID = 0xbd
	JuggernautResultsMsgs PacketID = 0xbe
	ReloadLevel           PacketID = 0xbf
	GravityForceUpdate    PacketID = 0xc0
	CooldownUpdate        PacketID = 0xc1
	CrystalDragMessage    PacketID = 0xc2
	CrystalMessage        PacketID = 0xc3
	KillRacePlayerMsgs    PacketID = 0xc4
	KillRaceLobbyMsgs     PacketID = 0xc5
	KillRaceGameMsgs      PacketID = 0xc6
	KillRaceResultsMsgs   PacketID = 0xc7
	TutorialGameMsgs      PacketID = 0xc8
	CinematicMsgs         PacketID = 0xc9
	ObjectiveAdd          PacketID = 0xca
	LootDropMessage       PacketID = 0xcb
	DebugPing             PacketID = 0xcc
)

// GameState follows the client state machine in raknet/client.h.
type GameState uint32

const (
	GameBoot GameState = iota
	GameLogin
	GameSpaceship
	GameEditor
	GameLevelEditor
	GamePreDungeon
	GameDungeon
	GameObserver
	GameCinematic
	GameSpectator
	GameReplay
	GameChainVoting
	GameChainCashOut
	GameOver
	GameQuit
	GameArenaLobby
	GameArenaRoundResults
	GameJuggernautLobby
	GameJuggernautResults
	GameKillRaceLobby
	GameKillRaceResults
)

const GameStateInvalid GameState = 0xffffffff

func ValidStateChange(from, to GameState) bool {
	if from == GameStateInvalid || to == GameLogin && from != GameLogin || to == GameSpaceship && from != GameSpaceship {
		return true
	}
	switch from {
	case GameLogin, GameDungeon, GameObserver, GameCinematic, GameSpectator:
		return true
	case GameEditor, GameChainCashOut, GameOver, GameJuggernautResults, GameKillRaceResults:
		return to == GameSpaceship
	case GameLevelEditor, GameReplay, GameArenaLobby, GameJuggernautLobby, GameKillRaceLobby:
		return to == GameSpaceship || to == GamePreDungeon
	case GameBoot:
		return to == GameLogin || to == GameSpaceship
	case GameSpaceship:
		return to == GameEditor || to == GameLevelEditor || to == GamePreDungeon || to == GameReplay || to == GameChainVoting || to == GameArenaLobby
	case GamePreDungeon:
		return to == GameSpaceship || to == GameDungeon || to == GameSpectator || to == GameReplay
	case GameChainVoting:
		return to == GameSpaceship || to == GamePreDungeon || to == GameChainCashOut
	case GameArenaRoundResults:
		return to == GameSpaceship || to == GameArenaLobby
	default:
		return false
	}
}

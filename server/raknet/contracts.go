package raknet

// PacketDirection describes endpoints present in build 5.3.0.103. It does not
// imply that an absent endpoint would be safe to invent.
type PacketDirection uint8

const (
	PacketDirectionNone PacketDirection = iota
	PacketDirectionClientToServer
	PacketDirectionServerToClient
	PacketDirectionBoth
)

// PacketEndpoint describes whether build 103 installs an application endpoint.
type PacketEndpoint uint8

const (
	PacketEndpointActive PacketEndpoint = iota
	PacketEndpointUnattached
	PacketEndpointUnconsumed
)

// PacketContract is the Go-level stub for one entry in build 103's complete
// 78-entry application vocabulary. Active body variants still require their
// own typed codec before they may be emitted or acted upon.
type PacketContract struct {
	ID        PacketID
	Name      string
	Direction PacketDirection
	Endpoint  PacketEndpoint
}

var applicationPacketContract = [...]PacketContract{
	{HelloPlayerRequest, "HelloPlayerRequest", PacketDirectionClientToServer, PacketEndpointActive},
	{HelloPlayer, "HelloPlayer", PacketDirectionServerToClient, PacketEndpointActive},
	{ReconnectPlayer, "ReconnectPlayer", PacketDirectionServerToClient, PacketEndpointActive},
	{Connected, "Connected", PacketDirectionServerToClient, PacketEndpointActive},
	{Goodbye, "Goodbye", PacketDirectionNone, PacketEndpointUnattached},
	{PlayerJoined, "PlayerJoined", PacketDirectionServerToClient, PacketEndpointActive},
	{PartyMergeComplete, "PartyMergeComplete", PacketDirectionServerToClient, PacketEndpointActive},
	{PlayerDeparted, "PlayerDeparted", PacketDirectionServerToClient, PacketEndpointActive},
	{VoteKickStarted, "VoteKickStarted", PacketDirectionServerToClient, PacketEndpointActive},
	{PlayerStatusUpdate, "PlayerStatusUpdate", PacketDirectionClientToServer, PacketEndpointActive},
	{GameAborted, "GameAborted", PacketDirectionNone, PacketEndpointUnattached},
	{GameStatePacket, "GameStatePacket", PacketDirectionServerToClient, PacketEndpointActive},
	{DirectorState, "DirectorState", PacketDirectionServerToClient, PacketEndpointActive},
	{ObjectCreate, "ObjectCreate", PacketDirectionServerToClient, PacketEndpointActive},
	{ObjectUpdate, "ObjectUpdate", PacketDirectionServerToClient, PacketEndpointActive},
	{ObjectDelete, "ObjectDelete", PacketDirectionServerToClient, PacketEndpointActive},
	{ObjectJump, "ObjectJump", PacketDirectionServerToClient, PacketEndpointActive},
	{ObjectTeleport, "ObjectTeleport", PacketDirectionServerToClient, PacketEndpointActive},
	{ObjectPlayerMove, "ObjectPlayerMove", PacketDirectionServerToClient, PacketEndpointActive},
	{ForcePhysicsUpdate, "ForcePhysicsUpdate", PacketDirectionServerToClient, PacketEndpointActive},
	{PhysicsChanged, "PhysicsChanged", PacketDirectionServerToClient, PacketEndpointActive},
	{LocomotionUpdate, "LocomotionUpdate", PacketDirectionServerToClient, PacketEndpointActive},
	{LocomotionUnreliable, "LocomotionUnreliable", PacketDirectionServerToClient, PacketEndpointActive},
	{AttributeDataUpdate, "AttributeDataUpdate", PacketDirectionServerToClient, PacketEndpointActive},
	{CombatantDataUpdate, "CombatantDataUpdate", PacketDirectionServerToClient, PacketEndpointActive},
	{InteractableUpdate, "InteractableUpdate", PacketDirectionServerToClient, PacketEndpointActive},
	{AgentBlackboardUpdate, "AgentBlackboardUpdate", PacketDirectionServerToClient, PacketEndpointActive},
	{LootDataUpdate, "LootDataUpdate", PacketDirectionServerToClient, PacketEndpointActive},
	{ServerEvent, "ServerEvent", PacketDirectionServerToClient, PacketEndpointActive},
	{ActionCommandMsgs, "ActionCommandMsgs", PacketDirectionClientToServer, PacketEndpointActive},
	{PlayerDamage, "PlayerDamage", PacketDirectionNone, PacketEndpointUnattached},
	{LootSpawned, "LootSpawned", PacketDirectionNone, PacketEndpointUnattached},
	{LootAcquired, "LootAcquired", PacketDirectionNone, PacketEndpointUnattached},
	{SystemMessage, "SystemMessage", PacketDirectionNone, PacketEndpointUnattached},
	{LabsPlayerUpdate, "LabsPlayerUpdate", PacketDirectionServerToClient, PacketEndpointActive},
	{ModifierCreated, "ModifierCreated", PacketDirectionServerToClient, PacketEndpointActive},
	{ModifierUpdated, "ModifierUpdated", PacketDirectionServerToClient, PacketEndpointActive},
	{ModifierDeleted, "ModifierDeleted", PacketDirectionServerToClient, PacketEndpointActive},
	{SetAnimationState, "SetAnimationState", PacketDirectionServerToClient, PacketEndpointActive},
	{SetObjectGFXState, "SetObjectGFXState", PacketDirectionServerToClient, PacketEndpointActive},
	{PlayerCharacterDeploy, "PlayerCharacterDeploy", PacketDirectionServerToClient, PacketEndpointActive},
	{ActionCommandResponse, "ActionCommandResponse", PacketDirectionServerToClient, PacketEndpointActive},
	{ChainVoteMsgs, "ChainVoteMsgs", PacketDirectionServerToClient, PacketEndpointActive},
	{ChainLevelResultsMsgs, "ChainLevelResultsMsgs", PacketDirectionNone, PacketEndpointUnconsumed},
	{ChainCashOutMsgs, "ChainCashOutMsgs", PacketDirectionServerToClient, PacketEndpointActive},
	{ChainPlayerMsgs, "ChainPlayerMsgs", PacketDirectionClientToServer, PacketEndpointActive},
	{ChainGameMsgs, "ChainGameMsgs", PacketDirectionServerToClient, PacketEndpointActive},
	{ChainGameOverMsgs, "ChainGameOverMsgs", PacketDirectionServerToClient, PacketEndpointActive},
	{QuickGameMsgs, "QuickGameMsgs", PacketDirectionServerToClient, PacketEndpointActive},
	{GamePrepareForStart, "GamePrepareForStart", PacketDirectionServerToClient, PacketEndpointActive},
	{GameStart, "GameStart", PacketDirectionServerToClient, PacketEndpointActive},
	{CheatMessage, "CheatMessage", PacketDirectionNone, PacketEndpointUnconsumed},
	{ArenaPlayerMsgs, "ArenaPlayerMsgs", PacketDirectionClientToServer, PacketEndpointActive},
	{ArenaLobbyMsgs, "ArenaLobbyMsgs", PacketDirectionServerToClient, PacketEndpointActive},
	{ArenaGameMsgs, "ArenaGameMsgs", PacketDirectionServerToClient, PacketEndpointActive},
	{ArenaResultsMsgs, "ArenaResultsMsgs", PacketDirectionServerToClient, PacketEndpointActive},
	{ObjectivesInit, "ObjectivesInit", PacketDirectionServerToClient, PacketEndpointActive},
	{ObjectiveUpdated, "ObjectiveUpdated", PacketDirectionServerToClient, PacketEndpointActive},
	{ObjectivesComplete, "ObjectivesComplete", PacketDirectionServerToClient, PacketEndpointActive},
	{CombatEvent, "CombatEvent", PacketDirectionServerToClient, PacketEndpointActive},
	{JuggernautPlayerMsgs, "JuggernautPlayerMsgs", PacketDirectionClientToServer, PacketEndpointActive},
	{JuggernautLobbyMsgs, "JuggernautLobbyMsgs", PacketDirectionNone, PacketEndpointUnconsumed},
	{JuggernautGameMsgs, "JuggernautGameMsgs", PacketDirectionServerToClient, PacketEndpointActive},
	{JuggernautResultsMsgs, "JuggernautResultsMsgs", PacketDirectionServerToClient, PacketEndpointActive},
	{ReloadLevel, "ReloadLevel", PacketDirectionServerToClient, PacketEndpointActive},
	{GravityForceUpdate, "GravityForceUpdate", PacketDirectionServerToClient, PacketEndpointActive},
	{CooldownUpdate, "CooldownUpdate", PacketDirectionServerToClient, PacketEndpointActive},
	{CrystalDragMessage, "CrystalDragMessage", PacketDirectionClientToServer, PacketEndpointActive},
	{CrystalMessage, "CrystalMessage", PacketDirectionServerToClient, PacketEndpointActive},
	{KillRacePlayerMsgs, "KillRacePlayerMsgs", PacketDirectionClientToServer, PacketEndpointActive},
	{KillRaceLobbyMsgs, "KillRaceLobbyMsgs", PacketDirectionNone, PacketEndpointUnconsumed},
	{KillRaceGameMsgs, "KillRaceGameMsgs", PacketDirectionServerToClient, PacketEndpointActive},
	{KillRaceResultsMsgs, "KillRaceResultsMsgs", PacketDirectionServerToClient, PacketEndpointActive},
	{TutorialGameMsgs, "TutorialGameMsgs", PacketDirectionServerToClient, PacketEndpointActive},
	{CinematicMsgs, "CinematicMsgs", PacketDirectionServerToClient, PacketEndpointActive},
	{ObjectiveAdd, "ObjectiveAdd", PacketDirectionServerToClient, PacketEndpointActive},
	{LootDropMessage, "LootDropMessage", PacketDirectionClientToServer, PacketEndpointActive},
	{DebugPing, "DebugPing", PacketDirectionBoth, PacketEndpointActive},
}

func ApplicationPacketContracts() []PacketContract {
	contract := make([]PacketContract, len(applicationPacketContract))
	copy(contract, applicationPacketContract[:])
	return contract
}

func LookupApplicationPacketContract(id PacketID) (PacketContract, bool) {
	index := int(id) - int(HelloPlayerRequest)
	if index < 0 || index >= len(applicationPacketContract) {
		return PacketContract{}, false
	}
	return applicationPacketContract[index], true
}

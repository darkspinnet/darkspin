package game

// These inputs describe authored trigger definitions. Runtime overlap, timer,
// callback cache and activation state belong to the campaign operation.
type CampaignTriggerVolumeDefinition struct {
	OnEnter                 *string
	OnExit                  *string
	OnStay                  *string
	OnEnterHash             uint32
	OnExitHash              uint32
	OnStayHash              uint32
	OnEnterEvent            *string
	OnExitEvent             *string
	IsUsingObjectDimensions bool
	IsKinematic             bool
	Shape                   uint32
	Offset                  Vec3
	TimeToActivate          float32
	IsPersistentTimer       bool
	IsTriggerOnceOnly       bool
	IsTriggerIfNotBeaten    bool
	TriggerActivationType   uint32
	LuaCallbackOnEnter      *string
	LuaCallbackOnExit       *string
	LuaCallbackOnStay       *string
	BoxDimensions           Vec3
	SphereRadius            float32
	CapsuleHeight           float32
	CapsuleRadius           float32
	IsServerOnly            bool
}

type CampaignSpawnTriggerDefinition struct {
	TriggerVolume     *CampaignTriggerVolumeDefinition
	DeathEvent        *string
	DeathEventHash    uint32
	ChallengeOverride int32
	WaveOverride      int32
}

type CampaignPickupTuning struct {
	ResurrectionHealthFraction float32
}

type CampaignEventListenerDefinition struct {
	Entries []CampaignEventListenerEntry
}

type CampaignEventListenerEntry struct {
	Ordinal            int
	EventHash          uint32
	EventName          *string
	NativeCallbackHash uint32
	NativeCallbackName *string
	LuaCallbackName    *string
}

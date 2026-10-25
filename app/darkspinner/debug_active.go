//go:build scenario

package main

// Native admission is separate from the independently observed server impact
// and release. Positive native result codes can mean approach or queuing.
type controlActiveCast struct {
	ActionIndex                uint32             `json:"action_index"`
	IsQueryProcessed           bool               `json:"is_query_processed"`
	IsNativeRequestAttempted   bool               `json:"is_native_request_attempted"`
	NativeResultCode           *int32             `json:"native_result_code"`
	AbilityID                  *uint32            `json:"ability_id"`
	TargetHandle               *uint32            `json:"target_handle"`
	IsNativeGroundQuerySuccess bool               `json:"is_native_ground_query_success"`
	RayPoint                   *controlWorldPoint `json:"ray_point"`
	ChosenGroundPoint          *controlWorldPoint `json:"chosen_ground_point"`
}

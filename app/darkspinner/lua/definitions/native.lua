---@meta

---@class NativeGameplaySnapshot
---@field is_available boolean
---@field is_world_active boolean Recent actual gameplay poll in the same native scene and frame owner; not controllability.
---@field controlled_handle integer|nil Opaque native generation-bearing handle, not a server object ID.
---@field picked_handle integer|nil Native retained target, not a fresh raycast or server object ID.
---@field position WorldPosition|nil
---@field frame_sequence integer
---@field owner_thread_id integer

---@class WorldProjection
---@field is_available boolean
---@field is_on_screen boolean Native camera front/frustum and owned client bounds checks.
---@field x number|nil
---@field y number|nil
---@field client_width integer
---@field client_height integer

---Native input processing only; observe world changes independently.
---@class WorldClickReceipt
---@field is_native_ground_query_success boolean Native ray and movement-point query returned success; not movement acceptance.
---@field ray_point WorldPosition|nil Terrain ray point sampled for the consumed press; may use a plane fallback.
---@field chosen_ground_point WorldPosition|nil Native adjusted movement candidate, distinct from the requested projected point; not arrival proof.
---@field is_projected boolean
---@field is_press_poll_observed boolean
---@field is_release_poll_observed boolean
---@field retained_handle integer|nil Opaque native handle, not a server object ID.

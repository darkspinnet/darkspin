---@meta

---@class NativeHeroSelection
---@field requested_slot integer Zero-based squad slot.
---@field previous_controlled_handle integer|nil Opaque native handle, not a server object ID.
---@field observed_controlled_handle integer|nil Immediate native observation; switching may still be pending.
---@field previous_slot integer|nil Native player slot scalar before dispatch.
---@field observed_slot integer|nil Native player slot scalar after processing.
---@field eligibility_code integer|nil Only native code zero permits dispatch.
---@field is_query_processed boolean

---@class HeroSelectionResult
---@field requested_slot integer
---@field initial_controlled_handle integer|nil
---@field controlled_handle integer|nil Fresh native controlled handle; actual switching requires changed handle plus requested server index.
---@field is_already_selected boolean Server already reports the requested slot; no native dispatch.
---@field action WindowCommandResult|nil Partial native processing/dispatch receipt, never switch success by itself.
---@field native WindowCommandResult|nil Latest independent native gameplay frame observation.
---@field world WorldSnapshot Latest server observation, retained on failure.

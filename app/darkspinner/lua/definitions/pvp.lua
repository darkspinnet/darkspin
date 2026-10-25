---@meta

---@class ClientPVP
---@field Snapshot fun(self:ClientPVP):ArenaLobbyResult|nil,ScenarioError|nil Samples the native arena lobby once without selecting or accepting anything.
---@field Accept fun(self:ClientPVP,timeout_ms?:integer):ArenaAcceptResult|nil,ScenarioError|nil Waits for the native ready guard, invokes OnReadyClicked once for the selected PvP squad and observes server acceptance. Uses client timeout; never replay after unknown outcomes. Accept all participants concurrently.
---@field Wait fun(self:ClientPVP,timeout_ms?:integer):WorldSnapshot|nil,ScenarioError|nil Waits for this player's accepted squad, actual server arena combat after the opening countdown, and native world readiness. Requires an already formed game; pins owned process/login/game.

---@class ArenaLobbyResult
---@field arena_lobby ArenaLobbySnapshot|nil Native observations; successful dispatch is not combat readiness.

---@class ArenaLobbySnapshot
---@field is_available boolean
---@field is_loaded boolean
---@field is_ready boolean
---@field squad_index? integer Native selected PvP squad index.
---@field is_accept_disabled? boolean
---@field is_accept_visible? boolean
---@field is_squad_valid? boolean

---@class ArenaAcceptResult
---@field is_already_accepted boolean True when server acceptance was observed without dispatching another action.
---@field action? WindowCommandResult Native action receipt if this call dispatched the Ready listener.
---@field profile ProfileSnapshot Matching server PvP deck acceptance observation.

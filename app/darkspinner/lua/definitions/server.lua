---@meta

---@class ServerStartRequest
---@field is_modded? boolean Enable the production Mod server behavior for this isolated run; must match each launched client.
---@field game_directory? string Installed game/runtime directory.
---@field content_path? string Coherent authoritative content.db path.
---@field fang_path? string Matching scenario Fang DLL.

---@class ServerIdentity
---@field is_modded boolean
---@field address string Owned loopback endpoint.
---@field is_ready boolean Server readiness only; proves no client login or world readiness.

---Identity fields are the initial snapshot. Creating a profile supplies setup
---without logging in, forming a party, or entering a mission. Creating an existing
---alias or resetting it first closes and releases its owned clients, then creates
---a fresh account. The alias changes only on success. Existing saves are unchanged.
---@class ServerHandle: ServerIdentity
---@field CreateProfile fun(self:ServerHandle,req:ProfileRequest):ProfileHandle|nil,ScenarioError|nil
---@field ResetProfile fun(self:ServerHandle,req:ProfileRequest):ProfileHandle|nil,ScenarioError|nil
---@field StartPVP fun(self:ServerHandle,req:PVPRequest):PVPMatch|nil,ScenarioError|nil Requires participants in Navigation with established owned parties; 1v1 opponents share one two-player party. Forms explicit arena teams through production server setup notifications. Does not issue native matchmaking input or prove loading. PVP:Accept() all participants concurrently, then PVP:Wait(). Never replay after a partial/unknown failure.

---@class PVPRequest
---@field teams ManagedClient[][] Exactly two equal teams of one or two distinct authenticated clients outside games and parties. Team membership follows list order. Requires an owned unlocked PvP squad; create profiles with is_pvp_enabled=true.

---@class PVPParticipant
---@field client_id string
---@field instance_id string
---@field pid integer
---@field user_id integer
---@field team integer 1 or 2.

---@class PVPMatch
---@field game_id integer
---@field team_size integer
---@field participants PVPParticipant[]
---@field is_mutation_attempted boolean
---@field is_mutation_accepted boolean
---@field is_projection_complete boolean Server notification delivery only.
---@field notification_count integer
---@field delivered_notification_count integer
---@field is_native_acceptance_observed false Observe loading and native world readiness separately.

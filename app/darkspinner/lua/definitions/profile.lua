---@meta

---@class SeedEquippedPart
---@field squad_category 'pve'|'pvp'
---@field hero_index integer Zero-based slot in the requested squad.
---@field rigblock_asset_id integer Installed LootRigblock ID.
---@field level integer Item level, 1 through 100.
---@field rarity integer Normal=0, uncommon=1, rare=2, epic=3.

---@class SeedProfileRequest
---@field equipped_parts? SeedEquippedPart[] Real owned equipment on this fresh profile; does not create an edited creature mesh or simulate an editor save.
---@field is_pvp_enabled? boolean Seeds an unlocked PvP squad with independent owned copies of the supplied heroes; preserves the campaign squad.
---@field name string Unique profile alias in the isolated session.
---@field hero_nouns integer[] Exactly three distinct installed squad template IDs, validated by the server; inventory also includes any missing ordinary-login starter heroes.
---@field chain_progression? integer Supplied completed-campaign progression; defaults to 0.

---@class CloneProfileRequest
---@field equipped_parts? SeedEquippedPart[] Explicit fresh equipment; source inventory is never copied.
---@field is_pvp_enabled? boolean Seeds an unlocked PvP squad using copies of the resolved source hero templates.
---@field name string Unique profile alias in the isolated session.
---@field source_login_name string Existing account in this owned isolated server; main-save import is unavailable.
---@field is_source_clone_allowed true Explicit permission to derive selected squad templates and progression; missing ordinary-login starter heroes are supplied in the new inventory without changing that squad.

---@alias ProfileRequest SeedProfileRequest|CloneProfileRequest
---@alias ReportedLocation ReportedScene

---@class ProfileIdentity
---@field is_pvp_enabled boolean
---@field pvp_squad_id integer Zero when PvP setup was not requested.
---@field name string
---@field login_name string
---@field user_id integer
---@field squad_id integer
---@field hero_nouns integer[] Effective installed template IDs.

---Identity fields are the initial snapshot. Methods address the session alias.
---Launch proves process/bridge launch only. Login waits prove accepted server
---authentication only. Location waits require authenticated matching presence
---with revision strictly greater than afterRevision (default 0). Pass a captured
---revision to require a later report; this is not native scene readiness.
---@class ProfileHandle: ProfileIdentity
---@field Logout fun(self:ProfileHandle):ScenarioError|nil Releases this owned account's authenticated server slot after all its clients stop; preserves its saved profile.
---@field Launch fun(self:ProfileHandle,req:ClientOptions):ManagedClient|nil,ScenarioError|nil
---@field Snapshot fun(self:ProfileHandle):ProfileSnapshot|nil,ScenarioError|nil
---@field WaitForLogin fun(self:ProfileHandle,timeoutMs:integer):LoginObservation|nil,ScenarioError|nil
---@field WaitForReportedLocation fun(self:ProfileHandle,location:ReportedLocation,timeoutMs:integer,afterRevision?:integer):ProfileSnapshot|nil,ScenarioError|nil

---@class LoginObservation
---@field name string Isolated profile alias.
---@field is_authenticated boolean Accepted live Blaze authentication.
---@field session_id string Opaque accepted session identity.

---@class ReportedPresence
---@field status 'available'|'unavailable'|'stale'
---@field source 'last_client_reported_presence'
---@field location string Last recognized client report; not a native scene read.
---@field revision integer Changed-envelope revision, not a frame or timestamp.

---Independent read-only server observations; reading does not activate a profile.
---@class ProfileSnapshot
---@field name string Owned profile alias.
---@field user_id integer
---@field captured_at string Collection time, not the client's publication time.
---@field is_authenticated boolean Accepted live Blaze login, sampled separately from other fields.
---@field session_id string
---@field is_user_active boolean
---@field game_id integer Active-user membership; zero proves no native UI readiness.
---@field playgroup_id integer Current server party membership.
---@field presence ReportedPresence Retained presence becomes stale after authentication ends.
---@field is_gameplay_active boolean Server binding exists; proves no client world readiness.
---@field is_gameplay_available boolean Dedicated lifecycle observation is available; distinct from an active binding.
---@field gameplay_unavailable_reason? string Missing/failed lifecycle observation; independent login and presence evidence remains usable.
---@field gameplay_game_id integer Actual gameplay binding identity, sampled separately from active-user game_id.
---@field session_stage string Server lifecycle stage, when bound.
---@field is_dungeon_setup_final boolean Server setup commitment, not rendering readiness.
---@field is_arena boolean
---@field is_arena_squad_accepted boolean Server accepted this player's PvP deck.
---@field is_arena_combat_active boolean Actual server arena countdown completed and combat is active.
---@field mission {label:string,asset:string,difficulty:integer} Empty before gameplay binding.

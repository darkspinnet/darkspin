---@meta

---@alias LaunchMode 'normal'|'detached'|'remote'

---@class ClientOptions
---@field is_modded? boolean Enable native Mod/creature editor hooks for a fresh scenario profile. Requires StartServer({is_modded=true}); independent of launch_mode.
---@field spore_path? string Optional installed Spore root for mod resource mounting; requires is_modded. Otherwise uses launcher configuration/detection, or stock editor assets if no source is configured.
---@field client_id string Managed process identity, independent of profile and party role.
---@field launch_mode? LaunchMode Normal or detached; remote is reserved and returns unavailable.
---@field is_detached? boolean Compatibility selector for the existing detached helper.
---@field timeout_ms? integer Default blocking-operation timeout, 60000 milliseconds when omitted; 1-120000.
---@field is_intro_skipped? boolean Skip introductory movies; scenario launches default true, normal launcher defaults false.
---@field is_cinematic_skipped? boolean Skip in-game cinematics; scenario launches default true, normal launcher defaults false.

---Flat compatibility request; profile:Launch supplies profile_name itself.
---@class ClientLaunchRequest: ClientOptions
---@field profile_name string Owned profile alias, distinct for each participant.

---@class ClientObservationSnapshot
---@field name string Managed client_id, independent of profile or party role.
---@field instance_id string Exact owned process instance.
---@field pid integer
---@field connection_generation integer
---@field executable_sha256 string
---@field architecture string
---@field build_id string
---@field sequence integer
---@field observed_at string
---@field bridge table Availability-qualified independent bridge heartbeat.
---@field frame_sequence table Availability-qualified game frame observation.
---@field scene_asset_id table Availability-qualified installed scene identity.
---@field scene_name table Availability-qualified scene name.
---@field controlled_hero_id table Unavailable until a supported observer exists.
---@field selected_hero_slot table Unavailable until a supported observer exists.

---@class ClientSnapshot
---@field is_modded boolean
---@field user_id integer Immutable launch account identity.
---@field client_id string
---@field profile_name string Session alias, independent of process identity and launch mode.
---@field instance_id string
---@field pid integer
---@field launch_mode LaunchMode
---@field is_detached boolean
---@field is_running boolean
---@field status string
---@field observation ClientObservationSnapshot Missing state is explicitly unavailable.

---An owned process handle. Its actual fields are ID and InitialSnapshot; snapshot
---fields are accessed through InitialSnapshot or Snapshot(), without inheritance.
---Login/profile methods resolve the associated profile alias. Blocking methods
---inherit the current client timeout (60000 milliseconds initially); optional
---per-call timeouts override that call only. Scene/UI scopes inherit changes
---dynamically, including scopes obtained before SetTimeout. afterRevision is 0.
---@class ManagedClient
---@field PVP ClientPVP Native arena lobby observations, guarded Accept and verified combat readiness.
---@field ID string Immutable client_id.
---@field InitialSnapshot ClientSnapshot Launch observation, not login or world readiness.
---@field Scene ClientScene Wait combines reported presence with native current-room and UI-eligibility probes; does not prove rendering or gameplay readiness.
---@field UI ClientUI Owned-window operations, independent of desktop focus.
---@field Diagnostics ClientDiagnostics Saved local observations, screenshot and bounded packet-trace tails.
---@field Debug ClientDebug Bounded debugger suspension of this owned client only; the server simulation continues.
---@field Ability ClientAbility Narrow supported active-ability observation and ordinary native cast; unsupported heroes fail before input.
---@field World ClientWorld Read-only server world snapshots; no native rendering or control proof.
---@field Cinematic CinematicScope Independent introductory-movie and in-game cinematic switches.
---@field Mission ClientMission Guarded native selection/actions and server deployment/dungeon observations; not native world readiness.
---@field Party ClientParty Owned-session server party fixture and observations; does not prove native UI acceptance.
---@field Snapshot fun(self:ManagedClient):ClientSnapshot|nil,ScenarioError|nil
---@field Time fun(self:ManagedClient):number Monotonic elapsed milliseconds since client launch began, including launch duration; no error result.
---@field Login fun(self:ManagedClient,timeout_ms?:integer):LoginObservation|nil,ScenarioError|nil Waits for accepted live server authentication; proves no native UI/world readiness.
---@field SetTimeout fun(self:ManagedClient,ms:integer):ScenarioError|nil Changes this client's default timeout; use scenario.Check.
---@field Ready fun(self:ManagedClient,timeout_ms?:integer):ClientReadiness|nil,ScenarioError|nil Fresh owner-thread responsiveness with profile-state evidence; does not establish a rendered scene.
---@field Close fun(self:ManagedClient):ScenarioError|nil Graceful close, waiting for completion or error.
---@field Terminate fun(self:ManagedClient):ScenarioError|nil Explicitly stops only this owned client and its detached helper, waiting for exit. Does not accept in-game quit/save prompts.
local ManagedClient = {}

---Compatibility alias; prefer client:Login(timeout_ms).
---@deprecated
---@param timeout_ms? integer Overrides the current client default for this call only.
---@return LoginObservation|nil result
---@return ScenarioError|nil err
function ManagedClient:WaitForLogin(timeout_ms) end

---Compatibility alias; prefer client.Scene:Snapshot().
---@deprecated
---@return ProfileSnapshot|nil result
---@return ScenarioError|nil err
function ManagedClient:ProfileSnapshot() end

---Compatibility alias; prefer client.Scene:Wait(sceneName,timeoutMs,afterRevision).
---@deprecated
---@param location ReportedScene
---@param timeoutMs integer From 1 to 120000 milliseconds.
---@param afterRevision? integer Nonnegative changed-envelope revision, defaults to 0.
---@return ProfileSnapshot|nil result Last partial observation is retained on timeout.
---@return ScenarioError|nil err
function ManagedClient:WaitForReportedLocation(location, timeoutMs, afterRevision) end

---Compatibility alias; prefer client.UI:InspectWindow().
---@deprecated
---@return WindowReceipt|nil result
---@return ScenarioError|nil err
function ManagedClient:Window() end

---Compatibility alias; prefer client.UI:Click(x,y).
---@deprecated
---@param x integer
---@param y integer
---@return WindowReceipt|nil result
---@return ScenarioError|nil err
function ManagedClient:Click(x, y) end

---Compatibility alias; prefer client.UI:Screenshot(label).
---@deprecated
---@param label string
---@return ScreenshotArtifact|nil result Partial metadata may accompany an error.
---@return ScenarioError|nil err
function ManagedClient:Screenshot(label) end

---Profile-state evidence and a probe of this owned client's window owner thread.
---On error the wrapper may retain only the observations completed so far.
---@class ClientReadiness
---@field client_id string
---@field instance_id string
---@field pid integer
---@field state ProfileSnapshot|nil Latest independently sampled profile-state evidence.
---@field readiness WindowCommandResult|nil Owner-thread responsiveness receipt when obtained.

---@meta

---@alias ScreenName 'none'|'profile'|'inventory'|'friends'|'leaderboards'|'manual'|'galaxy'

---@class ScreenNames
---@field None 'none'
---@field Profile 'profile'
---@field Inventory 'inventory'
---@field Friends 'friends'
---@field Leaderboards 'leaderboards'
---@field Manual 'manual'
---@field Galaxy 'galaxy'
screen = {}

---Owned native overlays, not ship rooms or server-reported location. Browser
---selection is independent of network response, DOM, or rendering readiness.
---@class ClientScreen
---@field Snapshot fun(self:ClientScreen):UIScreenResult|nil,ScenarioError|nil Queries the native screen on the owned window thread. Availability is explicit.
---@field Set fun(self:ClientScreen,target:ScreenName,timeout_ms?:integer):UIScreenResult|nil,ScenarioError|nil Queries first; if already selected returns without dispatch. Otherwise invokes the ordinary native action once then observes matching screen. Close Inventory before selecting another overlay. Never replays an unknown action.
---@field Wait fun(self:ClientScreen,target:ScreenName,timeout_ms?:integer):UIScreenResult|nil,ScenarioError|nil Pure native observation until matching selection under the client timeout. Does not wait for browser content rendering.
---@field Close fun(self:ClientScreen,timeout_ms?:integer):UIScreenResult|nil,ScenarioError|nil Equivalent to Set(screen.None); uses the actual native close gate and waits for no overlay. No ship scene change.

---@class NativeScreenState
---@field owner_generation integer Native UI lifecycle identity; zero unavailable. Set/Wait pin the first available owner and thread and fail on recreation. Dispatch rechecks the expected generation before invoking.
---@field is_available boolean Check before interpreting selection.
---@field screen string Supported names or an explicit unrecognized observation; never inferred from presence.
---@field is_inventory_open boolean Actual inventory visibility query.
---@field is_browser_open boolean Actual browser overlay state.
---@field native_screen_id integer|nil Native browser selection identity when observed; does not identify a ship room.
---@field selection_source? string Proven selection producer: dom for observed browser frame/tab state, native for native flags. The original cached native_screen_id may remain -1 when DOM evidence identifies Profile.
---@field is_content_ready boolean|nil Nil means unavailable. A selected/open overlay does not establish browser content or DOM readiness.
---@field is_invocation_attempted boolean Actual native action invocation was attempted; false for a pure query.
---@field is_invocation_returned boolean Actual void native invocation returned. This does not claim acceptance, selection, or browser content readiness.

---@class UIScreenResult
---@field client_id string
---@field instance_id string
---@field pid integer
---@field action WindowCommandResult|nil One ordinary void native invocation receipt, retained with errors. is_invocation_accepted is nil; nested screen_state attempted/returned records local invocation only. Subsequent observation proves selection, never content readiness.
---@field observation WindowCommandResult|nil Last pure native observation receipt, retained with errors.
---@field screen_state NativeScreenState|nil Last queried native overlay state.
---@field is_already_selected boolean Set returned after observation without dispatching a duplicate toggle.

---@meta

---Owned-window operations tied to this exact managed client. InspectWindow() makes a
---fresh Fang round trip. Click() processes a finite paired click at client
---coordinates without moving the desktop cursor or activating a window; inspect
---state after an unknown timeout/disconnect outcome before issuing another click.
---Screenshot() saves a bounded image and metadata locally. None proves gameplay
---or menu-transition success, rendering readiness, or a usable controlled hero.
---@class ClientUI
---@field OpenCreatureEditor fun(self:ClientUI,hero_noun:integer,timeout_ms?:integer):CreatureEditorOpenResult|nil,ScenarioError|nil Uses Mod's existing Creature Edit action for the unique owned Arsenal creature matching hero_noun, waits for Editor and verifies Mod ownership. Scene:Set(Editor) remains the ordinary equipment editor.
---@field Hierarchy fun(self:ClientUI):EditorUIHierarchy|nil,ScenarioError|nil Saves and returns the active editor's bounded native SPUI hierarchy. Unavailable outside Editor. Does not enumerate Flash/HTML controls or establish rendering readiness.
---@field Screen ClientScreen Native overlays, independent of ship Scene and reported presence.
---@field InspectWindow fun(self:ClientUI):WindowCommandResult|nil,ScenarioError|nil
---@field Click fun(self:ClientUI,x:integer,y:integer,button?:'left'|'right'):WindowCommandResult|nil,ScenarioError|nil Defaults to left. Paired owned-window input; processing does not establish the resulting UI action.
---@field Press fun(self:ClientUI,key:'Escape'|'Enter'):WindowCommandResult|nil,ScenarioError|nil Sends a finite paired key event through the owned window; proves processing only.
---@field Type fun(self:ClientUI,text:string):WindowCommandResult|nil,ScenarioError|nil Sends 1 through 256 printable ASCII characters to the owned window's current text control. No submit, clipboard or desktop focus change. Inspect processed_character_count on partial failure; never replay an unknown outcome.
---@field Screenshot fun(self:ClientUI,label:string):ScreenshotArtifact|nil,ScenarioError|nil Partial artifact metadata may accompany an error.
local ClientUI = {}

---@class EditorUIHierarchy
---@field client_id string
---@field instance_id string
---@field pid integer
---@field path string Saved raw native JSON artifact, under this client's log directory.
---@field sha256 string
---@field source 'native-editor-spui'
---@field observed_at string
---@field owner_generation integer Changes when the editor is configured or exits.
---@field is_complete boolean False when the 4096-node or 64-depth bound truncates traversal; never treat partial coverage as absence.
---@field is_body_mode boolean Observed Mod Body selection.
---@field is_mod_editor boolean Explicit Mod creature-editor lifecycle owns this editor, rather than ordinary equipment editing.
---@field editor_mode integer Ordinary native editor mode; Body and Equip both use native Build mode.
---@field nodes EditorUINode[] Breadth-first order. Nodes are observations, not retained native handles.

---@class EditorUINode
---@field id integer Native SPUI element ID; may be zero or non-unique.
---@field command integer Native command ID; informational, not an arbitrary command dispatcher.
---@field flags integer Raw IWindow flags.
---@field parent_index integer One-based index into nodes; zero only for the root.
---@field depth integer
---@field bounds number[] Client-coordinate left, top, right, bottom; may extend beyond the viewport or be clipped by a parent.
---@field is_visible boolean Visibility including ancestors; does not prove absence of occlusion.
---@field is_enabled boolean Enabled status including ancestors.

---@class CreatureEditorOpenResult
---@field action WindowCommandResult
---@field observation SceneObservation
---@field hierarchy EditorUIHierarchy

---Compatibility alias; prefer client.UI:InspectWindow().
---@deprecated
---@return WindowCommandResult|nil result
---@return ScenarioError|nil err
function ClientUI:Window() end

---Saved local owned-window capture. Labels contain 1-48 lowercase letters,
---digits, underscores or hyphens and start with a letter or digit. Must requires
---capture success; inspect partial metadata explicitly when an error is returned.
---@class ScreenshotArtifact
---@field client_id string
---@field instance_id string
---@field pid integer
---@field source string Capture adapter provenance.
---@field is_available boolean Actual capture availability.
---@field is_complete boolean Image and metadata finalized; proves no gameplay readiness.
---@field reason string Empty on success.
---@field started_at string RFC3339 UTC.
---@field finished_at string RFC3339 UTC.
---@field image_path string Saved PNG path, empty when unavailable.
---@field image_sha256 string Empty when unavailable.
---@field metadata_path string Includes unavailable diagnostics.
---@field metadata_sha256 string
---@field width integer Zero when unavailable.
---@field height integer Zero when unavailable.

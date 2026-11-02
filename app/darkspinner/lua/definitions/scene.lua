---@meta

---@alias ReportedScene 'arsenal'|'navigation'|'editor'

---Supported ship-room labels, also reported in client presence.
---@type {Arsenal:'arsenal',Navigation:'navigation',Editor:'editor'}
scene = {
    Arsenal = 'arsenal',
    Navigation = 'navigation',
    Editor = 'editor',
}

---Scene scope tied to this client and its profile alias. Snapshot() reads independent
---server observations. Wait() requires matching authenticated reported presence
---with revision greater than afterRevision, then performs a pure native scene.ready
---probe on the owner thread and rechecks authentication, report and client identity.
---Ship-room probes require the expected native current room, loaded/enabled UI,
---eligible progression/mode and no active camera/movie. Editor instead observes its
---own configured lifecycle, enabled unblocked UI and model on the owner thread.
---Explicitly unavailable probe results
---may be retried within the same deadline; no transition/input is sent by Wait().
---The current client timeout is used by default;
---a per-call override does not change it. afterRevision defaults to 0 (an existing
---report may qualify). Pass a captured revision to require a later report.
---These eligibility observations do not prove final rendering or gameplay readiness.
---@class ClientScene
---@field Snapshot fun(self:ClientScene):ProfileSnapshot|nil,ScenarioError|nil
---@field Wait fun(self:ClientScene,scene:ReportedScene,timeout_ms?:integer,afterRevision?:integer):SceneObservation|nil,ScenarioError|nil Partial evidence may accompany an error.
---@field Set fun(self:ClientScene,scene:ReportedScene,timeout_ms?:integer):SceneTransition|nil,ScenarioError|nil Waits for current-room eligibility, requests one ordinary native transition, then waits for matching fresh reported presence and destination native eligibility within one timeout; never replays the action.

---Matching reported presence, native eligibility receipt and owned-client identity.
---Partial evidence remains available when the returned error is non-nil.
---@alias SceneObservation ClientReadiness

---Transition receipt and subsequent observation, not proof of scene rendering.
---When the client already reports the requested scene, Set only probes it and
---does not issue a native action. Partial evidence may accompany an error.
---From Editor, Set(Arsenal) clicks the identified native Cancel button at its
---current bounds. Save/discard prompts are not answered automatically. Return
---to Arsenal before requesting another ship room from Editor.
---@class SceneTransition
---@field location ReportedScene Requested scene label.
---@field is_already_there boolean The initial authenticated report already matched.
---@field action WindowCommandResult|nil Ordinary transition receipt, absent when no action was needed.
---@field observation ClientReadiness Latest report/authentication/native eligibility evidence.

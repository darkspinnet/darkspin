---@meta

---Future participant model: types only. No remote control, party actions,
---shared mission operations, or ...Async functions are enabled by these types.
---@class Participant
---@field profile ProfileIdentity Account/game identity independent of process ownership.
---@field network_origin 'local'|'remote'
---@field managed_client ManagedClient|nil A remote guest may have no owned process or Fang observation.
---@field observation_status 'available'|'unavailable'|'stale'|'disconnected'

---@class PartyParticipant
---@field participant Participant
---@field role 'host'|'guest' Independent of launch mode and network origin.

---@class PartyPlan
---@field members PartyParticipant[] Planned only; ordinary party input adapters remain unavailable.

---@class OperationHandle
---@field id string Reserved nonblocking-operation identity; acceptance is not completion.
---@field participant_name string

---Reserved future participant-operation barrier payload. scenario.Parallel uses
---an error-only result with ScenarioError.Failures instead of this type.
---@class BarrierResult
---@field results table<string,table>
---@field failures table<string,ScenarioError>

---@meta

---Session-owned server party operations using existing Blaze handlers and normal
---notifications. Both actors must be distinct live owned clients/accounts for
---Invite and Join. These are explicit server fixtures, not native UI actions.
---Returned notifications do not prove native acceptance or a shared mission.
---Mutations may retain accepted/partial evidence on error; never replay them
---automatically when an attempted outcome is unknown.
---@class ClientParty
---@field Create fun(self:ClientParty,name?:string):PartyObservation|nil,ScenarioError|nil Creates or returns this actor's server party.
---@field Invite fun(self:ClientParty,other:ManagedClient):PartyObservation|nil,ScenarioError|nil Invites another owned client/account.
---@field Join fun(self:ClientParty,leader:ManagedClient):PartyObservation|nil,ScenarioError|nil Accepts the invitation associated with the specified owned leader.
---@field Snapshot fun(self:ClientParty):PartyObservation|nil,ScenarioError|nil Reads the actor's server party; id 0 means no membership.

---@class PartyObservation
---@field operation string
---@field client_id string Explicit owned actor.
---@field instance_id string Exact owned native process instance.
---@field pid integer
---@field is_mutation_attempted boolean
---@field is_mutation_accepted boolean Server feature acceptance, not native client acceptance.
---@field is_projection_complete boolean Server membership/projection completed.
---@field is_native_acceptance_observed false This server fixture does not prove native acceptance.
---@field notification_count integer Intended normal protocol notifications.
---@field delivered_notification_count integer Sent notifications, not proof they were applied by the native client.
---@field party PartySnapshot

---@class PartySnapshot
---@field id integer Zero means the observed actor has no server party membership.
---@field name string
---@field leader_user_id integer
---@field revision integer Server party revision, independent of client presence revision.
---@field members PartyMemberSnapshot[]

---@class PartyMemberSnapshot
---@field user_id integer
---@field name string
---@field avatar_id integer
---@field join_ordinal integer

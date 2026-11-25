---@meta

---Opaque fresh observation mark. Pass unchanged to the same owned client's Wait.
---@class ChatCursor
---@field offset integer
---@field native_offset integer
---@field instance_id string
---@field user_id integer
---@field login_session_id string

---@class ChatReply
---@field sequence integer Monotonic owned server-log position, strictly after the supplied cursor.
---@field command string Canonical lowercase slash verb, such as /stat.
---@field body string Full normalized command with arguments; native 0x1f and ! prefixes become /.
---@field reply string Exact text queued by the server.
---@field user_id integer Authenticated sender's immutable account ID.
---@field session_id integer Actual Blaze messaging session.
---@field source 'server_command_reply'
---@field is_native_rendered false This proves neither client receipt nor native rendered text.

---@class ChatSnapshot
---@field cursor ChatCursor
---@field source 'server_command_reply'
---@field replies ChatReply[] Empty at a fresh mark; historical replies are not replayed.

---@class ClientChat
local ClientChat = {}

---Capture immediately before UI:Press('Enter'), UI:Type(command), UI:Press('Enter').
---No command dispatch shortcut is exposed. Only this owned client's accepted login is observed.
---@return ChatSnapshot|nil result
---@return ScenarioError|nil err
function ClientChat:Snapshot() end

---Wait for a later queued server reply for the same owned account/login and canonical verb.
---Arguments are inspected via result.body. Native-handled commands without server replies
---(such as exit and client social commands) require separate native/outcome evidence.
---@param cursor ChatCursor
---@param command string Lowercase slash verb only, without arguments.
---@param timeout_ms? integer
---@return ChatReply|nil result
---@return ScenarioError|nil err
function ClientChat:Wait(cursor, command, timeout_ms) end

---@class NativeChatEvent
---@field event string Observed existing Fang trace marker; not rendered-text evidence.
---@field value integer
---@field sequence integer Owned trace byte position after the cursor.
---@field source 'native_client_trace'

---Reads only this owned launch's existing native trace. No hook or input is installed.
---Waiting for effect_preview_override fails if effect_preview_failure is observed.
---@param cursor ChatCursor
---@param event 'effect_preview_override'|'effect_preview_failure'|'chat_reset_post'|'chat_exit'
---@param timeout_ms? integer
---@return NativeChatEvent|nil result
---@return ScenarioError|nil err
function ClientChat:WaitNative(cursor, event, timeout_ms) end

---@class ChatArtifactRoot
---@field role string
---@field path string
---@field is_available boolean
---@field reason? string

---@class ChatArtifact
---@field role string
---@field path string
---@field name string
---@field byte_size integer
---@field modified_at string

---@class ChatArtifactSnapshot
---@field captured_at string
---@field source 'owned_artifact_metadata'
---@field roots ChatArtifactRoot[] Missing/unavailable roots are explicit.
---@field files ChatArtifact[] Metadata only, limited to this run's bug/snapshot roots and this launch's diagnostic root.

---Compare before/after names, sizes and modification times. A queued reply is not a saved artifact.
---Scenario /ss dump produces snapshot files; its automatic ZIP archive is disabled.
---@return ChatArtifactSnapshot|nil result
---@return ScenarioError|nil err
function ClientChat:Artifacts() end

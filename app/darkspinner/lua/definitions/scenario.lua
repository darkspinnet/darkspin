---@meta

---Development API; definitions are editor metadata and are never executed.
---Call operations from the returned entry function; source evaluation disables them.
scenario = {}

---Raises a non-nil error with its structured cause and failing Lua call-site traceback.
---@param err ScenarioError|string|nil
function scenario.Check(err) end

---Requires a result. Raises on a non-nil error or nil result; preserves false.
---Handle legitimate optional results explicitly with Check instead.
---@generic T
---@param result T|nil
---@param err ScenarioError|string|nil
---@return T
function scenario.Must(result, err) end

---@param version integer
---@return ScenarioError|nil err
function scenario.RequireAPI(version) end

---Monotonic elapsed milliseconds since this script execution began.
---@return number milliseconds Fractional milliseconds are permitted; no error result.
function scenario.Time() end

---Cancellable delay; other Parallel branches can progress during it. This only
---allows time to pass and establishes no scene, rendering or input readiness.
---@param duration_ms integer From 1 to 120000 milliseconds.
---@return ScenarioError|nil err
function scenario.Sleep(duration_ms) end

---Writes one message to the launcher's scenario output.
---@param message string One nonempty line, at most 2048 bytes.
---@return ScenarioError|nil err
function scenario.Log(message) end

---@return SessionSnapshot|nil result
---@return ScenarioError|nil err
function scenario.Snapshot() end

---Starts or reuses this session's owned loopback server with matching inputs.
---Changed inputs require stopping the session. Network multiplayer remains disabled.
---@param req ServerStartRequest
---@return ServerHandle|nil result
---@return ScenarioError|nil err
function scenario.StartServer(req) end

---Runs at most eight named branches and blocks at their shared completion barrier.
---All branches settle before return; ScenarioError.Failures retains every failed
---branch. Profile creation/logout, client launch/close and logging are supported, so
---workers can take successive jobs on one server. Lifecycle mutations serialize;
---independent client loads/waits overlap. StartServer and profile reset remain
---outside branches; nested Parallel is rejected. Do not wrap yielding operations
---inside pcall/xpcall in a branch; use returned operation errors or Parallel's
---structured branch failures so the scheduler can drain outstanding requests.
---@param branches table<string,fun():nil|ScenarioError|string>
---@return ScenarioError|nil err
function scenario.Parallel(branches) end

---Compatibility entry point; prefer server:CreateProfile(req).
---@deprecated
---@param req ProfileRequest
---@return ProfileHandle|nil result
---@return ScenarioError|nil err
function scenario.CreateProfile(req) end

---Compatibility entry point; prefer server:ResetProfile(req).
---@deprecated
---@param req ProfileRequest Explicit new seed or allowed source recipe.
---@return ProfileHandle|nil result
---@return ScenarioError|nil err
function scenario.ResetProfile(req) end

---Compatibility entry point; prefer profile:Launch(req).
---@deprecated
---@param req ClientLaunchRequest
---@return ManagedClient|nil result
---@return ScenarioError|nil err
function scenario.LaunchClient(req) end

---Compatibility entry point; prefer profile:Snapshot() or client.Scene:Snapshot().
---@deprecated
---@param profileName string Owned profile alias.
---@return ProfileSnapshot|nil result
---@return ScenarioError|nil err
function scenario.SnapshotProfile(profileName) end

---Compatibility entry point; prefer client:Login() or profile:WaitForLogin(timeoutMs).
---@deprecated
---@param profileName string Owned profile alias.
---@param timeoutMs integer From 1 to 120000 milliseconds.
---@return LoginObservation|nil result
---@return ScenarioError|nil err
function scenario.WaitForLogin(profileName, timeoutMs) end

---Compatibility entry point; prefer profile:WaitForReportedLocation(...) or client.Scene:Wait(...).
---@deprecated
---@param profileName string Owned profile alias.
---@param location ReportedLocation
---@param timeoutMs integer From 1 to 120000 milliseconds.
---@param afterRevision? integer Nonnegative changed-envelope revision, defaults to 0.
---@return ProfileSnapshot|nil result Last partial observation is retained on timeout.
---@return ScenarioError|nil err
function scenario.WaitForReportedLocation(profileName, location, timeoutMs, afterRevision) end

---Compatibility entry point; prefer client:Snapshot().
---@deprecated
---@param clientID string
---@return ClientSnapshot|nil result
---@return ScenarioError|nil err
function scenario.SnapshotClient(clientID) end

---Returns a handle for an already running client owned by this supervisor, allowing another batch without relaunching. Its Time() clock starts at lookup. Missing or exited clients return an error; this never attaches to an external process.
---@param clientID string
---@return ManagedClient|nil result
---@return ScenarioError|nil err
function scenario.GetClient(clientID) end

---Compatibility entry point; prefer client.UI:InspectWindow().
---@deprecated
---@param clientID string
---@return WindowReceipt|nil result
---@return ScenarioError|nil err
function scenario.ClientWindow(clientID) end

---Compatibility entry point; prefer client.UI:Click(x,y).
---@deprecated
---@param clientID string
---@param x integer
---@param y integer
---@return WindowReceipt|nil result
---@return ScenarioError|nil err
function scenario.Click(clientID, x, y) end

---Compatibility entry point; prefer client.UI:Screenshot(label).
---@deprecated
---@param clientID string
---@param label string Lowercase letters/digits/underscore/hyphen, 1-48 characters; starts with letter/digit.
---@return ScreenshotArtifact|nil artifact Partial metadata may accompany an error.
---@return ScenarioError|nil err
function scenario.Screenshot(clientID, label) end

---Compatibility entry point; prefer client:Close().
---@deprecated
---@param clientID string
---@return ScenarioError|nil err
function scenario.CloseClient(clientID) end

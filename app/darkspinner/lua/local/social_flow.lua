---Native Social-panel party flow at the verified 1280x720 client layout.
---Party:Snapshot is observation only; no server party fixture mutates this run.
local must = scenario.Must
local check = scenario.Check

---@param client ManagedClient
---@param x integer
---@param y integer
local function click(client, x, y)
    must(client.UI:Click(x, y))
    check(scenario.Sleep(600)) -- Let the ordinary UI consume its queued action.
end

---@param normal ManagedClient
---@param detached ManagedClient
---@param afterRevision? integer
---@return string|nil
local function waitParty(normal, detached, afterRevision)
    local deadline = scenario.Time() + 10000
    while scenario.Time() < deadline do
        local first = must(normal.Party:Snapshot())
        local second = must(detached.Party:Snapshot())
        if first.party.id ~= 0 and first.party.id == second.party.id
            and #first.party.members == 2 and #second.party.members == 2
            and first.party.revision > (afterRevision or 0)
            and second.party.revision > (afterRevision or 0) then
            return nil
        end
        check(scenario.Sleep(150))
    end
    must(normal.Diagnostics:Capture("native-party-failed"))
    must(detached.Diagnostics:Capture("native-party-failed"))
    return "native invite/accept did not establish the same two-member party"
end

return function()
    check(scenario.RequireAPI(1))
    local server = must(scenario.StartServer({}))
    local normalProfile = must(server:CreateProfile({
        name = "normal", hero_nouns = {1667741389, 3618328019, 749013658}, chain_progression = 1,
    }))
    local detachedProfile = must(server:CreateProfile({
        name = "detached", hero_nouns = {576185321, 3618328019, 749013658}, chain_progression = 1,
    }))
    local normal = must(normalProfile:Launch({client_id = "normal", launch_mode = "normal"}))
    local detached = must(detachedProfile:Launch({client_id = "detached", launch_mode = "detached"}))
    must(normal:Login())
    must(detached:Login())
    must(normal.Scene:Wait(scene.Arsenal))
    must(detached.Scene:Wait(scene.Arsenal))
    for _, client in ipairs({normal, detached}) do
        local receipt = must(client.UI:InspectWindow())
        if receipt.window == nil or receipt.window.width ~= 1280 or receipt.window.height ~= 720 then
            return "native social starter requires the verified 1280x720 client layout"
        end
    end
    click(normal, 105, 35) -- Social.
    click(normal, 368, 182) -- Lobby tab; exactly one other owned account.
    must(normal.UI:Screenshot("native-lobby"))
    click(normal, 930, 265) -- Ordinary Invite To Party action.
    must(detached.UI:Screenshot("native-invitation"))
    click(detached, 604, 53) -- Ordinary Accept, within the invitation lifetime.
    local partyError = waitParty(normal, detached)
    if partyError ~= nil then
        return partyError
    end
    must(normal.Diagnostics:Capture("native-party"))
    must(detached.Diagnostics:Capture("native-party"))

    click(normal, 759, 498) -- Dismiss the native invitation confirmation.
    click(normal, 962, 265) -- Add the actual lobby player as a friend.
    must(normal.UI:Screenshot("friend-added"))
    click(normal, 759, 498) -- Dismiss Add Friend confirmation.
    click(normal, 1036, 103) -- Close Social.
    click(normal, 120, 701) -- Focus the native chat edit control.
    local typed = must(normal.UI:Type("social-native-normal"))
    if typed.processed_character_count ~= #"social-native-normal" then
        return "native chat input did not process the complete text"
    end
    must(normal.UI:Press("Enter"))
    check(scenario.Sleep(800))
    -- A saved recipient image is evidence to inspect, not machine-verified text.
    must(detached.UI:Screenshot("native-chat-received"))
    click(normal, 165, 35) -- Party gained a second portrait; Social shifted right.
    click(normal, 268, 182) -- Friends.
    must(normal.UI:Screenshot("friend-online"))

    check(detached:Terminate()) -- Abrupt loss, retaining the existing account.
    check(scenario.Sleep(1000))
    local retained = must(normal.Party:Snapshot())
    if retained.party.id == 0 or #retained.party.members ~= 1 then
        return "disconnected ship client was not removed from the native party"
    end
    must(normal.Diagnostics:Capture("friend-offline"))
    local reconnected = must(detachedProfile:Launch({
        client_id = "detached-reconnected", launch_mode = "detached",
    }))
    must(reconnected:Login())
    must(reconnected.Scene:Wait(scene.Arsenal))
    -- Reconnect through the ordinary invitation and Join callback.
    click(normal, 930, 265)
    must(reconnected.UI:Screenshot("native-reinvitation"))
    click(reconnected, 604, 53)
    partyError = waitParty(normal, reconnected, retained.party.revision)
    if partyError ~= nil then
        return partyError
    end
    click(normal, 759, 498) -- Dismiss the fresh native invitation confirmation.
    check(scenario.Sleep(1000))
    must(normal.Diagnostics:Capture("friend-reconnected"))
    must(reconnected.Diagnostics:Capture("party-restored"))
    -- A newly launched client's chat edit is opened by the ordinary Enter key.
    must(reconnected.UI:Press("Enter"))
    check(scenario.Sleep(400))
    must(reconnected.UI:Type("/party social-native-reconnected"))
    must(reconnected.UI:Press("Enter"))
    check(scenario.Sleep(800))
    must(normal.UI:Screenshot("reconnected-chat"))
    must(normal.UI:Click(354, 265, "right"))
    check(scenario.Sleep(600))
    must(normal.UI:Screenshot("friend-context"))
    click(normal, 455, 381) -- View Profile in the observed friend context menu.
    check(scenario.Sleep(1200))
    must(normal.Diagnostics:Capture("friended-profile"))
    -- Profile rendering and displayed chat/roster require capture inspection.
    return nil
end

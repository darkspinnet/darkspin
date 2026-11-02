---Two owned clients joining one server party through the explicit server fixture.
---Protocol notifications and server membership do not prove native UI acceptance
---or mission entry. No mission-start or gameplay-readiness action is implied.
local must = scenario.Must
local check = scenario.Check

return function()
    check(scenario.RequireAPI(1))
    local server = must(scenario.StartServer({}))
    local normalProfile = must(server:CreateProfile({
        name = "normal", hero_nouns = {1667741389, 3618328019, 749013658}, chain_progression = 1,
    }))
    local detachedProfile = must(server:CreateProfile({
        name = "detached", source_login_name = normalProfile.login_name, is_source_clone_allowed = true,
    }))
    local normal = must(normalProfile:Launch({
        client_id = "normal", launch_mode = "normal",
    }))
    local detached = must(detachedProfile:Launch({
        client_id = "detached", launch_mode = "detached",
    }))
    must(normal:Login())
    must(detached:Login())
    must(normal.Scene:Wait(scene.Arsenal))
    must(detached.Scene:Wait(scene.Arsenal))

    must(normal.Party:Create("local-ship-party"))
    must(normal.Party:Invite(detached))
    must(detached.Party:Join(normal))
    local normalParty = must(normal.Party:Snapshot())
    local detachedParty = must(detached.Party:Snapshot())
    if normalParty.party.id == 0 or normalParty.party.id ~= detachedParty.party.id then
        return "owned clients do not observe the same server party"
    end
    if #normalParty.party.members ~= 2 or #detachedParty.party.members ~= 2 then
        return "server party does not contain both owned participants"
    end
    must(normal.UI:Screenshot("party-server-membership"))
    must(detached.UI:Screenshot("party-server-membership"))
    return nil
end

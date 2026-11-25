---Native /invite and /leave, plus observable co-op /follow and /ai effects.
---Native messaging/relation/preference/dance cases remain explicit coverage gates.
---This scenario intentionally fails its final coverage verdict until those read
---observers exist. Typing a command or saving a screenshot is never their pass.
local must = scenario.Must
local check = scenario.Check

local nativeCases = {
    {command="/tell", syntax="/tell <owned-name> <marker>", reason="missing owned recipient/channel message observation"},
    {command="/reply", syntax="/reply <marker>", reason="missing last-tell sender and actual reply recipient observation"},
    {command="/party", syntax="/party <marker>", reason="missing party-message delivery and outsider-exclusion observation"},
    {command="/game", syntax="/game <marker>", reason="missing game-message delivery and cross-game exclusion observation"},
    {command="/lobby", syntax="/lobby <marker>", reason="missing native lobby membership and message delivery observation"},
    {command="/invite", syntax="/invite <owned-name>"},
    {command="/leave", syntax="/leave"},
    {command="/friend", syntax="/friend <owned-name>", reason="missing owned durable friend relation/request observation"},
    {command="/unfriend", syntax="/unfriend <owned-name>", reason="missing owned durable friendship removal observation"},
    {command="/block", syntax="/block <owned-name>", reason="missing block relation and delivery filtering observation"},
    {command="/unblock", syntax="/unblock <owned-name>", reason="missing unblock relation and restored message delivery observation"},
    {command="/setprofanityfilter", syntax="/setprofanityfilter 0 and 1", reason="missing native filter preference and rendered-message observation"},
    {command="/dance", syntax="/dance", reason="missing independent native animation-state observation for the controlled hero"},
}

local function record(status, command, detail)
    check(scenario.Log(status .. " command=" .. command .. " " .. detail))
end

local function capture(client, label)
    local diagnostic = must(client.Diagnostics:Capture(label))
    local image = must(client.UI:Screenshot(label))
    check(scenario.Log("EVIDENCE " .. label .. " manifest=" .. diagnostic.manifest_path .. " image=" .. image.image_path))
end

local function typeCommand(client, text)
    must(client.UI:Press("Enter"))
    check(scenario.Sleep(250)) -- Text-control opening allowance, not acceptance.
    local typed = must(client.UI:Type(text))
    if typed.processed_character_count ~= #text then error("partial chat input; no resubmission") end
    must(client.UI:Press("Enter"))
end

local function serverCommand(client, text, verb, expected)
    local mark = must(client.Chat:Snapshot())
    typeCommand(client, text)
    local reply = must(client.Chat:Wait(mark.cursor, verb, 12000))
    if reply.source ~= "server_command_reply" or reply.command ~= verb or reply.body ~= text
        or not string.find(reply.reply, expected, 1, true) then
        error("unexpected correlated reply for " .. text .. "; no resubmission")
    end
    return reply
end

local function waitFor(label, timeout, observe)
    local deadline = scenario.Time() + timeout
    repeat
        if observe() then return end
        check(scenario.Sleep(100))
    until scenario.Time() >= deadline
    error(label .. " was not independently observed; no resubmission")
end

local function distance(first, second)
    local x, y, z = first.x-second.x, first.y-second.y, first.z-second.z
    return math.sqrt(x*x+y*y+z*z)
end

local function nativePosition(client)
    local native = must(client.World:SnapshotNative()).gameplay_state
    if native == nil or not native.is_available or not native.is_world_active or native.position == nil then
        error("native controlled-hero position unavailable")
    end
    return native.position
end

local function sameEpoch(client, baseline)
    local world = must(client.World:SnapshotLiving())
    if world.game_id ~= baseline.game_id or world.peer_generation ~= baseline.peer_generation
        or world.transport_generation ~= baseline.transport_generation
        or world.zone_generation ~= baseline.zone_generation then error("social scenario world ownership changed") end
    return world
end

local function partyName(client, userID)
    local party = must(client.Party:Snapshot()).party
    for _, member in ipairs(party.members) do
        if member.user_id == userID then return member.name end
    end
    error("owned account display name unavailable in its setup party")
end

local function nativeParty(leader, follower, profiles)
    -- Setup parties expose actual authenticated display names. The tested
    -- invitation and acceptance are native chat/UI, not Party:Invite/Join.
    must(leader.Party:Create("commands-social-leader"))
    must(follower.Party:Create("commands-social-follower"))
    local leaderName = partyName(leader, profiles[1].user_id)
    local followerName = partyName(follower, profiles[2].user_id)
    for _, name in ipairs({leaderName, followerName}) do
        if name == "" or string.find(name, "%s") then
            error("native command fixture requires observed single-token owned display names")
        end
    end
    typeCommand(follower, "/leave")
    waitFor("native /leave membership removal", 12000, function()
        return must(follower.Party:Snapshot()).party.id == 0
    end)
    record("PASS", "/leave", "owned setup-party membership removed")

    typeCommand(leader, "/invite " .. followerName)
    check(scenario.Sleep(750)) -- Invitation animation allowance; membership proves outcome.
    must(follower.UI:Click(604, 53)) -- Verified 1280x720 native invitation Accept.
    waitFor("native /invite and Accept", 15000, function()
        local first = must(leader.Party:Snapshot()).party
        local second = must(follower.Party:Snapshot()).party
        return first.id ~= 0 and first.id == second.id and #first.members == 2 and #second.members == 2
    end)
    record("PASS", "/invite", "native invitation Accept established the shared two-member party")
    return leaderName
end

local function enterMission(clients)
    for _, client in ipairs(clients) do
        must(client.World:Configure({run_seed=104729, is_ambient_population_suppressed=true}))
        must(client.Scene:Set(scene.Navigation))
    end
    must(clients[1].Mission:Select(1))
    must(clients[1].Mission:Start())
    check(scenario.Parallel({
        leader=function() must(clients[1].Mission:Continue()); return nil end,
        follower=function() must(clients[2].Mission:Continue()); return nil end,
    }))
    local baselines = {must(clients[1].World:Ready()), must(clients[2].World:Ready())}
    if baselines[1].game_id == 0 or baselines[1].game_id ~= baselines[2].game_id
        or baselines[1].zone_generation ~= baselines[2].zone_generation then
        error("owned clients did not enter one committed co-op world")
    end
    return baselines
end

local function reachablePoint(client, origin, radius)
    for _, offset in ipairs({{radius,0},{-radius,0},{0,radius},{0,-radius}}) do
        local point = {x=origin.x+offset[1], y=origin.y+offset[2], z=origin.z}
        local path = must(client.World:Path(origin, point))
        if path.is_available and path.is_complete and not path.is_truncated and path.goal ~= nil
            and path.goal.position ~= nil and distance(origin, path.goal.position) >= radius*0.8 then
            return path.goal.position
        end
    end
    error("fixture has no observed complete nearby navigation route")
end

local function followCase(clients, baselines, leaderName)
    local leader, follower = clients[1], clients[2]
    local origin = nativePosition(follower)
    local destination = reachablePoint(leader, origin, 14)
    must(leader.World:PlaceHero(destination)) -- Fixture, not the /follow action.
    waitFor("leader fixture native arrival", 10000, function()
        sameEpoch(leader, baselines[1])
        return distance(nativePosition(leader), destination) < 1
    end)
    local before = nativePosition(follower)
    serverCommand(follower, "/follow " .. leaderName, "/follow", "Following " .. leaderName)
    waitFor("actual native following", 15000, function()
        sameEpoch(follower, baselines[2])
        local current = nativePosition(follower)
        return distance(current, before) > 2 and distance(current, destination) < distance(before, destination)-2
    end)
    record("PASS", "/follow", "correlated acceptance plus native movement toward the named owned ally")

    must(follower.World:Click(origin)) -- Real movement input must cancel follow.
    waitFor("movement cancellation and arrival", 15000, function()
        sameEpoch(follower, baselines[2])
        return distance(nativePosition(follower), origin) < 1.5
    end)
    local settled = nativePosition(follower)
    check(scenario.Sleep(2000))
    sameEpoch(follower, baselines[2])
    if distance(nativePosition(follower), settled) > 1 then error("follow resumed after manual movement cancellation") end
    record("PASS", "/follow", "manual movement canceled following without another toggle")
end

local function aiCase(clients, baselines)
    local actor = clients[1]
    local origin = nativePosition(actor)
    local target = must(actor.World:SpawnControlledNPC("CitadelBasicMelee.Noun"))
    must(target:Move(reachablePoint(actor, origin, 4)))
    local before = must(target:Snapshot())
    if not before.is_available or before.is_ai_enabled or before.hit_point <= 0 then error("AI-off target fixture unavailable") end
    serverCommand(actor, "/ai", "/ai", "AI toggle requested")
    waitFor("AI attacks without human combat input", 20000, function()
        sameEpoch(actor, baselines[1])
        sameEpoch(clients[2], baselines[2])
        local current = must(target:Snapshot())
        if not current.is_available then error("AI target observation unavailable") end
        return current.hit_point < before.hit_point
    end)
    record("PASS", "/ai", "AI-off hostile lost authoritative HP without human attacks")
    serverCommand(actor, "/ai", "/ai", "AI toggle requested")
    -- A current projectile may settle after disabling. Retire this isolated
    -- target through its owning fixture rather than infer no in-flight damage.
    must(target:Retire())
    record("EVIDENCE", "/ai", "second toggle accepted; no claim that acceptance alone proves deactivation")
end

return function()
    check(scenario.RequireAPI(1))
    local server = must(scenario.StartServer({}))
    local profiles, clients = {}, {}
    for index, name in ipairs({"commands-social-first", "commands-social-second"}) do
        profiles[index] = must(server:CreateProfile({name=name, is_admin=true,
            hero_nouns={1667741389,3618328019,749013658}, chain_progression=1}))
        clients[index] = must(profiles[index]:Launch({client_id=name, launch_mode="normal",
            screen_width=1280, screen_height=720, native_ui_scale_percent=100,
            is_intro_skipped=true, is_cinematic_skipped=true}))
        check(clients[index]:SetTimeout(90000))
        must(clients[index]:Login())
        must(clients[index].Scene:Wait(scene.Arsenal))
        local window = must(clients[index].UI:InspectWindow()).window
        if window == nil or window.width ~= 1280 or window.height ~= 720 then
            error("native party acceptance requires the verified 1280x720 layout")
        end
    end

    local leaderName = nativeParty(clients[1], clients[2], profiles)
    local baselines = enterMission(clients)
    followCase(clients, baselines, leaderName)
    aiCase(clients, baselines)
    for index, client in ipairs(clients) do capture(client, "commands-social-final-" .. index) end

    local blocked = 0
    for _, case in ipairs(nativeCases) do
        if case.reason ~= nil then
            blocked = blocked+1
            record("BLOCKED", case.command, "syntax=" .. case.syntax .. " reason=" .. case.reason
                .. "; not submitted because its actual effect cannot be asserted")
        end
    end
    record("BLOCKED", "native-inventory", "the 13 help-advertised names are not a recovered exhaustive build-103 parser table")
    record("BLOCKED", "/ai-off", "no direct mode observation; second reply proves acceptance only")
    error("command coverage incomplete: " .. blocked .. " native effect observers plus inventory and AI-off gates; no false pass")
end

---Native /event aliases and one-shot /warp consumption. Use --lua-timeout 20m.
---Each destructive event sequence has its own isolated account and client.
local must = scenario.Must
local check = scenario.Check

local function capture(client, label)
    local report = must(client.Diagnostics:Capture(label))
    check(scenario.Log(label .. " manifest=" .. report.manifest_path))
end

local function send(client, text, expected)
    local cursor = must(client.Chat:Snapshot()).cursor
    if not must(client.UI:Press("Enter")).is_processed then error("chat open unprocessed; no retry") end
    check(scenario.Sleep(250))
    local typed = must(client.UI:Type(text))
    if typed.processed_character_count ~= #text then error("partial command input; no retry") end
    if not must(client.UI:Press("Enter")).is_processed then error("chat submit unprocessed; no retry") end
    local reply = must(client.Chat:Wait(cursor, string.match(text, "^(/[%a]+)"), 30000))
    if reply.source ~= "server_command_reply" or reply.is_native_rendered ~= false
        or not string.find(reply.reply, expected, 1, true) then
        capture(client, "commands-events-reply")
        error("unexpected reply to " .. text .. ": " .. reply.reply)
    end
    check(scenario.Log("REPLY " .. text .. " => " .. reply.reply))
    return reply
end

local function waitWorld(client, initial, label, observe)
    local deadline = scenario.Time() + 20000
    repeat
        local current = must(client.World:Snapshot())
        if not current.is_available or current.game_id ~= initial.game_id
            or current.zone_generation ~= initial.zone_generation then
            capture(client, "commands-events-epoch")
            error("world identity lost while observing " .. label)
        end
        if observe(current) then check(scenario.Log("PASS " .. label)); return current end
        check(scenario.Sleep(100))
    until scenario.Time() >= deadline
    capture(client, "commands-events-timeout")
    error("no independent effect for " .. label)
end

local function launch(server, name)
    local profile = must(server:CreateProfile({name = name, is_admin = true,
        hero_nouns = {1667741389, 3618328019, 749013658}, chain_progression = 1}))
    local client = must(profile:Launch({client_id = name, launch_mode = "normal",
        is_intro_skipped = true, is_cinematic_skipped = true}))
    check(client:SetTimeout(90000))
    must(client:Login())
    must(client.Scene:Wait(scene.Arsenal))
    must(client.World:Configure({run_seed = 672481, is_ambient_population_suppressed = true}))
    return client, profile
end

local function enter(client)
    must(client.Scene:Set(scene.Navigation))
    return must(client.Mission:Enter(1))
end

local function events(server, name, aliases)
    local client = launch(server, name)
    local initial = enter(client)
    send(client, "/god", "God mode enabled")
    send(client, "/event", "Events:")
    send(client, "/event invalid", "Events:")
    send(client, "/event " .. aliases[1], "Triggering event security-next")
    waitWorld(client, initial, aliases[1] .. " teleports to the next security route", function(current)
        local a, b = initial.hero.position, current.hero.position
        return (a.x-b.x)^2 + (a.y-b.y)^2 + (a.z-b.z)^2 > 25
    end)
    send(client, "/event " .. aliases[2], "Triggering event boss-start")
    waitWorld(client, initial, aliases[2] .. " publishes a living boss", function(current)
        if current.boss == nil or current.boss.leader_object_id == 0 or current.boss.is_complete then return false end
        for _, npc in ipairs(current.npcs) do
            if npc.object_id == current.boss.leader_object_id and npc.is_published and not npc.is_dead then return true end
        end
        return false
    end)
    send(client, "/event " .. aliases[3], "Triggering event boss-complete")
    waitWorld(client, initial, aliases[3] .. " actually completes that boss", function(current)
        return current.boss ~= nil and current.boss.leader_object_id ~= 0 and current.boss.is_complete
    end)
    capture(client, name .. "-complete")
    check(client:Terminate())
end

local function warp(server)
    local client, profile = launch(server, "commands-warp")
    -- Area 18 is the installed test_AI_arena map, not a numbered campaign mission.
    -- Sending its numeric alias exercises Fang's normalization before launch.
    local reply = send(client, "/warp 18", "Next campaign mission will warp to test_AI_arena")
    if reply.body ~= "/warp test_AI_arena" then error("Fang did not normalize the numeric warp alias") end
    enter(client)
    local observed = must(profile:Snapshot())
    if observed.mission.asset ~= "test_AI_arena" then
        capture(client, "commands-warp-wrong-map")
        error("accepted /warp was not consumed by the next mission")
    end
    capture(client, "commands-warp-complete")
    check(client:Terminate())
end

return function()
    check(scenario.RequireAPI(1))
    local server = must(scenario.StartServer({}))
    events(server, "commands-events-numeric", {"1", "2", "3"})
    events(server, "commands-events-named", {"security-next", "boss-start", "boss-complete"})
    warp(server)
    check(scenario.Log("COMPLETE event aliases and numeric warp: independent world effects observed"))
    return nil
end

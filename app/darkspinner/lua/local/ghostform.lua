---Native Ghost Form expiry/recovery and AI-companion placement investigation.
local must, check = scenario.Must, scenario.Check

local function capture(client, label)
    local report = must(client.Diagnostics:Capture(label))
    must(client.UI:Screenshot(label))
    local world = must(client.World:Snapshot())
    check(scenario.Log("capture " .. label .. " " .. report.manifest_path))
    if world.campaign then
        for _, member in ipairs(world.campaign.members) do
            local hero = member.characters[member.deployed_index + 1]
            if hero and hero.is_movement_available then
                local position = hero.movement_position
                check(scenario.Log("placement " .. label .. " user=" .. member.user_id .. " index=" .. member.deployed_index
                    .. " object=" .. hero.object_id .. " noun=" .. hero.noun .. " hp=" .. hero.hit_point
                    .. " position=" .. position.x .. "," .. position.y .. "," .. position.z))
            end
        end
    end
    return world
end

local function castGhost(client, label)
    local before = must(client.World:Snapshot())
    if before.hero.noun ~= 3591937345 then error("Ghost Form needs the owned Wraith Alpha") end
    local previous = before.ability and before.ability.last_dispatch and before.ability.last_dispatch.revision or 0
    local native = must(client.World:SnapshotNative()).gameplay_state
    if not native or not native.is_rig_ready then error("native Wraith rig unavailable") end
    local button = must(client.UI:Click(640, 655))
    if not button.is_processed then error("Ghost Form hotbar input unprocessed") end
    check(scenario.Sleep(300))
    local click = must(client.World:Click({x=native.position.x,y=native.position.y,z=native.position.z+1.5}))
    if not click.is_processed then error("Ghost Form world input unprocessed") end
    local deadline = scenario.Time()+4000
    repeat
        local world = must(client.World:Snapshot())
        local dispatch = world.ability and world.ability.last_dispatch
        if dispatch and dispatch.revision > previous and dispatch.ability_asset == "Ghostform" then
            check(scenario.Sleep(250))
            return capture(client, label)
        end
        check(scenario.Sleep(100))
    until scenario.Time() >= deadline
    capture(client,label .. "-unconfirmed")
    error("no new native Ghost Form dispatch; no automatic replay")
end

local function moveNearby(client)
    local native = must(client.World:SnapshotNative()).gameplay_state
    if not native or not native.is_rig_ready then error("native movement actor unavailable") end
    for _, offset in ipairs({{8,0},{0,8},{-8,0},{0,-8}}) do
        local goal={x=native.position.x+offset[1],y=native.position.y+offset[2],z=native.position.z}
        local path=must(client.World:Path(native.position,goal))
        if path.is_available and path.is_complete and not path.is_truncated and path.goal then
            local projected=must(client.World:Project(path.goal.position)).projection
            if projected and projected.is_available and projected.is_on_screen then
                local click=must(client.World:Click(path.goal.position))
                if not click.is_processed then error("native move unprocessed") end
                return
            end
        end
    end
    error("no nearby complete movement lane")
end

local function enterAI(profile)
    local client=must(profile:Launch({client_id="ghost-ai",launch_mode="normal",screen_width=1280,screen_height=720,
        native_ui_scale_percent=100,is_intro_skipped=true}))
    check(client:SetTimeout(90000))
    must(client:Login())
    must(client.Scene:Wait(scene.Arsenal))
    must(client.Scene:Set(scene.Navigation))
    must(client.Mission:Select(1))
    local window=must(client.UI:InspectWindow()).window
    local map=must(client.Mission:SnapshotSelection()).map_state
    if not window or window.width~=1280 or window.height~=720 or not map or not map.is_loaded
        or map.state~=5 or map.game_type~=2 or map.difficulty~=1 or map.animation_state~=0
        or map.animation_difficulty~=1 or map.party_member_count>1 or map.is_transactions_processing
        or not map.are_party_members_ready or map.is_start_disabled or not map.is_start_visible then
        error("native Matchmaker requires settled 1280x720 solo campaign selection")
    end
    local click=must(client.UI:Click(540,490))
    if not click.is_processed then error("native Matchmaker unprocessed") end
    must(client.Mission:WaitDeployment())
    must(client.Mission:Continue())
    must(client.Mission:Wait())
    return client
end

return function()
    check(scenario.RequireAPI(1))
    local server=must(scenario.StartServer({}))
    local profile=must(server:CreateProfile({name="ghost-ai",hero_nouns={3591937345,1667741389,749013658},chain_progression=1}))
    local client=enterAI(profile)
    check(scenario.Sleep(1800))
    local opening=capture(client,"ai-opening")
    if not opening.campaign or #opening.campaign.members<2 then error("co-op companion missing") end
    castGhost(client,"ghost-active")
    moveNearby(client)
    check(scenario.Sleep(700))
    must(client.Ability:Cancel())
    check(scenario.Sleep(700))
    capture(client,"ghost-after-native-cancel")
    check(scenario.Sleep(6500))
    capture(client,"ghost-expired")
    must(client.World:SelectHero(1))
    moveNearby(client)
    check(scenario.Sleep(6000))
    capture(client,"ai-after-owner-swap-and-follow")
    check(scenario.Log("Co-op Ghost Form and placement observations complete; starting owned solo recovery."))

    local soloProfile=must(server:CreateProfile({name="ghost-recovery",hero_nouns={3591937345,1667741389,749013658},chain_progression=1}))
    local solo=must(soloProfile:Launch({client_id="ghost-recovery",launch_mode="normal",screen_width=1280,screen_height=720,
        native_ui_scale_percent=100,is_intro_skipped=true}))
    check(solo:SetTimeout(90000))
    must(solo:Login())
    must(solo.Scene:Wait(scene.Arsenal))
    must(solo.World:Configure({run_seed=162,is_ambient_population_suppressed=true}))
    must(solo.Scene:Set(scene.Navigation))
    must(solo.Mission:Enter(1))
    check(scenario.Sleep(1800))
    castGhost(solo,"recovery-ghost-active")
    local replacement=must(solo.World:ReplaceHero(753829266))
    if not replacement.is_replacement or not replacement.is_rig_ready then error("owned recovery did not settle") end
    capture(solo,"ghost-after-full-recovery")
    check(scenario.Sleep(7000))
    capture(solo,"ghost-after-recovery-expiry-deadline")
    capture(client,"ai-final-placement")
    check(scenario.Log("Ghost Form normal expiry, native cancel, recovery, and companion placement captured; inspect modifier packets and images."))
end

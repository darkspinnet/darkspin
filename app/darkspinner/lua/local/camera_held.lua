---Bounded real-client investigation. Candidate thresholds are diagnostic only.
local must, check = scenario.Must, scenario.Check
local function distance(a, b)
    local x, y = a.x-b.x, a.y-b.y
    return math.sqrt(x*x+y*y)
end
local function capture(run, label)
    local artifact, err = run.client.Diagnostics:Capture(label)
    if err ~= nil then error(label .. ': capture failed: ' .. tostring(err)) end
    return artifact.manifest_path
end
local function observe(run)
    local worldSS = must(run.client.World:SnapshotLiving())
    if worldSS.instance_id ~= run.initial.instance_id or worldSS.game_id ~= run.initial.game_id
        or worldSS.peer_generation ~= run.initial.peer_generation
        or worldSS.zone_generation ~= run.initial.zone_generation then
        error('owned world changed; no input replay')
    end
    local receipt = must(run.client.World:SnapshotNative())
    local nativeSS = receipt.gameplay_state
    if not receipt.is_processed or nativeSS == nil or not nativeSS.is_available
        or nativeSS.camera == nil or not nativeSS.camera.is_available then
        error('native camera observation unavailable')
    end
    local cameraSS = nativeSS.camera
    local reason = nil
    if not cameraSS.is_override_active and cameraSS.mode == 5 then
        if cameraSS.target_handle ~= nativeSS.controlled_handle then reason = 'follow target mismatch'
        elseif cameraSS.is_anchor_available and cameraSS.source_position ~= nil
            and cameraSS.center ~= nil and distance(cameraSS.center, cameraSS.source_position) > 4 then
            reason = 'camera center/source gap above four units'
        end
    end
    if reason ~= nil then
        if run.candidate ~= reason then run.candidate, run.candidateAt = reason, scenario.Time()
        elseif scenario.Time()-run.candidateAt >= 2000 then
            error(reason .. '; sustained two seconds; candidate requires review; manifest=' .. capture(run, 'camera-candidate'))
        end
    else run.candidate, run.candidateAt = nil, nil end
    run.sampleCount = run.sampleCount+1
    return nativeSS
end
local function sampleFor(run, duration)
    local deadline = scenario.Time()+duration
    repeat observe(run); check(scenario.Sleep(100)) until scenario.Time() >= deadline
end
local function groundPoint(run, preferred)
    local nativeSS = observe(run)
    for _, offset in ipairs({preferred, {x=0,y=6}, {x=-6,y=0}, {x=0,y=-6}, {x=6,y=6}}) do
        local goal = {x=nativeSS.position.x+offset.x,y=nativeSS.position.y+offset.y,z=nativeSS.position.z}
        local path = must(run.client.World:Path(nativeSS.position, goal))
        if path.is_available and path.is_complete and not path.is_truncated and path.goal ~= nil
            and distance(nativeSS.position, path.goal.position) > 3 then
            local projection = must(run.client.World:Project(path.goal.position)).projection
            if projection ~= nil and projection.is_available and projection.is_on_screen then return path.goal.position end
        end
    end
    error('no complete visible local ground route; coverage unavailable')
end
local function verifyGround(receipt)
    local click = receipt ~= nil and receipt.gameplay_click or nil
    if receipt == nil or not receipt.is_processed or click == nil
        or not click.is_press_poll_observed or not click.is_release_poll_observed then
        error('input outcome unproven; no replay')
    end
    if not click.is_native_ground_query_success or click.retained_handle ~= 0 then
        error('native input selected an object instead of ground; coverage unavailable')
    end
end
local function setup(name, mission, progression)
    check(scenario.RequireAPI(1))
    local server = must(scenario.StartServer({}))
    local profile = must(server:CreateProfile({name=name,hero_nouns={454054015,2530835539,749013658},chain_progression=progression}))
    local client = must(profile:Launch({client_id=name,launch_mode='normal'}))
    check(client:SetTimeout(60000))
    must(client:Login()); must(client.Scene:Wait(scene.Arsenal))
    must(client.World:Configure({run_seed=104729,is_ambient_population_suppressed=true}))
    must(client.Scene:Set(scene.Navigation))
    local run = {client=client,initial=must(client.Mission:Enter(mission)),sampleCount=0}
    sampleFor(run,2500)
    capture(run,'camera-input-entry')
    return run
end
---Actual native held-left polls, fixed aim, 600/1200ms; releases between trials.
---This does not claim uninterrupted long holds or cursor steering while held.
return function()
    local run = setup('camera-held',24,23)
    local movementCount = 0
    for trial=1,12 do
        if trial == 7 then must(run.client.World:SelectHero(1)); sampleFor(run,2500) end
        local beforeSS = observe(run)
        local point = groundPoint(run,{x=trial%2 == 0 and -6 or 6,y=0})
        local duration = trial%2 == 0 and 1200 or 600
        local receipt, inputError
        local err = scenario.Parallel({
            input=function() receipt,inputError=run.client.World:Hold(point,duration) end,
            camera=function() sampleFor(run,duration+2500) end,
        })
        if err ~= nil or inputError ~= nil then
            error('held input stopped; no replay: ' .. tostring(err or inputError) .. '; manifest=' .. capture(run,'held-input-failed'))
        end
        verifyGround(receipt)
        local afterSS = observe(run)
        if distance(beforeSS.position,afterSS.position)>0.25 then movementCount=movementCount+1 end
        capture(run,'held-trial-' .. tostring(trial))
        check(scenario.Log('Held trial ' .. tostring(trial) .. ' completed; duration=' .. tostring(duration) .. 'ms'))
    end
    if movementCount<6 then error('fewer than six held trials produced movement; coverage incomplete') end
    capture(run,'held-camera-complete')
    check(scenario.Log('Held trials complete; review raw input_move_hold and camera traces. Client retained.'))
end
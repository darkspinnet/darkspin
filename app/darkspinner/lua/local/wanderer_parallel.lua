-- Eight rolling client workers sharing one owned server; real native loads.
local must,check=scenario.Must,scenario.Check
local jobs={
    {stage=14,seed=1203641,label="infinity_3-a"},
    {stage=19,seed=1203641,label="infinity_1-a"},
    {stage=20,seed=1203641,label="infinity_4-a"},
    {stage=21,seed=1203641,label="scaldron_2-a"},
    {stage=23,seed=4417884244153018,label="nocturna_1-b"},
    {stage=22,seed=1203641,label="scaldron_1-a"},
    {stage=18,seed=1203641,label="nocturna_2-a"},
    {stage=18,seed=4417884244153018,label="nocturna_2-b"},
}
local nextJob=1
local server
local function work()
    while nextJob<=#jobs do
        local job=jobs[nextJob]
        nextJob=nextJob+1
        local profile=must(server:CreateProfile({name="map-audit-"..job.label,hero_nouns={1667741389,3618328019,749013658},chain_progression=job.stage-1}))
        local client=must(profile:Launch({client_id="map-audit-"..job.label,launch_mode="normal",is_cinematic_skipped=true,timeout_ms=120000}))
        job.client=client
        check(scenario.Log(string.format("AUDIT_PROCESS client=%s pid=%d instance=%s",client.ID,client.PID,client.InstanceID)))
        local current=must(client:Snapshot())
        if current.pid~=client.PID or current.instance_id~=client.InstanceID or current.user_id~=profile.user_id then error("owned launch identity mismatch") end
        must(client.Diagnostics:WaitForSignal('rea-armed',30000))
        must(client:Login())
        must(client.Scene:Wait(scene.Arsenal))
        check(scenario.Log("AUDIT_MAP_READY "..job.label))
        must(client.World:Configure({run_seed=job.seed}))
        must(client.Scene:Set(scene.Navigation))
        must(client.Mission:Select(job.stage))
        must(client.Mission:Start())
        must(client.Mission:Continue())
        local world=must(client.World:Ready())
        local movie=must(client.Cinematic:Snapshot())
        check(scenario.Log(string.format("AUDIT_PRESENTATION %s mode=%d game=%d active=%s transitioning=%s",job.label,movie.mode,movie.game_mode,tostring(movie.is_active),tostring(movie.is_transitioning))))
        must(client.Diagnostics:WaitForSignal('rea-layout',120000))
        world=must(client.World:Ready())
        job.game_id=world.game_id
        local capture=must(client.Diagnostics:Capture(job.label))
        job.manifest_path=capture.manifest_path
        check(scenario.Log("AUDIT_DATA_READY "..job.label.." "..capture.manifest_path))
        check(client:Terminate())
        check(profile:Logout())
        job.is_complete=true
        check(scenario.Log("AUDIT_SLOT_FREE "..job.label))
    end
end
return function()
    check(scenario.RequireAPI(1))
    server=must(scenario.StartServer({}))
    check(scenario.Log("AUDIT_SERVER "..server.address))
    check(scenario.Parallel({worker1=work,worker2=work,worker3=work,worker4=work,worker5=work,worker6=work,worker7=work,worker8=work}))
    local games={}
    for _,job in ipairs(jobs) do
        if not job.is_complete or job.game_id==nil or games[job.game_id] then error("independent game completion missing") end
        games[job.game_id]=true
    end
    check(scenario.Log("AUDIT_COMPLETE "..tostring(#jobs).." loads; eight rolling workers; one server"))
end

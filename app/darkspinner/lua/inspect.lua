---Read the supervisor's real state without launching any game process.
local must = scenario.Must
local check = scenario.Check

return function()
    check(scenario.RequireAPI(1))
    local session = must(scenario.Snapshot())
    if session.id == nil then
        return "sessionState: missing supervisor identity"
    end
    return nil
end

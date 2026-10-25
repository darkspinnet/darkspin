---@meta

---Live switches for this exact owned client. Introductory movies and in-game
---cinematics are independent; changing one preserves the other. Operations
---return only an error, so call scenario.Check(client.Cinematic:SetSkipEnabled(true)).
---@class CinematicScope
---@field SetSkipEnabled fun(self:CinematicScope,is_enabled:boolean):ScenarioError|nil Controls in-game cinematic skipping.
---@field SetIntroSkipEnabled fun(self:CinematicScope,is_enabled:boolean):ScenarioError|nil Controls introductory-movie skipping.

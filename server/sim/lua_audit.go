package sim

// LuaBuiltinModuleModes reports which constrained compiler preloads a module.
// This is not a claim that arbitrary callbacks from the module are implemented.
func LuaBuiltinModuleModes(name string) []string {
	modes := make([]string, 0, 4)
	if _, isBuiltin := luaAbilityDefinitionBuiltinModule[name]; isBuiltin {
		modes = append(modes, "ability_definition", "ability_activation", "summon_passive_definition")
	}
	if _, isBuiltin := luaJobBuiltinModule[name]; isBuiltin {
		modes = append(modes, "job_allowlisted_preload")
	}
	return modes
}

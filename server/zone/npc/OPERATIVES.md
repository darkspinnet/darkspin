# Operatives

Research source: the installed build's `content.db`, read through `darkrun db` on 2026-09-20. Class descriptions confirm five Operative families:

| Family | Authored noun | Authored behavior |
| --- | --- | --- |
| Omicron | `NomadCyberOne.Noun` | Energy cage and a self-repair drone |
| Gravitic Confiner | `NomadSpacetimeAgent.Noun` | Long-range gravity cage and escape teleport |
| Haunt Strider | `NocturnaSpecialHaunt.Noun` | Stealth, lockdown, and healing nearby allies through stolen health |
| Vaulting Amphiod | `NomadSpecialFour.Noun` | Crystalline cage and poison pools |
| Magmatic Brute | `NomadPets.Noun` | Two summons, one trapping heroes and one breathing fire |

Each family has `_2` and `_3` difficulty variants. The public [enemy catalog](https://darksporegame.fandom.com/wiki/The_Darkspore) and [Omicron description](https://darksporegame.fandom.com/wiki/Omicron) corroborate the names and cooperative rescue role.

## Implemented encounter coverage

The population planner currently selects Omicron and Gravitic Confiner, including their tier variants. One or two selected ordinary encounter locations per map can replace an escort with an Operative while at least two members are connected. This is a requested server spawn policy, not a recovered retail spawn frequency. The budget counts defeated actors too and survives checkpoint restoration through NPC snapshots. Captains, bosses, Mutation Agents, solo missions, and PvP are not replacement candidates.

Both use the authored cage presentation and channel damage. Heroes cannot move, use abilities, switch, or escape through `/follow` while caged. Death, stun, sleep, fear, silence, immunity, target departure, or losing the last free rescuer releases the cage. The last-free-player protection is a server policy to prevent unrecoverable traps. Secondary drone and escape-teleport behavior, and the other three Operative families, are not enabled by this change.

## Recovered ability data

| Ability | Indexed chunk | Range | Windup | Damage tick |
| --- | --- | --- | --- | --- |
| `NomadCyberOne_Cage` | `Abilities/0xB3C8F5CF.lua` | 10 | 1.9 seconds | 15 Technology/Energy every 2 seconds |
| `NomadSpacetimeAgent_Cage` | `Abilities/0x33D80E40.lua` | 12 | 0.8 seconds | 15 Spacetime/Energy every 2 seconds |

Both scripts specify a 1000-second channel ceiling and zero damage coefficient. Retail cage cooldown properties are 6 and 5 seconds respectively; the current scheduler uses the channel tick interval while the target remains caged and repeats the windup after interruption. It does not yet model the separate post-channel cooldown or the authored accumulated-damage escape branch.

Modifier chunks are `Modifiers/0xA2A610FE.lua` (Omicron) and `Modifiers/0xC62A3029.lua` (Gravitic Confiner). Omicron's ability explicitly references modifier GUID `0x04d7ca69`. The modifiers inherit `template_modifier_cage.lua` and select `status_trapped_tc` and `status_trapped_sp` respectively.

No shipped assets were edited. No build, launch, or automated tests were performed.

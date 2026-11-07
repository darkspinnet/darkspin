# Embedded per-zone gameplay rules

Edit `server/runtime/zone-rules.json` in source and rebuild the server to change gameplay tuning. Go embeds this server-owned file into the binary. There is no runtime JSON file, runtime override, client endpoint, or launcher Config tab exposing it.

`default` contains the complete baseline rule entry. `campaign` and `replay` contain optional mission overrides and start empty. An unspecified mission uses `default` in code. When tuning a specific mission, copy the complete default entry into its group under the mission number, then edit the relevant settings. Overrides are complete entries, not partial field merges; omitting an optional feature section in an override disables that feature for that mission.

Mission numbers use full progression. `campaign["1-1"]`, `campaign["7-1"]`, and `campaign["13-1"]` represent different static Onslaught, Invasion, and Apocalypse levels. `replay["1-1"]` is distinct from static 1-1. A replay follows its selected mission number, not the tier-one number of its randomized destination. The `/noreplay` seeded static-map path uses campaign rules. Optional top-level `tutorial` and `arena` entries can similarly override the default.

The baseline starts with 15 ordinary enemies per section, the previous composition and alert rules, and a 500 ms inter-wave delay. Its properties are:

- `enemies_per_section`: ordinary population target per route section, bounded by authored spawn locations. Scripted and boss waves retain separate counts.
- `enemy_population`: agents, roster weighting, ambient elites, and summon/fixture health scaling. An ambient elite chance of 0 disables promotion; positive percentages scale the authored stage chance.
- `ally_alert`: range, range owner, and hop limit. Include this section to apply ally alerts; omit it to disable them.
- `boss_spawning`: opening adds per anchor, initial-mission boss adds, and captain delay. Mission-number rules still choose captain versus destructor.
- `mutation_agent`: horde waves, Mutation Agent eligibility and wave, completion policy, and inter-wave delay. Include the section to permit Mutation Agents when eligible; omit it to leave them disabled with ordinary three-wave horde defaults. `completion` is `agent_defeat` or `all_waves`.

There are no version fields, enable switches, or diagnostic logging switches. Startup validates the default and every supplied override, rejecting unknown properties, invalid mission keys, and invalid settings. It then retires the four old gameplay sections from `server.toml`, preserving unrelated sections such as `part_shop`. Old global gameplay settings no longer override embedded tuning.

Each new zone captures the selected entry. Existing zones, co-op joins, and checkpoint resumes retain their original rules, even across a rebuild. New checkpoints include the resolved rule key, population target, composition, boss, ally-alert, and effective horde policies. Older checkpoints preserve existing saved spawn rules and obtain newly introduced fields through the compatibility path.

Source, formatting, JSON syntax, and diff checks were performed. No build, scenario, or tests were run.

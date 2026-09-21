# Combat reports reviewed on 2026-09-20

Reviewed the ten stable 1.0.3 reports captured on September 19–20 against the current server. No build, test, launcher, or game was run. Conclusions below distinguish received protocol events from what the client rendered.

## Implemented changes

- Boss loot (`20260919T155715.721977600Z`, `20260919T181436.702819300Z`): NPC equipment used source amount 10 with a 0.45 loot scalar, giving bosses the ordinary 4.5% base chance. A defeated map boss now guarantees one equipment drop through the existing shared reservation. Ordinary enemies, obelisks, rarity generation, and ownership rolls are unchanged. This guarantee is a server reward policy, not a claim about an original guaranteed-drop rule recovered from client data.
- Charge continuation: `produceEnemyCharge` previously returned without scheduling anything if most charge families resumed before their readiness timestamp. Their action ownership remained held. All charge families now schedule a retry; the Ghost Charger's authored waiting pose remains intact. This is a confirmed control-flow defect, but the Shade Drifter capture itself continues cycling, so it does not prove this was that report's cause.
- Shade Drifter charge descriptors: imported Lua chunk 359 (`nAbility_NocturnaSpecialDrift_Charge`) specifies 16520: area damage, energy damage, and IgnorePlayerCount. Corrected the server profile from the projectile bit to IgnorePlayerCount. The start, loop, and end animation names already match that chunk.

## Reports requiring further evidence or visual verification

- `20260919T025538.224651300Z`, attacking after death: enemy 355 receives a killing event at client time 550511218, then deals 34.4 damage at 550512468, exactly 1.25 seconds later. The server log identifies its scheduled Lightning Juggernaut death detonation. That captured hit is intentional death-explosion behavior; the capture does not demonstrate repeated corpse attacks.
- `20260919T030502.624284100Z`, boss dies with health remaining: target 548 receives the final 5.29-damage killing event and a zero-health CombatantDataUpdate at 551090000, followed by its death animation. The preceding health was 5.29. No stale positive-health update follows in the capture. The visible health-bar issue is not established as a server health error.
- `20260919T043428.125121900Z`, enemy sometimes takes no damage: the retained combat events are Tree of Life healing for squad objects 1–3; they do not include the reported failed attack. Need an affected enemy and a capture covering that attack.
- `20260919T145057.691195700Z` and `20260919T195842.685642800Z`, excessive damage: neither retained client trace contains CombatEvent messages. No enemy, hit amount, or expected comparison is available to justify a balance change.
- `20260919T150222.609719300Z`, enemy deals no damage: the trace includes enemy attacks for 32 and 80 damage and a final 42-damage killing hit on hero 2 at 13780187. The report context records that hero at zero health. Need a reproduction against a living hero to establish a missed-damage defect.
- `20260919T181153.149674700Z`, Shade Drifter damage/animation: enemy 105 repeatedly sends start/end animations and life-drain hits of 2 or 4 damage about seven seconds apart. No charge-loop animation appears in the retained window. The animation names match imported Lua. Its missing traversal/contact damage still needs navigation and client presentation evidence; the cooldown and descriptor corrections alone do not establish resolution.
- `20260920T040550.635789900Z`, persistent Tree of Life: current code already queues an ObjectDelete to all same-zone clients when a healing producer fails, including its caster. The capture predates that fix and contains no retained combat events. Its specific visual outcome remains unverified.

Unresolved extracted reports remain in `bin/game/logs/bugs/combat-batch-20260920`. Resolved boss-loot extractions are removed; original attachments in Downloads are untouched.

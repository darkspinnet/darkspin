# Changelog

### 2026-09-17

- Update launcher build dependencies from Nano ID 3.3.16 to 3.3.19 and PostCSS 8.5.19 to 8.5.28 to resolve their security advisories.
- Expand stable releases and unstable artifacts from Windows x86 and Linux x64 to Windows amd64/win32, macOS amd64/arm64, and Linux amd64/arm64, with architecture-specific Windows self-updates.
- Fix The Corruptor's stalled movement, missing elemental attacks, and mocking poses; change elemental phase transitions from replaying his spawn to continuing combat in place.
- Restore Enemy Portal visuals and their authored explosion effect.

### 2026-09-16

- Use fixed stable release ZIP filenames so permanent Windows and Linux download links follow the latest release.
- Change stable launcher downloads from a standalone executable to the Windows ZIP for both release assets and self-updates.
- Fix duel lobbies stalling before squad selection by removing malformed character data from initial player updates.
- Generate main artifacts with unstable build numbers and commit labels, permanent download links, and Windows artifact self-updates while keeping GitHub releases exclusive to release.
- Prepare for darkspinnet org public launch

### 2026-09-07

- Change Invincitron's hover drone from remaining at its owner's position to circling above him, with its firing position following the same orbit.
- Restore Nashira's Shadow Panic shriek on her melee/lob attack paths, with timed nearby fear, a fifteen-second cooldown, and normal attacks resuming after the cast.
- Change Nashira illusion deaths from the full boss death animation to a brief duplication-effect disappearance; preserve the real boss's death sequence.

- Fix boss-summoned Exploder Scarabs failing damage and explosion processing because they were incorrectly treated as boss-wave actors.
- Change Laser Tank beams from following heroes to fixed placement anchors, with explicit effect cleanup when firing ends or is interrupted.

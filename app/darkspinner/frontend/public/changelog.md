# Changelog

### 2026-09-17

- Explicitly gate unstable artifact builds on pushes to main, excluding pull requests and other branches.
- Reject RW4 vertex declarations with more than 65,535 elements before allocating or encoding their 16-bit element count.
- Enforce portable signed 32-bit bounds for DSE counts and layout offsets, and calculate Fang mana-cost trace products in double precision.
- Add a security policy directing vulnerability reports to GitHub's private reporting flow.
- Remove overflow-prone Scaleform allocation sizing, guard accumulated instruction offsets, and require HTTPS for authentication cookies with the Secure attribute.
- Reject invalid Unix process IDs, bound firewall interface responses, and validate content offsets, sizes, and narrow integer fields before conversion to prevent overflow and excessive request-driven allocation.
- Reject out-of-range DSE animation event indexes, event counts, and interpolation modes instead of silently truncating them.
- Reject DSE animation counts that exceed the platform integer range before allocation, preventing integer overflow on 32-bit builds.
- Remove unchecked allocation-size arithmetic when parsing Scaleform action arguments to address CodeQL alert #8.
- Fix the false tutorial XP-bar “Capped” warning by sending uncapped accounts with the client's compatible no-cap value instead of zero.
- Default new local and remote Crogenitors to skipping the tutorial, while retaining the option to play it.
- Change tutorial Return to Ship from a forced scene switch to the native Blaze post-game and player-removal exchange, clearing the joined tutorial session and refreshing the profile before ship interaction.
- Split large launcher reports into independently openable ZIPs of at most 25 MB, preserve oversized logs as numbered chunks, and list every attachment in the report dialog and GitHub issue draft.
- Change the in-game Fullscreen checkbox from exclusive fullscreen to the same saved borderless mode as Alt+Enter, keeping native rendering windowed and the options state synchronized.
- Hide the cinematics toggle in both launchers and disable cinematic skipping for normal, automatic, remote, and detached launches.
- Show the running build version in the Darkspinner window title.
- Use the supplied green emblem across both Wails launchers, with dedicated 16-pixel artwork, larger Windows icon sizes, desktop app icons, and frontend favicons.
- Add browser links to create prefilled GitHub bug reports and manage past reports, with a bug-folder shortcut for attaching diagnostic ZIPs.
- Resize the legacy UI and Flash HUD with the render target, notify Flash layout handlers, and apply windowed/borderless transitions before frame updates to avoid stale layouts and black output when returning to windowed mode.
- Let the launcher version label expand to fit unstable build identifiers by shortening the adjacent progress bar.
- Change player dodge and energy resistance ratings from linear scaling to diminishing returns with a 75% avoidance ceiling, and cap combined passive, aura, resistance, and armor mitigation at 90% so stacked defenses cannot grant immunity.
- Match borderless rendering and the saved graphics resolution to the current monitor, and restore the remembered windowed resolution when switching back.
- Apply in-game resolution changes to the window size immediately and retain the selected size when returning from borderless to windowed mode.
- Change Alt+Enter from an unsaved exclusive-fullscreen toggle to a saved windowed/borderless toggle, default to windowed, and restore the previous window placement when leaving borderless.
- Correct client preferences from GameData to DarksporeData across launcher modes, preserve saved settings, and initialize new profiles with valid windowed defaults.
- Restore Enter-to-chat by correcting game-window detection from Game to the shipped Darkspore title.
- Keep launcher self-update checks silent and show update progress only when an update is found, without holding content preparation at zero.
- Fix content preparation failing at tutorialAlias by restoring authored tutorial names in content imports and runtime lookups.
- Add standalone Darkrun Windows amd64 and Linux amd64 ZIPs to stable releases and unstable build artifacts.
- Upgrade Echo from v4.13.3 to v4.15.3 to fix the static-file route middleware bypass vulnerability.
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

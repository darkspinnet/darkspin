# Darkrun with Docker Compose

**Stable setup is not available yet. Use unstable.** You need Linux amd64, Docker Engine with the Compose plugin, and a complete build-103 game installation.

## Download and prepare

[Download the unstable Compose setup ZIP](https://nightly.link/darkspinnet/darkspin/workflows/main-build/main/darkrun-compose-unstable.zip). Extract it into its own directory, such as `/srv/darkspin`. It contains `compose.yaml`, `.env.example`, this `README.md`, and `VERSION`.

```sh
cd /srv/darkspin
cp .env.example .env
mkdir -p darkspin
sudo chown -R 65532:65532 darkspin
```

Edit `.env`:

```dotenv
# Copy to .env beside compose.yaml. Linux amd64; PostgreSQL is required.
# Recreate affected containers after edits; restarting preserves their old environment.
# Explicit bootstrap environment values override saved runtime/darkspin.toml settings.

# Compose project/volume namespace. Keep stable to retain the same database volume.
# Takes effect on the next Compose invocation; never use down -v to preserve data.
COMPOSE_PROJECT_NAME=darkspin
# Infrastructure image reference. Recreate Darkrun after changing/pulling it.
DARKSPIN_BOOTSTRAP_IMAGE=ghcr.io/darkspinnet/darkrun-bootstrap:1
# Required: supply the reachable server IPv4 address; there is no universal default.
# Advertised to players after recreating Darkrun.
DARKSPIN_ADVERTISED_HOST=
# Container listener IPv4 address. Default listens on all container interfaces.
# Recreate Darkrun to change it; this does not restrict host-side published ports.
DARKSPIN_BIND_HOST=0.0.0.0
# Server port number, published on the host for both TCP and UDP. Recreate Darkrun.
DARKSPIN_PORT=42127

# Writable host directory for binaries, settings, cache and appearance files.
# Relative to compose.yaml; prepare permissions before recreating Darkrun.
DARKSPIN_HOME_PATH=./darkspin
# Required: supply a complete build-103 installation with DarksporeBin/ and Data/.
# No universal default. Mounted read-only after recreating Darkrun.
DARKSPIN_GAME_HOST_PATH=

# Player database name, shared by PostgreSQL initialization and Darkrun.
# Initialization only creates it on an empty volume; changes require migration
# of an existing database and recreation of both containers.
POSTGRES_DB=darkspin
# Database role, shared by PostgreSQL initialization and Darkrun.
# Initialization only creates it on an empty volume; changes require updating
# the existing role and recreating both containers.
POSTGRES_USER=darkspin
# Required: supply a long, unique password; there is no shared default.
# Single quotes preserve literal '$'. For an existing volume, change the role's
# password separately, then recreate both containers to use the new credential.
POSTGRES_PASSWORD=

# Enable scheduled backups and updates; manual updates still work when false.
# Applied when Darkrun is recreated; overrides saved [bootstrap] settings.
DARKSPIN_IS_UPDATE_ENABLED=true
# UTC cron schedule for starting the maintenance warning/drain period.
# Applied when Darkrun is recreated; overrides the saved schedule.
DARKSPIN_UPDATE_CRON="30 3 * * *"
# Maximum minutes of player warnings/draining before scheduled shutdown.
# Applied when Darkrun is recreated; shutdown can proceed earlier after draining.
DARKSPIN_UPDATE_WARNING_MINUTES=30
# Maximum seconds for candidate server readiness. Applied when Darkrun is recreated.
DARKSPIN_UPDATE_START_TIMEOUT_SECONDS=1800
# Days to retain completed backup snapshots. Applied when Darkrun is recreated;
# pruning occurs after successful candidate validation, not when this file is edited.
DARKSPIN_BACKUP_RETENTION_DAYS=7
# HTTPS manifest/manifest-ZIP URL for initial install and subsequent updates.
# Applied when Darkrun is recreated; overrides the saved update source.
DARKSPIN_UPDATE_MANIFEST_URL=https://nightly.link/darkspinnet/darkspin/workflows/main-build/main/darkrun-update-linux-amd64-unstable.zip
```

Supply the IPv4 players can reach, the absolute host path to the build-103 game installation, and a long, unique database password in the three empty assignments. They have no safe universal defaults. Single quotes preserve literal `$` characters in `.env`. Restrict access to `.env`, which contains credentials. Compose rejects an empty or omitted `POSTGRES_PASSWORD`.

The game root must contain `DarksporeBin/` and `Data/`. UID/GID 65532 must be able to read and traverse it. Allow the configured game port for **both TCP and UDP**.

The block above is the complete supported operator input inventory for this Compose deployment. Internal container paths and PostgreSQL service wiring stay fixed in `compose.yaml`; native-host PostgreSQL inputs below are not additional Compose `.env` settings. The published setup uses the listed defaults even when those defaulted assignments are empty or omitted. Non-empty shell variables supplied to Compose can override `.env` inputs; avoid stale shell overrides when changing the file.

Editing `.env` requires recreating the affected container to replace its environment. After a Darkrun-only change, run `docker compose up -d --no-build --force-recreate darkrun`. An ordinary `docker compose restart darkrun` keeps the existing environment. Changes shared with PostgreSQL require recreating both affected services with `docker compose up -d --no-build --force-recreate`; first perform any required existing database/role migration described in the comments. Keep the project name and persistence paths stable unless deliberately migrating their data.

On bootstrap startup, explicit command-line flags take precedence over non-empty environment values, which take precedence over saved TOML settings and application defaults. Compose supplies the bootstrap defaults explicitly, so its active default assignments override previously saved values rather than preserving them. The supervisor loads these settings when it starts; replacing the child server during an update does not reload the container environment or supervisor settings.

## Start and stop

```sh
docker compose pull
docker compose up -d --no-build darkrun
docker compose logs -f postgres darkrun
docker compose ps
```

On first launch, the Darkrun entry point downloads a versioned manifest and its exact binary artifact, verifies its SHA-256 and build identity, and installs it in the runtime mount. It then runs `darkrun bootstrap`, which starts the dedicated server with `/darkspin/runtime/darkspin.toml`. Subsequent launches recover pending updates before serving players. The server creates its settings, authentication secret, content cache, and missing player tables before accepting players.

Dedicated server and supervisor output is saved to `/darkspin/runtime/darkspin/logs/logs/YYYY-MM-DD.txt` using UTC dates, rather than normal Docker console output. Direct `darkrun server --dedicated` mode uses the same path beneath its configured runtime. Existing files are appended across restarts, and rotation happens at midnight UTC even while idle. On the host, these files are beneath `${DARKSPIN_HOME_PATH:-./darkspin}/runtime/darkspin/logs/logs`; use Admin → Diagnostic → Log to download them, or follow the current file directly. Docker logs still show the database, entry-point failures, and emergency output if file logging fails.

Retention runs at startup and midnight UTC. Daily logs keep the current UTC date and the six preceding dates. Regular files in `darkspin/logs/traces` and `darkspin/bugs`, including snapshots and reports, are deleted after seven days by modification time, and expired empty subdirectories are removed. The active protocol trace is protected until its writer closes. Cleanup stays inside these diagnostic directories and does not follow symbolic links or delete settings, saves, databases, or backups. Backup retention remains separate.

Connect through Darkspinner's Remote Servers account flow using the advertised address and port. This prototype uses username/password login; HTTPS and OAuth are not configured. Darkrun's health check probes its maintenance and main server endpoints; it does not establish that a multiplayer session works.

Darkspinner shows the full remote server version. Remote login requires client and server versions to share the same major and minor numbers: `1.2.1-unstable` and `1.2.5` work in either direction, while `1.3` is incompatible with `1.2`. The server enforces this at login. Older remote clients that do not send their version must upgrade once to a build with this protocol.

```sh
docker compose stop
```

This stops both services while preserving their data. `docker compose stop darkrun` stops only the game server and leaves PostgreSQL running. Normal `docker compose down` removes the containers and network while preserving the database volume.

## PostgreSQL player storage

`POSTGRES_DB`, `POSTGRES_USER`, and required `POSTGRES_PASSWORD` are the single source for both database initialization and Darkrun's `PGDATABASE`, `PGUSER`, and `PGPASSWORD`. Compose sets `PGHOST=postgres`, port 5432, and `PGSSLMODE=disable` for its private network. No second Compose variant or TOML database flag is needed.

For a native host deployment with an existing database, set `DARKSPIN_POSTGRES_URL` or `PGHOST`, `PGDATABASE`, and `PGUSER`, plus authentication/TLS settings, and run `darkrun server --dedicated --config PATH/darkspin.toml`. The database and role must already exist with permission to create tables. URL passwords must be URL-encoded. Darkrun requires these settings even outside Compose. Desktop local play remains on SQLite.

For administration inside Compose, the running container already has the correct connection environment:

```sh
docker compose exec darkrun /darkspin/bin/darkrun db user get player_name --config /darkspin/runtime/darkspin.toml
```

Native Linux hosts can use `darkrun bootstrap --path PATH` with the same environment and PostgreSQL client tools on `PATH`. Bootstrap verifies it can connect to the `postgres` maintenance database and that its role is a superuser or owns the player database with `CREATEDB` permission. Do not use `postgres`, `template0`, or `template1` as the player database. The standard Compose role already meets these requirements. Multiple database hosts, service files, `PGHOSTADDR`, and unsupported URL options are rejected to avoid differences between the server and backup tools' connection targets.

## Control panel administrator access

Remote login and control-panel requests currently use plain HTTP, including passwords and administrator bearer tokens. Restrict this prototype to an encrypted VPN or tunnel with trusted endpoints; account roles and IP restrictions do not encrypt traffic. Native HTTPS support remains outstanding.

In-game developer commands, including currency grants, level changes, spawning, forced outcomes, and `/noreplay`, require current administrator access (status greater than `250`) and the account's IP restriction. The server checks these permissions before dispatching the command. Ordinary chat, bug reporting, player information, following, and exiting remain available to players.

Open the selected local or remote Crogenitor's Actions → Control Panel in Darkspinner. Statistics and any authorized administration pages are inside that panel. Local profiles use the launcher's owned server without a password prompt and retain their stored access role; this does not grant administrator status. The panel has no Account tab, account-details page, or account/session-management controls.

The remote launcher control panel provides authenticated server information, activity graphs, and administrator controls. Crogenitor deletion remains available through the launcher's Actions menu and follows the 120-day recovery policy below.

Administrators can use Admin → Diagnostic → Log, Bug, or Snapshot to list, download, and delete saved server diagnostics. The lists cover regular diagnostic files beneath the server runtime's `darkspin/logs` and `darkspin/bugs` directories, including nested traces and split ZIP reports. Snapshot captures and completed snapshot ZIPs have their own tab; every list is ordered by modification date, newest first. Other directories, symlinks, databases, and configuration files are excluded. Dedicated server and supervisor output is recorded in the daily Log files; entry-point output and database container logs remain outside this list. Listings are paginated and bounded to 10,000 scanned entries per source; downloads are limited to 64 MiB per file.

Each download and deletion rechecks current administrator access and records its file reference and outcome in Audit. Deletion requires an in-window confirmation and permanently deletes the selected server file. Downloads save beneath the launcher's local `darkspin/bugs/server-downloads` directory; the panel displays the saved path and provides a folder button. No file contents or credentials are added to audit records.

Administrators can edit another account’s numeric status and Allow IP in Admin → User → Edit Access. Allow IP uses the existing `account_access.ip_limit` field and accepts one IP address or CIDR. Status and IP save together with an administration reason and an audit event; status 250 or higher requires a nonempty limit. Administrators cannot edit their own access, change accounts above their own status, or grant a status above their own. A saved change revokes panel/launch credentials and disconnects existing gameplay; trusted `darkrun db` commands remain available for operator recovery.

Administrators can search and paginate active accounts, kick players, ban/unban ordinary accounts with a reason, and inspect administration history. Bans persist before disconnection, block new admissions immediately, and revoke existing panel/launch credentials. A failed disconnection is reported as pending and retried through the server's scheduler; unbanning stops subsequent ban retries. Kick acceptance and its completed/failed outcome are recorded separately, and interrupted kicks are never automatically replayed against a later session. Remote moderation cannot ban the initiating administrator or another administrator. Operator-only recovery remains available through `darkrun db`.

Administration history supports actor account/type, target account, action, outcome, operation ID, and time filters. Text filters use exact codes, such as `account.kick`, `maintenance.update`, `accepted`, `completed`, and `failed`. Time inputs use your device's local timezone; event times display in UTC. The start is inclusive and the end exclusive. Load More retains the last applied filters until you apply a new search. Select an operation ID to clear unrelated filters and inspect all its recorded stages. Terminal maintenance events can use the `system` actor rather than the initiating account, so filtering only that account can hide the outcome. An accepted record without a terminal result means the outcome is unknown.

CLI maintenance warnings, requests, and cancellations record `local_operator` acceptance; scheduled requests use `system`. Bootstrap writes terminal outcomes even when a replacement child cannot remain running, retaining its result for later retry if the audit database is unavailable. Repeated acceptance/completion for the same operation does not add duplicate stages. Operations created before acceptance auditing are not backfilled.

Statistics includes daily account-population snapshots, creation-date distribution of remaining accounts, sampled online peaks, distinct observed accounts per bucket, and prospective first recorded gameplay admissions. Creation-date distributions exclude deleted accounts and are not an immutable signup ledger; first recorded admissions can include existing players returning after collection began. My Progress adds observed-presence and daily saved PvE/PvP counter-change graphs. Presence is not playtime, and missing/reset counters are not inferred as zero. Graph explanations are available through circled question-mark tooltips.

Dedicated servers also store distinct accepted game-login IPs in `user_login_ip`. Each `(user_id, ip_address)` row has `first_seen_at`, `last_seen_at` (UTC Unix seconds), and `login_count`. Repeated authentication on the same physical connection/account counts once; a reconnect counts again. Addresses come from the TCP peer, are normalized for IPv4/IPv6, and omit ports/zones. Failed authentication and panel sign-ins do not create records. IP writes have a two-second bound; failures are logged without blocking gameplay and may retry on later accepted authentication. The player-facing panel does not return these records. Soft deletion preserves them; permanent account purge deletes them through the account foreign key. Local desktop/LAN servers do not enable this collection.

Dedicated administrators can select a user and choose **Review Login History**. Every page requires a reason and commits an `account.loginHistory.read` audit record before returning addresses. Date ranges select overlapping first/last observation spans; they do not establish individual logins within that interval. Counts are retained totals. Paging is bounded and ordered by address; review data is never included in public/player statistics or cached by the launcher.

Inspect a dedicated account's recorded IPs through the existing operator command, replacing `42` with its immutable account ID:

```sh
docker compose exec darkrun /darkspin/bin/darkrun db user_login_ip get user_id=42 --config /darkspin/runtime/darkspin.toml
```

On a dedicated server, sign in through a remote Crogenitor and open Control Panel → Administration → Server to edit the server name, description, welcome message, human-readable rules, and HTTP/HTTPS contact link. Editing requires an unbanned account with persisted status strictly greater than `250`. These fields are stored in the dedicated profile database in the singleton `server_identity` row; they start empty and are not imported from TOML. They are not available in local Config or on local servers. Changes apply without restarting, use revision checks to reject conflicting edits, and are audited in administration history. Welcome text appears in the launcher and is queued as plain system chat after successful game persona login, once per connection. Edits affect the next connection rather than rebroadcasting to existing sessions. Chat delivery normalizes whitespace, neutralizes markup delimiters, and splits long text into bounded lines. A delivery failure does not reject login. Custom image upload remains pending.

Administrators can broadcast a bounded plain-text announcement through existing game system chat. The launcher retains the latest 20 announcements for up to one hour in server memory; restarting clears that feed. A ten-second server-wide cooldown applies. Delivery reports distinguish partial/failed delivery and audit failures instead of claiming that every player received the message. Accepted operations and terminal outcomes are recorded in administration history.

The health page measures database connectivity and buffered profile persistence, including pending accounts and unresolved failures per account. A successful save for another account does not clear an unresolved failure. Save observations cover the current process; no successful save, or one older than five minutes, leaves persistence health unknown. Administrators also see timestamped total and available runtime storage bytes on Windows/Linux; failed or unsupported observations remain unknown, while known zero available space degrades health. These figures describe the runtime filesystem and its caller-available capacity, not write permission, backup storage on another mount, or an external PostgreSQL host.

The backup section reports recorded attempts/failures and the last completed bootstrap snapshot's ID, build, start/completion times, and age. Observations persist in `runtime/backup-observation.json` and apply to new snapshot attempts after deploying the updated supervisor. A recorded failure degrades health and preserves the previous completed snapshot; an interrupted recorded attempt or missing/invalid metadata leaves its outcome unknown. Observation-write failures are logged without changing whether the real backup succeeded, so displayed records do not establish the outcome of unrecorded later attempts. The panel never starts a backup, scans archives, or claims that a recorded snapshot still exists or has passed a restore rehearsal.

Administrators can hold gameplay admissions, restart the installed server, or request an update from the Maintenance page. Hold works on standalone servers; restart and update require bootstrap management. The default 30-minute warning is a maximum drain interval: maintenance proceeds earlier when existing matches finish, or cleans up remaining sessions at the deadline. New gameplay sessions and matches are rejected server-side, including previously issued launch credentials, while panel sign-in remains available. Arena inter-round sessions conservatively remain active until players leave or the deadline is reached. A hold keeps admissions closed until explicitly cancelled.

Cancellation targets the exact operation and cannot interrupt shutdown or forced session cleanup already in progress. Accepted requests are not completed operations: after a restart, sign in again to read the correlated supervisor outcome. Restart uses the installed binary without backup or update checks; update retains the existing stopped-server backup, verification, validation, and rollback lifecycle. Failed updates distinguish recovery of the installed server from an actual rollback. Remote acceptance and terminal outcomes appear in administration history.

Admission intent persists in `runtime/darkspin/maintenance-state.json`; bootstrap progress and outcomes persist in `runtime/maintenance-result.json`. A crash does not reopen a maintenance hold. A restored shutdown without a matching supervisor outcome becomes failed and stays closed, allowing explicit administrator cancellation after recovery. A matching supervisor operation still starting or validating must finish before cancellation can reopen admissions. Do not edit these operational records to bypass recovery.

The account page lists your active launcher panel sessions with creation, last-use, and expiry times, marking the current session. Last-use includes background panel refreshes. Confirm your current password to revoke a selected session or all other sessions; revoking the current session requires signing in again. Revocation ends panel access without disconnecting games already admitted or changing credentials issued by the separate desktop launch flow. The server stores only token digests and independent random management IDs; session records stay in memory and expire after 30 minutes or a server restart. Persistent session storage, device labels, and recent sign-in history are not implemented yet.

Admin → Manage shows the server status, update check, operation controls, and health details. Manage is selected when opening Admin. Check for Updates reads and validates candidate metadata from the configured bootstrap update source and compares its checksum with the running executable; it does not download an executable, make a backup, or enter maintenance. A different checksum is an available candidate, including a patch or same-version rebuild, rather than a claim that semantic versions are ordered. Installing still uses Update with Backup in Manage.

Update checks share one in-flight request and a one-minute cooldown across administrators. Refresh reads the cached observation without contacting the source; failures leave availability unknown. Observations reset on server restart. Metadata checks also work for standalone/all-in-one hosts using their existing bootstrap manifest configuration. Managed hosts use the supervisor’s effective source in preference to the on-disk setting. Missing or unsupported sources show an explicit unsupported status, and the check control remains visible. Installation support remains separate from metadata checking. The page labels the configured source without exposing its URL or assuming a stable/unstable release channel.

Dashboard shows activity graphs for the last hour, 24 hours, seven days, 30 days, or the last year (rolling 365 days). Online counts are sampled on startup and UTC hour boundaries. Week-or-shorter graphs show hourly points; longer ranges show daily averages. Brief peaks between samples may be missed; same-hour retries retain the highest recorded count.

Once per Monday-based UTC week, the first observation or startup packs complete days older than seven days into durable daily points. A saved completion marker allows catch-up after downtime; packing and removal of source hours share one transaction. With weekly cleanup, physical hourly data can remain for roughly seven to fourteen days, while the most recent week stays available at hourly resolution. Longer graphs also aggregate recent unpacked hours into daily points. Daily rollups preserve observed peaks, coverage, and distinct-player membership. Existing minute observations are migrated into hourly records during schema initialization. New-account counts use account records independently of online-count sampling; average levels have their own daily snapshot schedule.

Player State Distribution uses those hourly/startup observations to assign each online human account one primary state: confirmed active PvE or PvP first, then matchmaking, idle/pregame/postgame lobby, or Other when game state cannot be confirmed. Server bots are excluded. Stacked columns show the whole observation; restarting within an hour replaces that hour's entire distribution instead of combining per-state peaks. Month/year graphs average recorded hourly counts with the same denominator for every state. These sampled counts are not player-hours or exact live populations and can differ from the separate peak-online graph. Missing hours remain unknown. Hourly history is packed weekly after seven days, daily history is retained for 370 days, and reads share the existing hourly cache. State history starts at its own collection boundary and is never inferred from old online totals.

Queue history samples PvE (combining difficulties), ranked/unranked 1v1, and ranked/unranked 2v2 at startup and UTC hour boundaries. Each human party member contributes once. Three graphs show queued-player count, mean active waiting age, and the oldest observed wait. A known empty queue has zero players and unknown waiting age. Longer ranges show daily mean queue sizes, waiting ages weighted by queued-player observations, and the greatest sampled waiting age. Long waits may appear in multiple observations, so this does not measure average time to find a match. Cancellations and successful formations are not inferred from snapshots. Each hour is replaced coherently on same-hour restarts; weekly packing preserves count sums, age sums, and observation counts in the same transaction. Daily history lasts 370 days, begins at a separate collection boundary, and shares the existing hourly cache without additional panel polling.

Activity collection begins after deploying this version and persists in the player database across restarts. Observations are retained for 370 days. The yearly graph uses daily buckets; existing gaps or already expired observations cannot be recovered. Missing collection periods remain gaps rather than zero players, and incomplete coverage is labelled. New-account counts use recorded creation timestamps independently of activity coverage and exclude currently deleted accounts. Average level uses daily snapshots of persisted account levels and can lag buffered gameplay saves. No prior activity is inferred from a last-login timestamp. Players receive aggregate metrics only; individual presence records are not exposed by the panel. Use the separate administrator activity history for recorded administration actions and their outcomes.

Average Crogenitor level is captured once per UTC day from saved non-deleted accounts. The hourly collector records it at midnight, or on startup/next observation if today has no snapshot. The first observation stays unchanged for that day. Daily history lasts up to 370 days, does not reconstruct offline days, and treats an empty population as unknown.

Campaign progression follows the same daily UTC policy and measures saved highest-completed campaign indexes. Zero means no campaign completed. The first snapshot each day is preserved across restarts, missed offline days are not filled, and an empty population remains unknown. Daily progression history is retained for up to 370 days.

Dashboard queries run on demand when opened and share server-side cached results across clients. Daily population, level, progression, and campaign graphs are cached per range until midnight UTC; online, signup, activity, and DNA results until the next UTC hour. Concurrent requests reuse the same calculation. Caches cover the five supported ranges and reset on restart.

Darkspinner caches selected server/account responses until the server-provided refresh time. Revisiting Dashboard or pressing Refresh uses valid cached results. Panel loading waits for Actions → Control Panel, requires a matching authenticated session, and changes cache ownership when switching server/account. Maintenance progress and health retain their operational refresh behavior.

Dashboard offers Server and My Progress graph views for local and remote Crogenitors. Personal graphs include DNA earnings, daily level/progression, campaign completions, and mission participation. Daily personal snapshots store only level and progression. Personal history retains up to 370 days without reconstructing earlier activity; per-account records are removed on permanent account purge. Observed presence hours and completed-match PvP round time remain available in administrator reviews.

Administrators can choose Review Metrics in the Users page, select a range, and enter a reason before loading a player's history. Every server review checks current administrator authorization and writes a completed `account.metrics.read` audit entry with actor, target, range, and reason before returning data. If auditing fails, no metrics are returned. Cached data still goes through these checks; admin review does not automatically poll or replay requests. These observations support manual investigation and are not automatic botting verdicts. Ordinary players can only request their own history, and exact IP records are not included in metric responses.

DNA Earned shows the selected range's total and an hourly graph for week-or-shorter ranges, with daily sums for longer ranges. It includes campaign pickups, PvP rewards, positive tutorial awards, and vendor sales; starting balances, developer grants, spending, and editor refunds are excluded. These are gross earnings rather than net balance changes. Earnings counters and their aggregate deltas commit with the full profile save, so retrying a save cannot count it twice. UTC buckets follow the durable save hour; buffered rewards can be recorded later than the moment they were awarded. DNA has its own collection-start timestamp, follows weekly hourly-to-daily compaction and 370-day retention, and does not reconstruct earlier earnings from balances. Missing observations are shown as unknown rather than zero.

Daily signup history counts successful new registrations transactionally and preserves anonymous totals after account deletion. Imports and copied accounts do not count as registrations. Collection begins with this version; earlier registrations are not reconstructed. This differs from the creation-date inventory of accounts still stored on the server.

Personal level and progression graphs compare compatible UTC-day observations against server averages. Administrator PvP round-time graphs aggregate only match counts and active-round time from completed best-of-three receipts. Partial rounds and unfinished matches do not contribute. Lifetime combat counters, outcome/rank/streak summaries, kills/deaths, damage, and healing are not collected as separate dashboard metrics; gameplay statistics and source match receipts remain intact.

Daily Campaign Completions graphs show all successful player completions alongside their repeat subset, for both the server and the authenticated player. Counts commit with the successful completion receipt and profile rewards; duplicate receipt delivery does not add another run. Four co-op players completing a level contribute four player completions. The graphs have their own collection boundary, refresh daily, and leave days without recorded completions as gaps. Anonymous server totals survive profile purging; personal records follow account deletion retention. They do not count starts, failures, server bots, PvP, or infer earlier activity from saved progression.

Most Completed Campaign Levels shows up to ten selected campaign levels for the same server or personal date range. Total completion bars include first and repeat completions; the repeat overlay is a subset of that total. Counts sort descending, with ties ordered by campaign number and then level number. Campaign and level remain separate integers displayed as `1-1`; randomized maps share the player's selected level group. This measures selected-level completion popularity rather than actual-map or difficulty usage. Anonymous daily per-level totals commit in the completion transaction and survive profile purging. Personal receipts use the same new collection boundary, and older receipts are not backfilled because they cannot reconstruct anonymous totals for accounts already purged. Queries use containing UTC days, the existing daily cache, and 370-day retention. No account identities are exposed in server rankings.

Mission Participation records a campaign player's first accepted dungeon-start handshake using the account and checkpoint-stable completion identity. It stores the original start time, selected campaign/level, map, and server-bound difficulty. Duplicate handshakes, reconnects, and checkpoint continuations preserve that start. Successful saved completion receipts resolve only matching recorded starts in the same profile transaction; a success without a recorded start does not invent one. Starts are grouped by UTC start day and linked successes by completion day, so subtracting these series is not a failure count or completion rate. Unresolved Mission Starts shows starts from each day without a linked success as of the daily read; those values can decrease when a continuation succeeds later. Unresolved does not establish defeat, abandonment, or current online status.

This participation identity can span a wipe and checkpoint replay. Permanent failure/retry outcomes and measured active playtime require additional lifecycle ownership and remain unfinished. Mission-start collection is best effort after acceptance and does not reject gameplay if observation fails. Server day aggregates are anonymous and survive profile purging; personal records follow account retention. PvP, tutorials, and server bots are excluded. The new collection boundary is prospective, reads share daily caches, and history is retained for 370 days. These timestamps are not a measurement of active mission time.

Backup health observes the actual backup filesystem and the last recorded snapshot's required files and expected sizes. A creation-time checksum record is distinguished from a fresh integrity check. Last Restore shows the last recorded actual rollback attempt, its backup identity, timestamps, and a safe failure status. Restoration is recorded separately from healthy startup: healthy recovery requires the matching restored binary to pass three readiness checks in its first eligible startup. A supervisor interruption or failed first startup conservatively leaves restoration without healthy certification. Metadata-write failures are logged and do not obstruct rollback. Missing, invalid, or unfinished observations remain unknown, and failed restores degrade health.

Restore Verification shows recovered-and-healthy evidence only when that observed recovery identifies the currently displayed backup. This is historical evidence for one actual restore, not a guarantee about the current archive contents or every retained snapshot. Health reads never trigger a backup or restoration and use the existing 30-second metadata cache. Detailed session/mission histories remain unfinished. The panel uses the existing server listener; it does not open another port. Its HTTP transport remains unencrypted, so use a trusted network or a separately secured transport when transmitting credentials.

Under **Admin → User**, **Timeout** blocks a Crogenitor for a chosen duration in minutes, hours, or days, from one minute to 365 days. It revokes existing credentials, disconnects active gameplay, and records the reason and expiry in Audit. Restrictions expire automatically, including across restarts; **End Timeout** lifts them early. Permanent bans remain separate, and administrators cannot time out themselves or another administrator.

Accounts start with status `0`. Status strictly greater than `250` grants panel administrator access; `250` does not. Accounts at status **250 or above** require a matching `ip_limit` to sign in from a non-loopback peer. An empty limit locks those accounts out of non-loopback authentication. Actual socket peers in IPv4 loopback (`127.0.0.0/8`) or IPv6 loopback (`::1`) are exempt from the IP limit, but still require valid credentials and normal account authorization. A ban or active timeout overrides administrator status. Inspect the account through the existing database getter:

```sh
docker compose exec darkrun /darkspin/bin/darkrun db user get login_name=PlayerName --config /darkspin/runtime/darkspin.toml
```

Dedicated PostgreSQL status changes can run while Darkrun, bootstrap, and PostgreSQL stay online. Use an exact login name or numeric account ID:

```sh
docker compose exec darkrun /darkspin/bin/darkrun db user set status 251 ip_limit 203.0.113.5 where login_name=PlayerName --config /darkspin/runtime/darkspin.toml
```

Replace the example address with the player's real address as seen by the server. The command sets status and limit atomically for exactly one active account, with one credential-revision change and audited before/after values. `ip_limit` accepts one numeric IPv4/IPv6 address or CIDR, such as `203.0.113.0/24`; it does not resolve hostnames. It uses the socket peer and ignores forwarded headers. A proxy therefore appears as its own peer address. Docker port forwarding may present a bridge address even when the client connects to `127.0.0.1`; that non-loopback peer still needs a matching limit. A loopback proxy also makes its clients appear local, so do not use one to expose unrestricted privileged authentication. There is no automatic enrollment.

Change a limit without changing status using `db user set ip_limit 203.0.113.5 where login_name=PlayerName`. Clear it explicitly with `ip_limit none`; that locks out status-250-or-higher accounts from non-loopback peers until a matching limit is configured or their status is lowered. Status-only updates preserve the existing limit. Accounts below 250 are unaffected by the IP limit. Use status `0` to remove administrator access.

Status and limit changes advance the credential revision, so existing panel sessions and unused launch credentials fail their next authorization check; sign in again to obtain updated access. Active Blaze and game HTTP requests also recheck current IP policy, while gameplay UDP checks it on admission/re-admission. The command preserves passwords, progress, and bans. This dedicated path uses the existing PostgreSQL schema without starting another server, opening another listener, or running profile migrations. Direct writes to the access and administration-history tables through `db set` are rejected. Trusted local database commands remain available to repair a mistaken limit.

For native dedicated deployments, use `darkrun db user set status 251 ip_limit 203.0.113.5 where id=42 --config PATH/darkspin.toml` with the same PostgreSQL environment as the server; it can remain online. Local SQLite servers must still be stopped so the command can acquire their runtime lock. Explicit `--config` takes precedence over the Docker configuration defaults from `DARKSPIN_CONFIG_PATH` or `DARKSPIN_BOOTSTRAP_PATH`.

## Undelete a profile

Profile deletion blocks login, hides the account, and adds `_`, `__`, or another available underscore suffix to its names so the original name can be reused. Account data remains for **120 days from deletion**. On startup, the server permanently removes deleted accounts older than that cutoff. Recovery is an administrator-only Darkrun command for dedicated servers; there is no launcher or local-server undelete option.

Stop Darkrun before recovery and leave PostgreSQL running. From the directory containing `compose.yaml` and `.env`, list retained deleted accounts:

```sh
docker compose stop darkrun
docker compose up -d postgres
docker compose run --rm --no-deps --entrypoint /darkspin/bin/darkrun darkrun db user get trmt_id=1 --config /darkspin/runtime/darkspin.toml
```

Find the account's numeric `id` in the output. Recover that exact account, replacing `42` below with its ID:

```sh
docker compose run --rm --no-deps --entrypoint /darkspin/bin/darkrun darkrun db user undelete 42 --config /darkspin/runtime/darkspin.toml
```

The command restores the saved progress and original names. If another account has reclaimed the login, recovery fails without overwriting it. Supply an available replacement name instead:

```sh
docker compose run --rm --no-deps --entrypoint /darkspin/bin/darkrun darkrun db user undelete 42 RecoveredName --config /darkspin/runtime/darkspin.toml
```

To give a deleted account the name held by an active replacement account, swap their login and display names first. The first ID must be deleted and the second active. For example, if deleted account `42` is named `Alice_` and active account `84` is named `Alice`, this gives account `42` the name `Alice` and account `84` the name `Alice_`:

```sh
docker compose run --rm --no-deps --entrypoint /darkspin/bin/darkrun darkrun db user swap 42 84 --config /darkspin/runtime/darkspin.toml
docker compose run --rm --no-deps --entrypoint /darkspin/bin/darkrun darkrun db user undelete 42 --config /darkspin/runtime/darkspin.toml
```

Swap changes names atomically and prints the resulting logins. Account IDs, passwords, progress, deletion flags, and retention dates stay with their existing accounts. The first account remains deleted until the separate `undelete` command succeeds. Keep Darkrun stopped throughout both commands.

After successful recovery, start Darkrun again:

```sh
docker compose up -d --no-build darkrun
```

The one-off commands inherit the service's PostgreSQL environment and runtime mount. Overriding the entry point runs only the requested database command, without starting bootstrap or triggering startup cleanup. If an account has reached 120 days but still exists, recover it before starting the server again. Undelete cannot recover an account that startup cleanup has already permanently removed; that requires a separate backup recovery.

## Persistent layout

**`docker compose down -v` or removing the volume deletes the database.** Persistence is not a backup. PostgreSQL major upgrades require a database upgrade or dump/restore; do not merely change the image's major version.

The separate `DARKSPIN_HOME_PATH` directory is mounted writable at `/darkspin`:

```text
darkspin/
  bin/darkrun
  runtime/
    darkspin.toml
    server.toml
    darkspin/
      saves/      # appearance files; old SQLite saves may remain
      cache/      # SQLite content.db and generated web content
      logs/
        logs/YYYY-MM-DD.txt
        traces/
      bugs/
  backup/
    YYYY-MM-DD/
      <timestamp-and-build>/
        postgres.dump
        snapshot.json
        bin/darkrun
        runtime/  # settings and saves from the same stopped server
  firstboot.json  # durable update/recovery state, when present
```

Scheduled snapshots include PostgreSQL alongside authentication secrets and uploaded appearance files. Keep an independent copy of completed backups outside this host. Copying only `runtime/` does not capture PostgreSQL player data, and a dump is not continuous point-in-time recovery. Keep the game mount read-only and never point multiple servers at the same runtime.

## Scheduled backups, updates and recovery

By default, maintenance warnings start daily at **03:30 UTC**, with a **30-minute maximum drain interval**. Shutdown proceeds earlier when active matches finish. The active `.env` settings and Compose defaults own these `[bootstrap]` values over saved `runtime/darkspin.toml` settings. Changing that TOML through a separate local Config interface does not override the container environment; manage this deployment through `.env`. The remote panel only observes update state and requests maintenance; it does not edit these deployment settings. The defaults allow 30 minutes for candidate startup and retain completed backups for seven days. Set `DARKSPIN_IS_UPDATE_ENABLED=false` to disable scheduled maintenance.

Each scheduled clean shutdown follows this sequence, even when no new binary is available:

1. Darkrun disconnects players, flushes pending writes and checkpoints, closes its database connections, and exits. Bootstrap acquires the runtime lock. PostgreSQL remains running.
2. Bootstrap copies the installed binary, settings, and appearance/save files, then runs `pg_dump --format=custom` against the same configured player database. The compressed `postgres.dump` includes schema, rows, and sequences. It does not include cluster-wide roles or passwords.
3. The dump is flushed to disk, its archive table of contents is checked with `pg_restore --list`, and its SHA-256 and destination identity are recorded in `snapshot.json`. Only then is the snapshot marked complete. A dump failure prevents installation of an update; the existing server can restart without accepting a partial snapshot as a backup.
4. Bootstrap fetches and verifies an available candidate binary. Its durable `firstboot.json` marker identifies the matching backup before installation. Three readiness probes commit the candidate; only completed backups outside the retention window are pruned.
5. A failed candidate or interrupted first boot restores the database with `pg_restore --clean --if-exists --create --exit-on-error`, then restores its matching binary and runtime files. Recreating the player database removes any tables added by the failed candidate. The archive hash and configured destination must match before restoration. Failed file state is retained for diagnostics; failed PostgreSQL state is replaced by the snapshot.

Restore uses the `postgres` maintenance database so it can retry after interruption even if the player database was already dropped. It is not a single transaction: the recovery marker remains until all database and file restoration succeeds, and the server stays stopped on failure. Only the configured game database is recreated; the PostgreSQL service and cluster remain running. Do not connect another server or writer to that database during maintenance or recovery. Changing the host, port, or database while recovery is pending is rejected; use the original destination to recover first.

Dump and restore commands each have a 30-minute limit and are cancelled on supervisor shutdown. Ordinary container stops do not create a scheduled snapshot. `pg_restore --list` checks archive readability, not a full restore rehearsal. Keep independent backups for failures discovered after a candidate has already been committed healthy.

## Request an update from Docker

Request a self-update through the running server's authenticated local maintenance channel:

```sh
docker compose exec darkrun /darkspin/bin/darkrun maintenance update --config /darkspin/runtime/darkspin.toml
```

The command returns after accepting the request; follow the current daily server log or use Admin → Manage for the backup and update result. The default maximum drain interval is 30 minutes; maintenance can proceed earlier when active matches finish. Choose a shorter interval with `--minutes 5`, or use `--minutes 0` to skip the countdown and begin the final announcement and clean shutdown immediately. Warnings repeat at the same remaining-minute intervals as scheduled maintenance, including every minute during the final five minutes. New gameplay admissions and matches are blocked during the countdown; panel sign-in remains available.

This command performs the same stopped-server backup, verified binary check, startup validation, and recovery sequence described above. It also works with `DARKSPIN_IS_UPDATE_ENABLED=false`: that option disables automatic scheduling, while an explicit update still uses the configured manifest. The server restarts even if the manifest has no newer binary. A normal `docker compose restart darkrun` remains an ordinary restart and does not request this update/backup sequence.

Check or cancel a pending update:

```sh
docker compose exec darkrun /darkspin/bin/darkrun maintenance status --config /darkspin/runtime/darkspin.toml
docker compose exec darkrun /darkspin/bin/darkrun maintenance cancel --config /darkspin/runtime/darkspin.toml
```

Repeating `maintenance update` preserves the active update's deadline. Cancel it first to request a different interval. `maintenance announce` remains warning-only and does not schedule a shutdown; an active warning-only countdown must be cancelled before requesting an update. The update command requires a bootstrap-managed server and reports an error for ordinary standalone server runs. After final shutdown begins, the process can no longer accept cancellation.

Recreate Darkrun after changing environment overrides. The running bootstrap process keeps its loaded updater code while replacing the child binary; a later container restart loads the new updater.

For packaging with `mage darkrun:compose` and source builds, see [INSTALL.md](https://github.com/darkspinnet/darkspin/blob/main/INSTALL.md#packaging-and-source-development).

## Local deployment from this checkout

With Docker running in Linux-container mode and the build-103 game installation available under `bin/game`, run these commands from the repository root:

```sh
mage deployment:start
mage deployment:status
mage deployment:logs
mage deployment:stop
```

Start builds the current Linux amd64 Darkrun source, builds the bootstrap infrastructure image from `deployment/Dockerfile`, and starts an isolated `darkspin-deployment` Compose project. It waits for both services to become healthy. Stop the deployment before starting again to rebuild; stopping preserves its data.

`mage deployment:logs` reads the latest daily server file directly from the bind directory and shows up to 100 lines within a 64 KiB tail. It does not require a Docker connection. Use `docker compose logs` separately for PostgreSQL and container entry-point diagnostics.

All generated files and bind paths live under `bin/deployment`: `runtime/` contains the bootstrap binary, settings, and content cache; `postgres/` contains PostgreSQL data; `game` is a read-only-mounted directory link to the existing game installation; `image/` contains the infrastructure build context; and `logs/` contains build/Compose output. Game assets are neither copied into the image nor modified. The generated `.env` contains a random database password, an advertised host IPv4, and port `42137`; subsequent starts preserve it. Scheduled updates are disabled for this local deployment so published binaries cannot automatically replace the source build. The downloadable production Compose setup remains separate.

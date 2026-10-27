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
COMPOSE_PROJECT_NAME=darkspin
DARKSPIN_ADVERTISED_HOST=192.0.2.10
DARKSPIN_PORT=42127
DARKSPIN_HOME_PATH=./darkspin
DARKSPIN_GAME_HOST_PATH=/srv/game-build-103
POSTGRES_DB=darkspin
POSTGRES_USER=darkspin
POSTGRES_PASSWORD='replace-with-a-long-unique-password'
```

Replace the example address with the IPv4 players can reach and choose a long, unique database password. Single quotes preserve literal `$` characters in `.env`. Restrict access to `.env`, which contains credentials. Compose rejects an empty or omitted `POSTGRES_PASSWORD`.

The game root must contain `DarksporeBin/` and `Data/`. UID/GID 65532 must be able to read and traverse it. Allow the configured game port for **both TCP and UDP**.

## Start and stop

```sh
docker compose pull
docker compose up -d --no-build darkrun
docker compose logs -f postgres darkrun
docker compose ps
```

On first launch, the Darkrun entry point downloads a versioned manifest and its exact binary artifact, verifies its SHA-256 and build identity, and installs it in the runtime mount. It then runs `darkrun bootstrap`, which starts the dedicated server with `/darkspin/runtime/darkspin.toml`. Subsequent launches recover pending updates before serving players. The server creates its settings, authentication secret, content cache, and missing player tables before accepting players.

Connect through Darkspinner's Remote Servers account flow using the advertised address and port. This prototype uses username/password login; HTTPS and OAuth are not configured. Darkrun's health check probes its maintenance and main server endpoints; it does not establish that a multiplayer session works.

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

By default, maintenance warnings start daily at **03:30 UTC**, with shutdown after a **30-minute warning**. The existing `[bootstrap]` settings in `runtime/darkspin.toml` remain editable through Darkspinner's Config interface. Optional `.env` overrides are shown in `.env.example`; omitted inputs preserve saved settings. The defaults allow 30 minutes for candidate startup and retain completed backups for seven days. Set `DARKSPIN_IS_UPDATE_ENABLED=false` to disable scheduled maintenance.

Each scheduled clean shutdown follows this sequence, even when no new binary is available:

1. Darkrun disconnects players, flushes pending writes and checkpoints, closes its database connections, and exits. Bootstrap acquires the runtime lock. PostgreSQL remains running.
2. Bootstrap copies the installed binary, settings, and appearance/save files, then runs `pg_dump --format=custom` against the same configured player database. The compressed `postgres.dump` includes schema, rows, and sequences. It does not include cluster-wide roles or passwords.
3. The dump is flushed to disk, its archive table of contents is checked with `pg_restore --list`, and its SHA-256 and destination identity are recorded in `snapshot.json`. Only then is the snapshot marked complete. A dump failure prevents installation of an update; the existing server can restart without accepting a partial snapshot as a backup.
4. Bootstrap fetches and verifies an available candidate binary. Its durable `firstboot.json` marker identifies the matching backup before installation. Three readiness probes commit the candidate; only completed backups outside the retention window are pruned.
5. A failed candidate or interrupted first boot restores the database with `pg_restore --clean --if-exists --create --exit-on-error`, then restores its matching binary and runtime files. Recreating the player database removes any tables added by the failed candidate. The archive hash and configured destination must match before restoration. Failed file state is retained for diagnostics; failed PostgreSQL state is replaced by the snapshot.

Restore uses the `postgres` maintenance database so it can retry after interruption even if the player database was already dropped. It is not a single transaction: the recovery marker remains until all database and file restoration succeeds, and the server stays stopped on failure. Only the configured game database is recreated; the PostgreSQL service and cluster remain running. Do not connect another server or writer to that database during maintenance or recovery. Changing the host, port, or database while recovery is pending is rejected; use the original destination to recover first.

Dump and restore commands each have a 30-minute limit and are cancelled on supervisor shutdown. Ordinary container stops do not create a scheduled snapshot. `pg_restore --list` checks archive readability, not a full restore rehearsal. Keep independent backups for failures discovered after a candidate has already been committed healthy.

Cancel a pending warning countdown with `docker compose exec darkrun /darkspin/bin/darkrun maintenance cancel --config /darkspin/runtime/darkspin.toml`. Recreate Darkrun after changing environment overrides. The running bootstrap process keeps its loaded updater code while replacing the child binary; a later container restart loads the new updater.

For packaging with `mage darkrun:compose` and source builds, see [INSTALL.md](https://github.com/darkspinnet/darkspin/blob/main/INSTALL.md#packaging-and-source-development).

# Workspace conventions

## Generated diagnostics

- Write game and IDA reverse-engineering output under `bin/game/logs`. Do not
  write generated log files directly into `bin`.
- Keep protocol event captures under `bin/server/darkspin/logs/traces` so
  machine-readable traces remain with the server runtime.

## Launcher dialogs

- Never use browser-native `alert`, `confirm`, or `prompt`, Wails
  `runtime.MessageDialog`, Windows `MessageBox`, or another operating-system
  dialog for Darkspinner launcher interaction.
- Present launcher confirmations, warnings, errors, and information through the
  established themed in-window modal or inline-status components.
- If a condition occurs before the Wails window exists, focus the existing
  launcher when possible; otherwise record it through stderr or launcher-owned
  logs, or carry it into the first themed launcher view. Do not create a native
  fallback popup.

## Changelog

- Record user-visible changes in the root `CHANGELOG.md` under a daily heading
  formatted as `### YYYY-MM-DD`.
- Do not apply the Go line-length limit or fixed-column wrapping to Markdown. Keep each changelog bullet on one physical line.
- Reuse the current day's heading when multiple completed tasks land on the
  same date; do not create duplicate headings for that date.
- Keep each change to one brief `- ` line. Describe meaningful transitions as
  `from X to Y` where that context makes the result clearer.
- Name abilities by their full in-game name in changelog entries. Do not use
  ambiguous shorthand such as `Ride` for `Ride the Lightning`.
- Add or revise changelog lines only after the requested task is finished. Do
  not update the changelog iteratively for investigations, intermediate edits,
  internal refactors, generated files, or behavior that has not reached its
  completed implementation state.

## Client reverse engineering

- Keep Fang hooks observational or debugger-only by default. Do not use Fang
  to alter packaged UI defaults, suppress native presentation, bypass client
  decisions, or otherwise patch client gameplay logic unless the user
  explicitly approves that exact compatibility hook.

## Immutable game installation

- Never modify, patch, replace, rebuild, delete, rename, or write into any
  shipped game binary or asset.
- This prohibition includes executables, DLLs, DBPF/package files, property
  lists, cinematics, navigation assets, UI resources, and every other shipped
  client file, even when a content patch appears to be the simplest fix.
- Implement compatibility through server behavior, server-owned cached data,
  launcher configuration, or explicitly approved Fang source hooks. Never use
  an in-place client-asset mutation as a compatibility mechanism.

## Local development commands

- Prefer `mage darkrun:auth` to start the loopback-only local authentication broker.
- Use `mage darkspinner:build` after changing the all-in-one desktop application
  and `mage darkspinner:run` to start its existing binary without rebuilding it.
- Run `mage build` explicitly after changing the server, launcher, or Fang.

## Git safety

- Never run Git commands that stage, unstage, or commit changes. Read-only Git
  inspection such as status, log, show, and diff is allowed.

## Database inspection

- Prefer the built-in `darkrun db` commands for routine database inspection and
  focused updates. Do not create temporary Go programs or one-off SQLite scripts
  merely to look up or change a value that the command supports.
- Treat the runtime `content.db` as the authoritative local source for imported
  Lua 5.1 bytecode, including tutorial scripts. In the Darkspinner layout it is
  normally `bin/darkspinner/darkspin/cache/content.db`; query it with
  `darkrun db` and `--config bin/darkspinner/darkspin.toml` rather than assuming
  the older loose extracted Lua corpus is present.
- Use `darkrun db <table> get <id-or-login-name>` for shorthand lookup. Numeric
  values match `id`; other values partially match `login_name`.
- Use an explicit comparison for field queries, such as
  `darkrun db user get new_player_progress=3000`.
- Use `darkrun db <table> set <key> <value> where <field>=<value>` for focused
  updates. Never omit the explicit `where` clause.
- Use `darkrun db <table> bget <column> where <field>=<value> --output <path>`
  to extract exactly one binary column from exactly one row. Pass
  `--decode zlib` for zlib-compressed payloads such as
  `server_data.decoded_payload`, and pass `--force` only when replacing the
  named output is intentional. Write reverse-engineering output beneath
  `bin/game/logs`; do not pipe raw binary through PowerShell or JSON.
- For indexed Lua, first resolve the chunk with
  `darkrun db lua_chunk get source_name=<group>/<resource>.lua`, then use its
  `server_data_resource_id` as the `content_source_resource_id` predicate when
  extracting `server_data.decoded_payload` with `bget --decode zlib`.
- The command reads the storage driver from `darkspin.toml` and resolves the table
  between `darkspin/saves/darkspin.db` and `darkspin/cache/content.db`. Pass `--config` when
  operating outside the runtime directory containing that configuration.

## Package inspection

- Prefer `darkrun inspect <package>` for a read-only DBPF inventory before
  extracting a package. It lists every resource's stable synthetic filename,
  type, group, instance, stored and decoded sizes, and compression method.
- DBPF indexes do not retain original authored filesystem paths. Treat names
  printed by `inspect` as stable resource identities, not recovered source
  filenames. Use `darkrun unzip` only when payload bytes are actually needed.

## Archive inspection

- Always use the native 7-Zip executable to list or extract ZIP, RAR, 7z, and
  other general-purpose archives. Never use the Windows `tar` command for
  archive inspection or extraction; it can silently expose only part of a
  nested archive and produce an incomplete diagnostic inventory.

## Go error handling

- Never use an inline initializer for an error conditional. Assign the error on
  its own line, then check it in a separate `if err != nil` block. This applies
  to production code and tests.
- Never discard a return slot with `, _ :=`. Bind every result to a descriptive
  name and handle it explicitly so errors and other contract-bearing statuses
  remain visible. Errors are pivotal and must always be checked. When an error
  genuinely has no actionable recovery, handle it explicitly (at minimum with
  an `if err != nil` branch documenting why it is ignored), and preferably log
  the event with useful context.

```go
err := cmd.Execute()
if err != nil {
	// Handle the error.
}
```

## Control flow

- Prefer guard clauses and early returns over nested conditions. Handle invalid,
  exceptional, or terminating cases first so the main execution path remains
  flat and easy to scan.
- Use nested conditions when guard clauses would duplicate substantial logic or
  make genuinely complex control flow harder to understand.

## Simplicity and file size

### No tests without an explicit request

- **DO NOT CREATE, MODIFY, RENAME, MOVE, REGENERATE, OR RUN TESTS unless the
  user explicitly requests tests in the current task.** An implementation,
  bug-fix, refactor, verification, hero-kit sweep, or broad instruction to
  finish work does not grant permission to touch or run tests.
- This prohibition includes new `*_test.go` files, new test cases, edits or
  assertions added to existing tests, fixtures, snapshots, golden files,
  test-only helpers, mocks, fuzz targets, and isolated profile/decoder checks.
- Existing tests are read-only and must remain untouched unless the user
  explicitly asks to change them. Verify permitted work with production builds,
  compilation, static checks, and direct production observability instead.
  Report that tests were not run because repository policy prohibits them.
- During the current prototype phase, the acceptance check is whether the
  production game builds, launches, and performs the requested behavior in the
  real client/server path. Defer automated tests until the product path is
  proven and the user explicitly asks to introduce tests again.
- The conditional test-design guidance below applies only after the user has
  explicitly requested tests. It never implies permission by itself and never
  overrides this prohibition.

- Prefer the simplest direct control flow that expresses the behavior. Do not
  introduce callback pipelines, generic registries, or abstraction layers when
  a named function or small method is sufficient.
- Do not hide feature behavior in local function variables. A local closure is
  acceptable only when it is short, used at the call site, and captures no
  mutable domain state. Move scheduled actions, recursive behavior, reusable
  operations, and callbacks referenced later into named functions or methods.
- Keep state ownership explicit. When several operations share mutable state,
  give that state a small feature-owned type with named operations instead of
  capturing surrounding variables in closures.
- Do not create a new production Go file longer than 5,000 lines. Treat 500
  lines as a review signal that the file may own more than one responsibility.
- Do not add unrelated behavior to an existing file longer than 5,000 lines.
  Behavior-preserving edits that extract code from an oversized file are
  allowed until the file is below the limit.
- Split code by cohesive gameplay or business scope, not by arbitrary line
  ranges. Prefer a few plainly named packages and files over many tiny layers.
- When tests are explicitly requested, test reusable Lua compilation,
  scheduling, cancellation, and semantic-event behavior at their generic
  owner. Do not add a separate Go test for every Lua callback, private
  scheduled-step struct, or stale-generation guard.
- When tests are explicitly requested, give feature operations focused
  invariant coverage and wire adapters representative encoding fixtures.
  Prefer table-driven semantic traces for content variants, and remove root
  integration tests once equivalent feature/adapter coverage exists.
- Even when tests are explicitly requested, do not create a dedicated unit
  suite for every named ability. Test shared ability-loading, semantic-runtime,
  targeting, and wire-codec patterns, then cover a named ability only through a
  real integrated gameplay path, a confirmed regression, or a protocol contract
  unique to that ability.
- If tests were explicitly requested, still avoid theoretical coverage for
  behavior that is not connected to a production caller. Prefer making the
  production path functional and observable over expanding isolated fixtures.

## Identifier plurality

- Use `e` as the default method receiver name for Dark Spin-owned Go types.
  Do not use `i` as a receiver; reserve index-style names such as `i` and `j`
  for iteration.

- Reserve plural variable, field, and property names for collections that can
  contain multiple elements, such as maps, slices, arrays, and sets. Use a
  singular name for every scalar or single-value identifier.
- Choose collection nouns that form their plural with a simple `s` suffix.
  Replace irregular plurals such as `radii` with a clearer collection role,
  such as `footprints`, so plurality remains visually consistent.
- Name every Dark Spin-owned map and slice in the plural, including private
  fields, parameters, local variables, and return names. The name should
  describe the elements, such as `members map[uint64]Member`, `plans []Plan`,
  or `packets [][]byte`. A map used as an index may use an explicitly plural
  index name such as `usersByID`. Opaque byte buffers such as `payload []byte`,
  cryptographic secrets, and externally defined protocol/storage fields retain
  their conventional or contract-defined names.
- Name a service, manager, repository, store, or database reference in the
  singular even when that dependency manages or returns a collection. Give the
  reference its concrete role: use `userManager UserManager`, never `users`
  or a generic name such as `manager`.
- Apply repository naming conventions only to identifiers owned by darkspin.
  Never rename standard-library, third-party, generated, or protocol-defined
  API names to make them match local style rules.
- Prefix every boolean with `is` or `are` across Go, TOML, SQLite, PostgreSQL,
  and their integration types. Use `Is*` or `Are*` for Go identifiers and
  `is_*` or `are_*` for TOML keys, SQL columns, and SQLx `db` tags. This applies
  to native boolean columns and boolean-equivalent representations such as
  `TINYINT(1)`.
- Avoid generic variable names such as `value` and `values`. Every variable is
  inherently a value, so these names do not communicate its role. Prefer the
  concrete domain noun, such as `pkg`, `packageInst`, `user`, `field`, or
  `contentAsset`. Use `value` only when no more specific meaning exists, such as
  code implementing an intentionally generic container or protocol primitive.
- For a read-only filesystem handle returned by `os.Open`, prefer the
  conventional short name `r`, which communicates that the handle is being used
  as a reader. Do not call an open handle `file`; that describes the underlying
  object but not the handle's role. Name the corresponding `os.FileInfo` result
  `fi`, not the generic `info`. Use a more specific role name when multiple
  readers or file-info results are simultaneously in scope.
- Name a typed request or command payload parameter `req`. For example, use
  `NewTargetedAOERun(req TargetedAOEInput)` rather than naming that boundary
  payload `input`. Keep specific domain names for persistent state, returned
  results, individual scalar arguments, and values that are not request
  payloads.

### Error context

- Every error propagated across a function boundary must add a brief,
  operation-specific breadcrumb with `fmt.Errorf("<step>: %w", err)`. Do not
  use a bare `return err`, `return nil, err`, or equivalent.
- Keep wrapped breadcrumbs to one or two words. Prefer a stable lower-camel tag
  such as `userPath`, `userMarshal`, or `profileSave`; indexed context may use a
  compact suffix such as `fieldDecode[%d]`.
- Breadcrumbs must identify the specific failing branch. Within a function,
  different failure sites must not share a broad prefix such as `createUser`.
  Use `createPath`, `createMarshal`, and `createWrite` instead.
- Wrap sentinel errors too when local context is useful. `%w` preserves
  `errors.Is` and `errors.As` behavior.
- Wrapped breadcrumbs start lowercase and do not end with punctuation. When
  each layer follows this rule, the final chain reads like a compact stack
  trace: `domainUnlock: unlockSave: saveReplace: access denied`.
- When an error is handled instead of propagated, its log, CLI, protocol, or
  user-facing message may be longer and describe the complete scenario. The
  brevity rule applies to `%w` breadcrumbs, not to messages that explain a
  failure to an operator or player.
- Directly return a newly-created domain/sentinel error only when the current
  function is the place where that condition originates. Add context whenever
  that error is passed through another operation boundary.

```go
path, err := r.userPath(user.Username)
if err != nil {
	return fmt.Errorf("createPath: %w", err)
}
contents, err := marshalUser(user)
if err != nil {
	return fmt.Errorf("createMarshal: %w", err)
}
```

## File naming

- Use package and directory context to keep filenames short. Prefer one to
  three descriptive words and avoid filenames with four or more underscore-
  separated words. A long filename usually indicates that code belongs in a
  clearer package or repeats its directory context.
- Use singular package and directory names. Prefer `util` over `utils`,
  `instance` over `instances`, and `result` over `results`.

## Feature packages and boundaries

- Organize server code by business capability (package by feature / vertical
  slice), not by top-level technical layers such as `domain`, `application`, or
  `persistence`. A feature package owns its data, rules, operations, errors, and
  the small ports its operations consume.
- Define a port in the feature package that consumes it. Place filesystem,
  SQLite, PostgreSQL, and other implementations in adapter subpackages such as
  `account/xmlstore` or `account/sqlite`; adapters depend on the feature, never
  the reverse. Wire concrete adapters only in the server composition root.
- Dependency direction is `transport -> feature operation -> feature-owned
  port <- adapter`. Do not create shared technical-layer packages merely to
  reproduce a layered architecture.
- HTTP, Blaze, RakNet, and command-line adapters must not mutate persistent
  `sporenet.User` fields or call a repository directly. They decode a command,
  call a feature operation, and encode its result.
- Feature operations authenticate/authorize the actor, invoke business
  behavior, and own the persistence transaction. Rejected operations
  must not write storage. Persistence failures must not leave an accepted
  in-memory mutation behind.
- Feature data and business methods enforce gameplay invariants. They do not
  know XML, JSON, SQL, HTTP, TDF, RakNet, or process-global configuration.
- Treat every wire and storage representation as an adapter detail. Never add
  XML/JSON tags, SQL row concerns, TDF labels, or packet offsets to feature
  business types. Decode into typed feature commands and explicitly allowlist
  outbound fields in protocol-owned DTOs before marshalling data for a player.
- Return transport-neutral typed results or semantic events from feature
  operations. The selected wire adapter maps those payloads to its protocol
  DTOs and bytes; feature code must not instantiate a RakNet encoder, choose
  RakNet reliability or ordering, broadcast packets, or return encoded packet
  bytes.
- Never serialize a feature aggregate or storage row directly to an untrusted
  client. Transport adapters own their response DTOs and redact traces from
  those DTOs, so credentials, moderation state, anti-cheat data, internal IDs,
  and future server-only fields cannot leak through automatic serialization.
- Keep interfaces small and define them in the consuming package. Accept
  interfaces and return concrete types unless a real substitution boundary
  exists.
- Pass `context.Context` as the first argument to operations that can perform
  I/O. Do not store contexts in structs or use `context.Background()` in a
  transport request path.
- Only when the user explicitly requests tests, cover successful persistent
  operations, rejected invariants with no storage write, and storage
  failure/rollback.

## SQL naming

- Write SQL keywords, built-in data types, and SQLite `PRAGMA` in uppercase in
  embedded statements and schema definitions. Keep table names, column names,
  pragma names, parameters, and string literals in their authored case.
- Use singular names for SQL tables and every SQLx `db` struct tag, including
  columns that contain collections or serialized lists. For example, use the
  table `creature` instead of `creatures`, and use `db:"creature_reward"`,
  never `db:"creature_rewards"`.

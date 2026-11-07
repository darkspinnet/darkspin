# Darkrun conversion

`darkrun convert` converts packages into editable `.ds` directories and rebuilds packages from those directories. It also compiles Lua source and decompiles native Lua 5.1 bytecode, entirely inside the executable. No Lua, Java, Rust, or external decompiler installation is required to use it.

```text
darkrun convert script.lua script.luac
darkrun convert script.luac recovered.lua
darkrun convert ServerData.package ServerData.ds
darkrun convert ServerData.ds rebuilt.package
darkrun convert ServerData.package:123 recovered.lua
```

Bytecode is recognized by its header, including when its filename ends in `.lua`. Without an explicit destination, source produces `<name>.luac`, bytecode produces `<name>.lua`, and bytecode already named `.lua` produces `<name>.decompiled.lua`. Outputs default to the current directory. Existing destinations are never overwritten. Use a separate output location; never replace an installed game package.

Standalone compilation targets the build-103 Lua 5.1 layout: little endian, four-byte integers, string lengths, instructions, and floating-point numbers. Parsing and constant folding use the target's floating-point precision. The compiler does not run the source. Decompilation reconstructs readable syntax; it cannot recover original comments or names absent from debug information. Standalone decompilation reports unsupported control flow or invalid output as an error rather than emitting editable pseudocode.

## Lua in DS directories

Each Lua resource has a `.dse` descriptor and an editable `.lua` source sidecar. The descriptor records the target byte order, integer/string-length/number widths, and source identity. Every Lua source is compiled when rebuilding a package, including unchanged sources. No original resource bytes, byte hashes, or origin-package dependency are retained.

If source recovery fails for a package resource, conversion emits a `.lua.txt` diagnostic/disassembly instead of a `.lua` file. The descriptor marks this as `FORMAT disassembly`, and the command reports how many resources could not be recovered. Supply valid Lua at the descriptor's `SOURCE` path before rebuilding that resource. Package creation fails explicitly while any such source is missing; diagnostic listings are never compiled as Lua.

Lua-containing directories include `.luarc.json` and `definitions/game.d.lua`. Open the `.ds` directory as the editor workspace with Lua Language Server installed to load Lua 5.1 support and the generated definitions. Definitions describe external globals and table members observed statically in the converted source. They do not execute scripts or establish native API contracts; unknown types and signatures remain unknown. Script-owned declarations are left to the source files. These editor files are validated as sidecars and are not packed into game resources.

Legacy DS directories containing opaque Lua resources remain readable. Converting one to a new `.ds` directory upgrades its Lua resources to source sidecars and generates the editor files.

Lua exports are organized under `lua/<resource-group>/`, such as `lua/Abilities` and `lua/Modifiers`. Names come from the resource registry, hash-verified source/module references, or matching script-defined symbols. A script-defined label without a matching filename hash retains its instance ID in the filename. Scripts with no reliable name keep their ID within the known group.

`lua-index.json` is a searchable lookup of every Lua resource's group, instance, output path, naming evidence, defined symbols, module references, and ambiguous candidates. It is editor metadata and is not packed into the game. To add noun/creature ownership, supply the imported content database:

```text
darkrun convert ServerData.package ServerData.ds --content-db bin/darkspinner/darkspin/cache/content.db
```

Owner entries retain all matching nouns, ability slots, and authored ability asset names. These links come from exact catalog asset references; shared helpers may have multiple owners, and an owner link does not establish an original script filename. Repacking excludes the index. A DS-to-DS rewrite preserves its existing paths and index.

The decompiler and compiler are embedded WebAssembly modules generated from pinned source. Their binaries are ignored by Git. Mage application builds run `darkrun:preparelua`, which validates a cached build-input bundle and configures the Go build overlay pointing to the modules under `bin`. On a cache miss it builds them using the documented development toolchains. For a direct build, first run `mage darkrun:preparelua`, then `go build -overlay=bin/cache/lua-wasm/overlay.json -o bin/server/darkrun ./app/darkrun`; use `mage darkrun:buildluawasm` to force a source rebuild. CI builds the modules once and distributes the bundle to the application build jobs; Rust and the C toolchain are not needed by those consumers or by deployed applications.

Third-party components and their source callouts are documented in the root `ATTRIBUTION.txt`, including the required copyright and license texts. Run `darkrun convert --attribution` to display that same file from the binary; `--lua-notices` remains an alias.

Direct Go tooling, including editor language servers, needs the same overlay in its build flags when loading packages that embed the Lua modules. Mage configures it for its child processes; a standalone preparation command does not modify the parent shell environment.

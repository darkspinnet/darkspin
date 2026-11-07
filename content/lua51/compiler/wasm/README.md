# Embedded Lua compiler

The generated `compiler32.wasm` and `compiler64.wasm` are Lua 5.1.5's compiler and binary serializer, with the small `compile.c` stdin/stdout entry point. Their binaries live only under `bin/cache/lua-wasm`; a Go build overlay supplies the package-local logical paths needed by `go:embed`. They load source with `luaL_loadbuffer` and serialize the resulting function with `lua_dump`. They never call that function or open Lua libraries. The host provides no filesystem, environment variables, network, or subprocess interfaces. Each invocation has bounded input, output, linear memory, and execution time.

The number width selects literal parsing, constant folding, and chunk serialization. This is necessary because folding in float64 and subsequently narrowing serialized constants does not reproduce float32 arithmetic. The float32 configuration changes `LUA_NUMBER` to `float`, uses `strtof`, and updates the number formatting macros. Arithmetic contraction is disabled. Both variants retain full Lua compiler debug metadata.

Lua normally recovers compilation errors with `setjmp`/`longjmp`. WASI's version requires WebAssembly exception support unavailable in the host engine. The build substitutes `LUAI_THROW` with a diagnostic and process exit, makes `LUAI_TRY` execute directly, and removes the unused `setjmp.h` include from its private build copy. Each compiler guest handles one source and is discarded on error, so no recovery inside the guest is needed. The Lua parser and emitter are otherwise unmodified. Modified copies stay under `bin/cache`.

## Source and toolchain pins

- Lua 5.1.5: <https://www.lua.org/ftp/lua-5.1.5.tar.gz>; byte-identical distribution mirror <https://distfiles.macports.org/lua/lua-5.1.5.tar.gz>. Archive SHA256: `2640fc56a795f29d28ef15e13c34a47e223960b0240e8cb0a82d9b0738695333`.
- WASI SDK 25.0, Windows x86_64: <https://github.com/WebAssembly/wasi-sdk/releases/download/wasi-sdk-25/wasi-sdk-25.0-x86_64-windows.tar.gz>. Archive SHA256: `ebd4868f95a6e764bf29283e3bdf54df0df3902a29737176da9c17da218b939b`.
- SDK clang: LLVM 19.1.5, commit `ab4b5a2db582958af1ee308a790cfdb42bd24720`.
- Runtime license sources: wasi-libc tag `wasi-sdk-25` (`LICENSE`, `LICENSE-MIT`, `libc-top-half/musl/COPYRIGHT`, `libc-bottom-half/cloudlibc/LICENSE`, `dlmalloc/src/malloc.c`), and llvm-project tag `llvmorg-19.1.5` (`compiler-rt/LICENSE.TXT`). Their notices are consolidated in the root `ATTRIBUTION.txt`, with source callouts and the CLI's attribution output.

Download archives under `bin/cache`, verify both hashes, and extract each gzip followed by its tar with native 7-Zip. From the repository root, run:

```text
mage darkrun:buildluacompiler <extracted-sdk-directory> <extracted-lua-5.1.5-directory>
```

The Mage target builds both precision variants, keeps modified source copies and temporary files under `bin/cache/lua-compiler`, writes successful artifacts under `bin/cache/lua-compiler`, and prints their SHA256 hashes. Temporary environment settings apply only to the compiler child process.

For the complete compiler/decompiler bundle, use `mage darkrun:preparelua`. It validates the source fingerprint and module hashes in `bin/cache/lua-wasm/manifest.json` before generating the Go build overlay; a missing or stale bundle triggers a source build. Set `WASI_SDK` and `LUA_SOURCE` to use installed prerequisites. Windows can download the pinned archives automatically. Install Rust 1.99.1 with the `wasm32-wasip1` target for decompiler source builds (Windows uses the GNU host toolchain). `mage darkrun:buildluawasm` forces a complete rebuild. CI caches the bundle and uploads it as an internal artifact for downstream build jobs.

No C or Rust toolchain is needed at runtime or when preparing from a valid bundle. The WASM files remain embedded in deployed binaries. Reference artifact SHA256 from the Windows source build:

- `compiler32.wasm`: `c58d40afb9e75dc3c0b2db8909ebc7441c2c9b68ace38ed168cd8cb79bd17379`
- `compiler64.wasm`: `c9b6c2fd7a6be36967d952f31908f3d3f2e9e7b171f2261d454992179dbc6a3e`

The build-103 target header was independently confirmed as `1B-4C-75-61-51-00-01-04-04-04-04-00` from authoritative imported content database chunk 382, `Modifiers/0x2CC4DEFE.lua`, SHA256 `7f46544508eb7b34ea302436911f3eeead0adf633866cad0ae796b4bd38548e6`. Its provenance is also documented in `notes/pet-tooltip.md`; the live build-103 state uses float32 numbers. No installed client file is modified by compilation or conversion.

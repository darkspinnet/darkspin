#include "mod/mod.h"
#include "hook.h"

#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include <wchar.h>

/* Mod-only build-103 compatibility: mount read-only Spore databases behind the
 * game's own resources. Appearance selection remains server-owned. No package
 * bytes or client defaults are changed. DBBF resources use an in-memory DBPF
 * index and source-backed stream. Called only on the native update thread.
 * Source: build-103 package construction at RE source line 990740 and native
 * AddDatabase calls at vtable offset 80 (e.g. RE source line 939115).
 */
typedef void* (__cdecl* mod_manager_fn)(void);
typedef void* (__cdecl* mod_allocate_fn)(unsigned int, const char*, unsigned int);
typedef void* (FANG_THISCALL* mod_construct_fn)(void*, const wchar_t*, void*);
typedef void (FANG_THISCALL* mod_ref_fn)(void*);
typedef void (FANG_THISCALL* mod_initialize_fn)(void*);
typedef void (FANG_THISCALL* mod_path_fn)(void*, const wchar_t*);
typedef unsigned char (FANG_THISCALL* mod_open_fn)(void*, int, int, int);
typedef unsigned char (FANG_THISCALL* mod_add_fn)(void*, int, void*, int);

static int mod_state;
static unsigned int mod_attempts;
static wchar_t mod_manifest[32768];

/* The retail service discovery still requests its fixed loopback ports after
 * bootstrap. A Mod client must never fall through to the normal save server. */
int fang_redirect_mod_address(struct sockaddr_in* address, unsigned short port, unsigned short party_port) {
    static LONG mode = -1;
    LONG is_enabled = InterlockedCompareExchange(&mode, -1, -1);
    wchar_t marker[2];
    unsigned short requested;
    if (is_enabled < 0) {
        is_enabled = GetEnvironmentVariableW(L"DARKSPIN_MOD_MANIFEST", marker, 2) != 0;
        InterlockedCompareExchange(&mode, is_enabled, -1);
    }
    if (!is_enabled || (ntohl(address->sin_addr.s_addr) >> 24) != 127) return 0;
    requested = ntohs(address->sin_port);
    if (requested == 42127) {
        address->sin_port = htons(port);
        return 1;
    }
    if (requested == 42128) {
        address->sin_port = htons(party_port);
        return 1;
    }
    return 0;
}

static int mod_signature(BYTE* base, unsigned int offset, const BYTE* bytes, size_t count) {
    return readable_range(base + offset, count) && memcmp(base + offset, bytes, count) == 0;
}

static int mod_supported(BYTE* base) {
    static const BYTE allocator[] = {0x56, 0xe8, 0x3a, 0x59, 0x02, 0x00};
    static const BYTE constructor[] = {0x53, 0x56, 0x8b, 0xf1};
    void** stream_methods = (void**)(base + 0xc3f238);
    void** database_methods = (void**)(base + 0xc48e60);
    return sizeof(void*) == 4 &&
        mod_signature(base, 0x6efda0, allocator, sizeof(allocator)) &&
        mod_signature(base, 0x71b640, constructor, sizeof(constructor)) &&
        readable_range(base + 0x3aeb70, 6) &&
        base[0x3aeb70] == 0xa1 && base[0x3aeb75] == 0xc3 &&
        readable_range(stream_methods, 60) &&
        stream_methods[7] == base + 0x6f23d0 &&
        stream_methods[10] == base + 0x6f2550 &&
        stream_methods[12] == base + 0x6f25e0 &&
        readable_range(database_methods, 108) &&
        database_methods[6] == base + 0x71a2c0 &&
        database_methods[26] == base + 0x71a590;
}

static int mod_mount(BYTE* base, void* manager, const wchar_t* path, int priority, unsigned int index,
    const BYTE* payload, unsigned int size) {
    mod_allocate_fn allocate = (mod_allocate_fn)(base + 0x6efda0);
    mod_construct_fn construct = (mod_construct_fn)(base + 0x71b640);
    void* allocation = allocate(904, "Darkspin/SporeMod", 0);
    void* database;
    void* reference;
    void** methods;
    void** ref_methods;
    void** manager_methods;
    unsigned char is_added = 0;
    if (!allocation) return 0;
    database = construct(allocation, NULL, NULL);
    if (!database || !readable_range(database, 8)) return 0;
    reference = (BYTE*)database + 4;
    methods = *(void***)database;
    ref_methods = *(void***)reference;
    manager_methods = *(void***)manager;
    if (!readable_range(methods, 48) || !readable_range(ref_methods, 8) ||
        !readable_range(manager_methods, 84)) return 0;
    ((mod_ref_fn)ref_methods[0])(reference);
    ((mod_initialize_fn)methods[1])(database);
    ((mod_path_fn)methods[11])(database, index == 11 ? L"DarkspinModEditorEnvironment.memory.package" :
        index == 10 ? L"DarkspinModEditorScene.memory.package" :
        payload ? L"DarkspinModEditorMusic.memory.package" : path);
    if (index == 11 ? !fang_mod_attach_environment(database, path) :
        index == 10 ? !fang_mod_attach_scene(database, path) :
        payload ? !fang_mod_attach_memory(database, path, payload, size) :
        fang_mod_attach_dbbf(database, path, index) < 0) {
        ((mod_ref_fn)ref_methods[1])(reference);
        return 0;
    }
    /* File access 1 is read-only; native disposition 6 opens an existing file. */
    if (((mod_open_fn)methods[6])(database, 1, 6, 0)) {
        is_added = ((mod_add_fn)manager_methods[20])(manager, 1, database, priority);
        if (!is_added) trace_client_state("mod_database_register_failed", index);
    } else {
        trace_client_state("mod_database_open_failed", index);
    }
    ((mod_ref_fn)ref_methods[1])(reference);
    return is_added != 0;
}

void fang_update_mod(void) {
    BYTE* base;
    void* manager;
    unsigned int mounted = 0;
    unsigned int failed = 0;
    unsigned int audio_count = 0;
    unsigned int index;
    wchar_t path[32768];
    wchar_t key[32];
    fang_mod_update_dance();
    fang_mod_update_editor();
    if (mod_state == 2) return;
    if (mod_state == 0) {
        DWORD length = GetEnvironmentVariableW(L"DARKSPIN_MOD_MANIFEST", mod_manifest,
            sizeof(mod_manifest) / sizeof(wchar_t));
        if (length == 0 || length >= sizeof(mod_manifest) / sizeof(wchar_t)) {
            mod_state = 2;
            return;
        }
        if (GetPrivateProfileIntW(L"mod", L"version", 0, mod_manifest) != 1) {
            trace_client_state("mod_manifest_rejected", 1);
            mod_state = 2;
            return;
        }
        mod_state = 1;
    }
    base = (BYTE*)GetModuleHandleW(NULL);
    if (!mod_supported(base)) {
        trace_client_state("mod_build_unsupported", 1);
        mod_state = 2;
        return;
    }
    manager = ((mod_manager_fn)(base + 0x3aeb70))();
    if (!manager || !readable_range(manager, sizeof(void*))) {
        if (++mod_attempts >= 600) {
            trace_client_state("mod_manager_timeout", mod_attempts);
            mod_state = 2;
        }
        return;
    }
    /* Never retry partially mounted manifests. The native manager owns each
     * successfully registered database for the rest of this client process. */
    mod_state = 2;
    trace_client_state("mod_png_observer", fang_mod_observe_import(base, mod_manifest));
    fang_mod_install_baseline(base, mod_manifest);
    fang_mod_install_scale(base, mod_manifest);
    fang_mod_install_fit(base, mod_manifest);
    for (index = 1; index <= 8; ++index) {
        if (index >= 7 && GetPrivateProfileIntW(L"mod", L"model_audio", 0, mod_manifest) != 1) continue;
        _snwprintf(key, sizeof(key) / sizeof(wchar_t), L"package%u", index);
        if (!GetPrivateProfileStringW(L"mod", key, L"", path,
            sizeof(path) / sizeof(wchar_t), mod_manifest)) continue;
        if (mod_mount(base, manager, path, -2000 + (int)index, index, NULL, 0)) {
            ++mounted;
            if (index >= 7) ++audio_count;
            trace_client_state("mod_package_mounted", index);
        } else {
            ++failed;
            trace_client_state("mod_package_failed", index);
        }
    }
    trace_client_state("mod_package_count", mounted);
    trace_client_state("mod_package_failure_count", failed);
    trace_client_state("mod_audio_bank_count", audio_count);
    if (mounted && !failed) fang_mod_install_portrait(base, mod_manifest);
    if (mounted && !failed) fang_mod_install_character(base, mod_manifest);
    /* Owned stock heroes use the shipped editor assets. Body editing in Mod
     * mode does not require an import binding or mounted source packages. */
    fang_mod_install_editor(base, mod_manifest);
    if (mounted && !failed && GetPrivateProfileIntW(L"mod", L"editor_scene", 0, mod_manifest) == 1) {
        if (GetPrivateProfileStringW(L"mod", L"package2", L"", path,
            sizeof(path) / sizeof(wchar_t), mod_manifest) &&
            mod_mount(base, manager, path, -1990, 10, NULL, 0)) {
            trace_client_state("mod_editor_scene_package_mounted", 1);
            trace_client_state("mod_editor_environment_package_mounted",
                mod_mount(base, manager, path, -1989, 11, NULL, 0));
            fang_mod_install_scene(base);
        } else trace_client_state("mod_editor_scene_package_failed", 1);
    }
    if (audio_count == 2) {
        if (GetPrivateProfileIntW(L"mod", L"editor_music", 0, mod_manifest) == 1) {
            BYTE music_package[320];
            unsigned int size = fang_mod_music_package(music_package, sizeof(music_package));
            /* path still names package8, the successfully mounted Spore Audio2
             * bank. The additional database contains only a private event. */
            if (size && mod_mount(base, manager, path, -1991, 9, music_package, size)) {
                trace_client_state("mod_editor_music_package_mounted", 1);
                fang_mod_install_music(base, mod_manifest);
            } else trace_client_state("mod_editor_music_package_failed", 1);
        }
        fang_mod_install_audio(base);
        fang_mod_install_emote(base);
        fang_mod_install_dance(base);
    }
}

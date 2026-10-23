#include "hook.h"

#include <errno.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <wchar.h>

/* Only picture-key formatting at five native portrait controls is wrapped.
 * The formatter still owns its EASTL string. Model loading, GFx calls, hero
 * labels and packaged UI are untouched. Use PNGs from the mounted Spore source.
 */
typedef void (__cdecl* portrait_format_fn)(const uint32_t*, void*, const char*);
typedef unsigned char (__cdecl* portrait_lookup_fn)(uint32_t, uint32_t, uint32_t*);

typedef struct {
    uint32_t noun;
    uint32_t source;
    uint32_t key[3];
    int is_key_ready;
} mod_portrait;

static portrait_format_fn native_format;
static portrait_lookup_fn native_lookup;
static mod_portrait portraits[128];
static unsigned int portrait_count;
static volatile LONG portrait_trace_count;

static void __cdecl mod_portrait_format(const uint32_t* key, void* text, const char* suffix) {
    uint32_t source = 0;
    uint32_t picture[3];
    unsigned int index;
    if (!readable_range(key, 12) || key[1] != 0x2f7d0004) {
        native_format(key, text, suffix);
        return;
    }
    if (!fang_mod_portrait_source(key, &source)) {
        uint32_t model_key[3] = {key[0], 0x2b978c46, key[2]};
        /* Registered native saves deliberately have no source override. */
        if (fang_mod_is_bound_key(model_key)) {
            native_format(key, text, suffix);
            return;
        }
        for (index = 0; index < portrait_count; ++index) {
            mod_portrait* portrait = &portraits[index];
            if (!portrait->is_key_ready)
                portrait->is_key_ready = native_lookup(portrait->noun, 0, portrait->key) != 0;
            if (portrait->is_key_ready && key[0] == portrait->key[0] && key[2] == portrait->key[2]) {
                source = portrait->source;
                break;
            }
        }
    }
    if (!source) {
        native_format(key, text, suffix);
        return;
    }
    picture[0] = source;
    picture[1] = 0x2f7d0004;
    picture[2] = 0x40626200;
    native_format(picture, text, suffix);
    if (InterlockedIncrement(&portrait_trace_count) <= 64) {
        trace_client_state("mod_portrait_requested", key[0]);
        trace_client_state("mod_portrait_source", source);
    }
}

static int portrait_number(const wchar_t* manifest, const wchar_t* name, uint32_t* number) {
    wchar_t text[32];
    wchar_t* end;
    unsigned long long parsed;
    if (!GetPrivateProfileStringW(L"mod", name, L"", text, sizeof(text) / sizeof(wchar_t), manifest))
        return 0;
    errno = 0;
    parsed = wcstoull(text, &end, 10);
    if (errno || end == text || *end || !parsed || parsed > UINT32_MAX) return 0;
    *number = (uint32_t)parsed;
    return 1;
}

void fang_mod_install_portrait(BYTE* base, const wchar_t* manifest) {
    /* SetCreatureImage: 0x41F950, 0x4200C0, 0x4203F0 and the squad-row
     * update at 0x442750. SetCreatureIcon: 0x443210. All five calls to
     * 0x7AE040 were checked against build-103 executable disassembly.
     */
    static const unsigned int calls[] = {0x1fa71, 0x201b4, 0x204a2, 0x4288b, 0x43389};
    static const BYTE lookup_signature[] = {0x8b, 0x4c, 0x24, 0x0c, 0x8b, 0x54, 0x24, 0x08};
    unsigned int index;
    unsigned int installed = 0;
    if (GetPrivateProfileIntW(L"mod", L"model_portrait", 0, manifest) != 1) return;
    if (!readable_range(base + 0xe4e35, sizeof(lookup_signature)) ||
        memcmp(base + 0xe4e35, lookup_signature, sizeof(lookup_signature)) != 0) {
        trace_client_state("mod_portrait_unsupported", 1);
        return;
    }
    for (index = 1; index <= sizeof(portraits) / sizeof(portraits[0]); ++index) {
        wchar_t name[32];
        uint32_t noun;
        uint32_t source;
        _snwprintf(name, sizeof(name) / sizeof(wchar_t), L"hero%u", index);
        if (!portrait_number(manifest, name, &noun)) continue;
        _snwprintf(name, sizeof(name) / sizeof(wchar_t), L"source%u", index);
        if (!portrait_number(manifest, name, &source)) {
            trace_client_state("mod_portrait_binding_invalid", index);
            return;
        }
        portraits[portrait_count].noun = noun;
        portraits[portrait_count].source = source;
        ++portrait_count;
    }
    if (!portrait_count) return;
    native_format = (portrait_format_fn)(base + 0x3ae040);
    native_lookup = (portrait_lookup_fn)(base + 0xe4e30);
    for (index = 0; index < sizeof(calls) / sizeof(calls[0]); ++index) {
        if (!patch_call(base + calls[index], native_format, mod_portrait_format)) break;
        ++installed;
    }
    if (installed != sizeof(calls) / sizeof(calls[0])) {
        trace_client_state("mod_portrait_hook_failed", installed);
        while (installed) {
            --installed;
            if (!patch_call(base + calls[installed], mod_portrait_format, native_format))
                trace_client_state("mod_portrait_rollback_failed", installed);
        }
        return;
    }
    trace_client_state("mod_portrait_enabled", portrait_count);
}

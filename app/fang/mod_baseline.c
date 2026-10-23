#include "hook.h"

#include <stdint.h>
#include <errno.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <wchar.h>

/* Build 103's 0x6BA870 compares immutable appearance features with the
 * original template and emits 0x10000000 on mismatch. This optional adapter
 * supplies the bound import as its template baseline. It preserves the
 * comparison itself and all other model-validation rules.
 * Activation requires model_baseline=1 in the launcher-owned Mod manifest.
 */
typedef unsigned char (__cdecl* mod_baseline_fn)(const uint32_t*, void*, uint32_t*, void*, void*);
typedef void** (__cdecl* mod_provider_fn)(void);

typedef struct {
    void* slots[10];
    mod_baseline_fn original;
} mod_baseline_provider;

static __thread mod_baseline_provider baseline_provider;
static mod_provider_fn native_provider;
static CRITICAL_SECTION baseline_lock;
static uint32_t source_instances[128];
static uint32_t hero_nouns[128];
static uint32_t model_keys[512][3];
static uint32_t portrait_sources[512];
static uint32_t fit_sources[512];
static uint32_t fit_nouns[512];
static unsigned int source_count;
static unsigned int model_count;
static int is_baseline_enabled;

int fang_mod_is_bound_key(const uint32_t* key) {
    unsigned int index;
    int is_bound = 0;
    if (!is_baseline_enabled || !readable_range(key, 12)) return 0;
    EnterCriticalSection(&baseline_lock);
    for (index = 0; index < model_count; ++index) {
        if (memcmp(key, model_keys[index], sizeof(model_keys[index])) == 0) {
            is_bound = 1;
            break;
        }
    }
    LeaveCriticalSection(&baseline_lock);
    return is_bound;
}

/* Import identity also covers preview rigs, which have no gameplay actor.
 * Edited saves retain their explicit parent noun; original imports retain the
 * source creation so shared-source targets can be checked for ambiguity. */
int fang_mod_bound_identity(const uint32_t* key, uint32_t* source, uint32_t* noun) {
    unsigned int index;
    int is_found = 0;
    if (!is_baseline_enabled || !source || !noun || !readable_range(key, 12)) return 0;
    EnterCriticalSection(&baseline_lock);
    for (index = 0; index < model_count; ++index) {
        if (memcmp(key, model_keys[index], sizeof(model_keys[index])) != 0) continue;
        *source = fit_sources[index];
        *noun = fit_nouns[index];
        is_found = 1;
        break;
    }
    LeaveCriticalSection(&baseline_lock);
    return is_found;
}

int fang_mod_portrait_source(const uint32_t* key, uint32_t* source) {
    unsigned int index;
    int is_found = 0;
    if (!is_baseline_enabled || !source || !readable_range(key, 12) || key[1] != 0x2f7d0004) return 0;
    EnterCriticalSection(&baseline_lock);
    for (index = 0; index < model_count; ++index) {
        if (model_keys[index][0] == key[0] && model_keys[index][2] == key[2] && portrait_sources[index]) {
            *source = portrait_sources[index];
            is_found = 1;
            break;
        }
    }
    LeaveCriticalSection(&baseline_lock);
    return is_found;
}

static unsigned char __cdecl mod_bound_baseline(const uint32_t* key, void* source,
    uint32_t* core_key, void* allowed_parts, void* context) {
    unsigned char is_valid = baseline_provider.original(key, source, core_key, allowed_parts, context);
    if (is_valid && readable_range(key, 12) && readable_range(core_key, 12) && fang_mod_is_bound_key(key)) {
        memcpy(core_key, key, 12);
        trace_client_state("mod_bound_baseline", key[0]);
    }
    return is_valid;
}

static void** __cdecl mod_bound_provider(void) {
    void** provider = native_provider();
    if (!readable_range(provider, sizeof(baseline_provider.slots)) || !provider[9]) return provider;
    memcpy(baseline_provider.slots, provider, sizeof(baseline_provider.slots));
    baseline_provider.original = (mod_baseline_fn)provider[9];
    baseline_provider.slots[9] = mod_bound_baseline;
    return baseline_provider.slots;
}

int fang_mod_bind_baseline(const uint32_t* metadata, const uint32_t* model_key) {
    unsigned int index;
    uint32_t portrait_source;
    uint32_t fit_source = 0;
    uint32_t fit_noun = 0;
    int is_source_bound = 0;
    if (!is_baseline_enabled || !readable_range(metadata, 44) || !readable_range(model_key, 12) ||
        model_key[1] != 0x2b978c46 || model_key[2] != 0x40626200) return 0;
    for (index = 0; index < source_count; ++index) {
        if (source_instances[index] == metadata[10]) {
            fit_source = source_instances[index];
            is_source_bound = 1;
            break;
        }
        /* Native editor saves replace the source instance but retain the
         * parent hero asset ID at metadata +24. Recognize only version-7
         * saves belonging to a hero explicitly bound in this Mod profile.
         * The immutable-template comparison itself still runs unchanged. */
        if (metadata[0] == 7 && metadata[7] == 0 && hero_nouns[index] != 0 &&
            metadata[6] == hero_nouns[index]) {
            trace_client_state("mod_baseline_saved_hero", metadata[6]);
            fit_source = source_instances[index];
            fit_noun = hero_nouns[index];
            is_source_bound = 1;
            break;
        }
    }
    if (!is_source_bound) return 0;
    /* Original imports have a PNG in the mounted Spore package. Native editor
     * saves use their own generated portrait; do not replace painted saves
     * with the original Spore picture. */
    portrait_source = metadata[0] == 6 ? source_instances[index] : 0;
    EnterCriticalSection(&baseline_lock);
    for (index = 0; index < model_count; ++index) {
        if (memcmp(model_keys[index], model_key, 12) == 0) {
            portrait_sources[index] = portrait_source;
            fit_sources[index] = fit_source;
            fit_nouns[index] = fit_noun;
            LeaveCriticalSection(&baseline_lock);
            return 1;
        }
    }
    if (model_count < sizeof(model_keys) / sizeof(model_keys[0])) {
        portrait_sources[model_count] = portrait_source;
        fit_sources[model_count] = fit_source;
        fit_nouns[model_count] = fit_noun;
        memcpy(model_keys[model_count++], model_key, 12);
        trace_client_state("mod_baseline_registered", model_key[0]);
    } else {
        trace_client_state("mod_baseline_capacity", model_count);
        LeaveCriticalSection(&baseline_lock);
        return 0;
    }
    LeaveCriticalSection(&baseline_lock);
    return 1;
}

void fang_mod_install_baseline(BYTE* base, const wchar_t* manifest) {
    unsigned int index;
    wchar_t key[32];
    BYTE* first_call = base + 0x2ba89a;
    BYTE* second_call = base + 0x2ba8a4;
    if (GetPrivateProfileIntW(L"mod", L"model_baseline", 0, manifest) != 1) {
        trace_client_state("mod_baseline_enabled", 0);
        return;
    }
    for (index = 1; index <= sizeof(source_instances) / sizeof(source_instances[0]); ++index) {
        unsigned long long instance;
        wchar_t text[32];
        wchar_t* end;
        _snwprintf(key, sizeof(key) / sizeof(wchar_t), L"source%u", index);
        if (!GetPrivateProfileStringW(L"mod", key, L"", text,
            sizeof(text) / sizeof(wchar_t), manifest)) continue;
        errno = 0;
        instance = wcstoull(text, &end, 10);
        if (errno || end == text || *end || !instance || instance > UINT32_MAX) {
            trace_client_state("mod_baseline_source_invalid", index);
            return;
        }
        source_instances[source_count] = (uint32_t)instance;
        _snwprintf(key, sizeof(key) / sizeof(wchar_t), L"hero%u", index);
        if (GetPrivateProfileStringW(L"mod", key, L"", text,
            sizeof(text) / sizeof(wchar_t), manifest)) {
            errno = 0;
            instance = wcstoull(text, &end, 10);
            if (errno || end == text || *end || !instance || instance > UINT32_MAX) {
                trace_client_state("mod_baseline_hero_invalid", index);
                return;
            }
            hero_nouns[source_count] = (uint32_t)instance;
        }
        ++source_count;
    }
    if (!source_count) { trace_client_state("mod_baseline_source_missing", 1); return; }
    native_provider = (mod_provider_fn)(base + 0x2694d0);
    InitializeCriticalSection(&baseline_lock);
    if (!patch_call(first_call, native_provider, mod_bound_provider)) {
        trace_client_state("mod_baseline_hook_failed", 1);
        return;
    }
    if (!patch_call(second_call, native_provider, mod_bound_provider)) {
        if (!patch_call(first_call, mod_bound_provider, native_provider))
            trace_client_state("mod_baseline_rollback_failed", 1);
        trace_client_state("mod_baseline_hook_failed", 2);
        return;
    }
    is_baseline_enabled = 1;
    trace_client_state("mod_baseline_enabled", 1);
}

#include "mod/mod.h"
#include "hook.h"

#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include <wchar.h>

#define CHARACTER_ALIAS 0x46414e47u

typedef int (__cdecl* character_graphics_fn)(uint32_t, const uint32_t*, int, void*, void*, int);
typedef BYTE* (__cdecl* character_noun_fn)(uintptr_t, void*);
typedef const uint32_t* (__cdecl* character_state_fn)(const uint32_t*, const uint32_t*, int);
typedef unsigned char (__cdecl* character_lookup_fn)(uint32_t, uint32_t, uint32_t*);

typedef struct {
    uint32_t noun;
    uint32_t source;
    uint32_t key[3];
    uint32_t noun_copy[96];
    uint32_t descriptor[14];
    wchar_t path[32768];
    void* retained;
    int is_attempted;
    int is_ready;
    int is_noun_ready;
} mod_character;

typedef struct character_graphics {
    struct character_graphics* next;
    uint32_t object;
    const uint32_t* original;
    uint32_t state[50];
} character_graphics;

static mod_character characters[128];
static unsigned int character_count;
static character_graphics* graphics;
static unsigned int graphics_count;
static character_graphics_fn native_graphics;
static character_noun_fn native_noun;
static character_state_fn native_state;
static character_lookup_fn native_lookup;
static __thread mod_character* active_character;
static volatile LONG character_trace_count;

uint32_t fang_mod_noun_id(uintptr_t reference) {
    const char* name;
    uint32_t hash = 0x811c9dc5;
    unsigned int index;
    if (!readable_range((void*)reference, sizeof(name))) return (uint32_t)reference;
    name = *(const char**)reference;
    if (!readable_range(name, 256)) return 0;
    for (index = 0; index < 256; ++index) {
        unsigned char letter = (unsigned char)name[index];
        if (!letter) return index ? hash : 0;
        if (letter >= 'A' && letter <= 'Z') letter += 'a' - 'A';
        hash = hash * 0x01000193 ^ letter;
    }
    return 0;
}

static mod_character* character_binding(uint32_t noun) {
    unsigned int index;
    for (index = 0; index < character_count; ++index)
        if (characters[index].noun == noun) return &characters[index];
    return NULL;
}

/* Only this graphics-construction call receives a private noun header. The
 * simulation and every other native noun lookup keep the authored definition.
 * Clear the effect-only renderer alternative at +252; geometry selection is
 * replaced by the creature renderer in the private graphics state below. */
static BYTE* __cdecl mod_character_noun(uintptr_t reference, void* guard) {
    BYTE* original = native_noun(reference, guard);
    uint32_t noun = fang_mod_noun_id(reference);
    mod_character* character = active_character;
    if (!character || character->noun != noun || !character->is_ready ||
        !readable_range(original, sizeof(character->noun_copy)))
        return original;
    if (!character->is_noun_ready) {
        memcpy(character->noun_copy, original, sizeof(character->noun_copy));
        character->noun_copy[38] = noun;
        character->noun_copy[39] = CHARACTER_ALIAS;
        character->noun_copy[63] = 0;
        character->is_noun_ready = 1;
    }
    return (BYTE*)character->noun_copy;
}

static const uint32_t* __cdecl mod_character_state(const uint32_t* state,
    const uint32_t* noun, int requested) {
    mod_character* character;
    if (readable_range(state, 24) && state[5] == CHARACTER_ALIAS) {
        character = character_binding(state[4]);
        if (character && character->is_ready) return character->descriptor;
    }
    return native_state(state, noun, requested);
}

static unsigned char __cdecl mod_character_lookup(uint32_t instance, uint32_t group, uint32_t* key) {
    mod_character* character = group == CHARACTER_ALIAS ? character_binding(instance) : NULL;
    if (!character || !character->is_ready || !readable_range(key, 12))
        return native_lookup(instance, group, key);
    memcpy(key, character->key, 12);
    return 1;
}

static int __cdecl mod_character_graphics(uint32_t object, const uint32_t* state,
    int requested, void* context, void* is_transform, int child) {
    mod_character* character;
    character_graphics* copy;
    mod_character* previous;
    int result;
    if (!readable_range(state, 200))
        return native_graphics(object, state, requested, context, is_transform, child);
    character = character_binding(fang_mod_noun_id(state[2]));
    if (!character) return native_graphics(object, state, requested, context, is_transform, child);
    if (!character->is_attempted) {
        unsigned int index;
        character->is_attempted = 1;
        for (index = 0; index < character_count; ++index) {
            mod_character* source = &characters[index];
            if (!source->is_ready || source->source != character->source ||
                wcscmp(source->path, character->path) != 0) continue;
            memcpy(character->key, source->key, sizeof(character->key));
            character->is_ready = 1;
            break;
        }
        if (!character->is_ready)
            character->is_ready = fang_mod_import_source(character->path, character->source,
                character->key, &character->retained);
    }
    if (!character->is_ready) return native_graphics(object, state, requested, context, is_transform, child);
    for (copy = graphics; copy; copy = copy->next)
        if (copy->object == object) break;
    if (!copy) {
        if (graphics_count >= 4096) {
            trace_client_state("mod_character_graphics_capacity", object);
            return native_graphics(object, state, requested, context, is_transform, child);
        }
        copy = (character_graphics*)HeapAlloc(GetProcessHeap(), HEAP_ZERO_MEMORY, sizeof(*copy));
        if (!copy) return native_graphics(object, state, requested, context, is_transform, child);
        copy->object = object;
        copy->original = state;
        copy->next = graphics;
        graphics = copy;
        ++graphics_count;
    }
    memcpy(copy->state, state, sizeof(copy->state));
    copy->state[4] = character->noun;
    copy->state[5] = CHARACTER_ALIAS;
    /* Retail graphics construction checks these mesh/effect alternatives
     * before its creature asset. Clear them only on our owned presentation
     * copy, retaining noun identity, transforms and all simulation inputs. */
    copy->state[23] = copy->state[24] = copy->state[25] = copy->state[26] = 0;
    copy->state[32] = copy->state[33] = copy->state[36] = copy->state[39] = 0;
    previous = active_character;
    active_character = character;
    result = native_graphics(object, copy->state, 0, context, is_transform, child);
    active_character = previous;
    if (!result) {
        trace_client_state("mod_character_graphics_failed", character->noun);
        active_character = NULL;
        result = native_graphics(object, state, requested, context, is_transform, child);
        active_character = previous;
        return result;
    }
    if (InterlockedIncrement(&character_trace_count) <= 128)
        trace_client_state("mod_character_replaced", character->noun);
    return result;
}

void fang_mod_install_character(BYTE* base, const wchar_t* manifest) {
    /* Four entry/refresh callers; the first is native compound recursion. */
    static const unsigned int calls[] = {0x108e07, 0x10994a, 0x10a3c3, 0x10a572};
    unsigned int index;
    unsigned int installed = 0;
    wchar_t name[32];
    if (GetPrivateProfileIntW(L"mod", L"character_override", 0, manifest) != 1) return;
    for (index = 1; index <= 128; ++index) {
        mod_character* character = &characters[character_count];
        _snwprintf(name, 32, L"is_npc%u", index);
        if (GetPrivateProfileIntW(L"mod", name, 0, manifest) != 1) continue;
        _snwprintf(name, 32, L"hero%u", index);
        character->noun = GetPrivateProfileIntW(L"mod", name, 0, manifest);
        _snwprintf(name, 32, L"source%u", index);
        character->source = GetPrivateProfileIntW(L"mod", name, 0, manifest);
        _snwprintf(name, 32, L"png%u", index);
        if (!character->noun || !character->source ||
            !GetPrivateProfileStringW(L"mod", name, L"", character->path, 32768, manifest)) {
            trace_client_state("mod_character_binding_invalid", index);
            return;
        }
        ++character_count;
    }
    if (!character_count) return;
    native_graphics = (character_graphics_fn)(base + 0x108b60);
    native_noun = (character_noun_fn)(base + 0x5eca10);
    native_state = (character_state_fn)(base + 0xf1a40);
    native_lookup = (character_lookup_fn)(base + 0xe4e30);
    /* Supporting calls first. Their exact original targets gate this build. */
    if (!patch_call(base + 0x108ba0, native_noun, mod_character_noun)) goto failed;
    installed = 1;
    if (!patch_call(base + 0x108d74, native_state, mod_character_state)) goto failed;
    installed = 2;
    if (!patch_call(base + 0xeeb6c, native_lookup, mod_character_lookup)) goto failed;
    installed = 3;
    for (index = 0; index < sizeof(calls) / sizeof(calls[0]); ++index) {
        if (!patch_call(base + calls[index], native_graphics, mod_character_graphics)) goto failed;
        ++installed;
    }
    trace_client_state("mod_character_enabled", character_count);
    return;
failed:
    trace_client_state("mod_character_hook_failed", installed + 1);
    while (installed > 3) {
        --installed;
        if (!patch_call(base + calls[installed - 3], mod_character_graphics, native_graphics))
            trace_client_state("mod_character_rollback_failed", installed);
    }
    if (installed >= 3 && !patch_call(base + 0xeeb6c, mod_character_lookup, native_lookup))
        trace_client_state("mod_character_rollback_failed", 3);
    if (installed >= 2 && !patch_call(base + 0x108d74, mod_character_state, native_state))
        trace_client_state("mod_character_rollback_failed", 2);
    if (installed >= 1 && !patch_call(base + 0x108ba0, mod_character_noun, native_noun))
        trace_client_state("mod_character_rollback_failed", 1);
}

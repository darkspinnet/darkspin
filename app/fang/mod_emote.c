#include "hook.h"

#include <stdint.h>
#include <string.h>

typedef BYTE* (FANG_THISCALL* emote_find_fn)(void*, uint32_t);
typedef BYTE* (FANG_THISCALL* emote_create_fn)(void*, const uint32_t*);

typedef struct {
    const char* state;
    uint32_t animation;
} mod_emote;

/* Retained Spore CSA clips, verified from retail ANIM source headers. Names
 * match server/emote's allowlist; these states never replace stock states. */
static const mod_emote emotes[] = {
    {"mod_spore_happy", 0xec71c3bf},
    {"mod_spore_laugh", 0x84362f26},
    {"mod_spore_scared", 0x760f3b4b},
    {"mod_spore_swoon", 0x887a8207},
    {"mod_spore_roar", 0xd40ce7d2},
    {"mod_spore_backflip", 0xca269d94},
    {"mod_spore_flex", 0x938dd2d4},
    {"mod_spore_yawn", 0xfa7cc24b}
};

static emote_find_fn native_find;
static emote_create_fn native_create;

static uint32_t emote_hash(const char* name) {
    uint32_t hash = 0x811c9dc5;
    while (*name) hash = hash * 0x01000193 ^ (unsigned char)*name++;
    return hash;
}

int fang_mod_is_emote(uint32_t state) {
    unsigned int index;
    for (index = 0; index < sizeof(emotes) / sizeof(emotes[0]); ++index)
        if (emote_hash(emotes[index].state) == state) return 1;
    return 0;
}

int fang_mod_prepare_emote(void* manager, uint32_t state) {
    unsigned int index;
    BYTE* entry;
    uintptr_t begin;
    uintptr_t end;
    uintptr_t capacity;
    if (!native_create || !readable_range(manager, 16)) return 0;
    for (index = 0; index < sizeof(emotes) / sizeof(emotes[0]); ++index)
        if (emote_hash(emotes[index].state) == state) break;
    if (index == sizeof(emotes) / sizeof(emotes[0])) return 0;
    entry = native_find(manager, state);
    if (!entry) entry = native_create(manager, &state);
    if (!readable_range(entry, 116)) return 0;
    begin = *(uintptr_t*)(entry + 52);
    end = *(uintptr_t*)(entry + 56);
    capacity = *(uintptr_t*)(entry + 60);
    if (end == begin + 16 && readable_range((void*)begin, 16) &&
        *(uint32_t*)begin == emotes[index].animation) return 1;
    /* Native state construction supplies two inline animation slots. Fill one
     * empty slot; never replace a populated state or borrow stack storage. */
    if (begin != end || capacity < begin || capacity - begin < 16 ||
        !readable_range((void*)begin, 16)) {
        trace_client_state("mod_emote_state_conflict", state);
        return 0;
    }
    *(uint32_t*)begin = emotes[index].animation;
    *(float*)(begin + 4) = 1.f;   /* Selection weight. */
    *(float*)(begin + 8) = -1.f;  /* Native default blend in/out. */
    *(float*)(begin + 12) = -1.f;
    *(uintptr_t*)(entry + 56) = begin + 16;
    *(uint32_t*)(entry + 104) = 0; /* Sequential, once. */
    entry[108] = 1;
    entry[109] = 1;
    entry[110] = 0;
    entry[111] = 0;
    *(float*)(entry + 112) = 1.f;
    trace_client_state("mod_emote_registered", state);
    trace_client_state("mod_emote_clip", emotes[index].animation);
    return 1;
}

void fang_mod_install_emote(BYTE* base) {
    static const BYTE find_signature[] = {0x83, 0xec, 0x08, 0x56, 0x8b, 0xf1};
    static const BYTE create_signature[] = {0x64, 0xa1, 0, 0, 0, 0, 0x6a, 0xff};
    if (!readable_range(base + 0xd8180, sizeof(find_signature)) ||
        !readable_range(base + 0xdc260, sizeof(create_signature)) ||
        memcmp(base + 0xd8180, find_signature, sizeof(find_signature)) != 0 ||
        memcmp(base + 0xdc260, create_signature, sizeof(create_signature)) != 0) {
        trace_client_state("mod_emote_unsupported", 1);
        return;
    }
    native_find = (emote_find_fn)(base + 0xd8180);
    native_create = (emote_create_fn)(base + 0xdc260);
    trace_client_state("mod_emote_enabled", 1);
}

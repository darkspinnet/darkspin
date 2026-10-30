#include "mod/mod.h"
#include "hook.h"

#include <math.h>
#include <stdint.h>
#include <string.h>

/* Build 103: 0x4F3F20 dispatches primary animation states to 0x4DAAE0;
 * 0x4F3F90 dispatches overlays to 0x4DAB20. Keep both native results intact.
 * Own only the sound handles created here, on the native update thread.
 */
typedef void* (FANG_THISCALL* dance_state_fn)(void*, void*, uint32_t, int, float);
typedef void* (FANG_THISCALL* dance_overlay_fn)(void*, void*, uint32_t);
typedef BYTE* (__cdecl* dance_resolve_fn)(uint32_t);
typedef BYTE* (__cdecl* dance_component_fn)(void*);
typedef uint32_t (__cdecl* dance_handle_fn)(void);
typedef int (__cdecl* dance_play_fn)(uint32_t, uint32_t, float, float, float);
typedef int (__cdecl* dance_stop_fn)(uint32_t, int);
typedef int (__cdecl* dance_position_fn)(uint32_t, float, float, float);

typedef struct {
    uint32_t object_id;
    uint32_t state;
    uint32_t handle;
    BYTE* actor;
    BYTE* component;
    BYTE* rig;
    uint32_t key[3];
    float position[3];
} mod_dance;

static dance_state_fn native_state;
static dance_overlay_fn native_overlay;
static dance_resolve_fn native_resolve;
static dance_component_fn native_component;
static dance_handle_fn native_handle;
static dance_play_fn native_play;
static dance_stop_fn native_stop;
static dance_position_fn native_position;
static mod_dance dances[64];
static int is_dance_enabled;

// Reads: returns the case-insensitive hash of an animation-state name.
static uint32_t dance_hash(const char* text) {
    uint32_t hash = 0x811c9dc5;
    while (*text) {
        unsigned char letter = (unsigned char)*text++;
        if (letter >= 'A' && letter <= 'Z') letter += 'a' - 'A';
        hash = hash * 0x01000193 ^ letter;
    }
    return hash;
}

// Reads: checks a state hash against the supported native dance names.
static int dance_is_state(uint32_t state) {
    /* Authored playable-hero dance states from noun_physics. */
    static const char* names[] = {
        "apache", "apache_gun_r", "boyband", "breakdance", "c-walk", "c-walk_gun_m",
        "carlton", "cossack_gun_m", "crank_that", "crank_that_gun_r", "disco1",
        "disco1_gun_r", "disco2", "disco3", "disco3_gun_m", "disco3_gun_r", "disco4",
        "electric_slide", "electric_slide_gun_m", "gaga", "hammer_gun_m", "hammer_gun_r",
        "mj", "moonwalk", "napoleon_dynamite", "pbj", "riverdance", "riverdance_gun_r",
        "riverdance_staff_l", "robot", "runman", "runman_gun_r", "thriller", "twist",
        "worm", "ymca"
    };
    unsigned int index;
    for (index = 0; index < sizeof(names) / sizeof(names[0]); ++index) {
        char name[80] = "emote_dance_";
        strcat(name, names[index]);
        if (dance_hash(name) == state) return 1;
    }
    return 0;
}

// Reads/modifies: resolves a bound animation rig and writes its resource key to the caller output.
static BYTE* dance_rig(BYTE* component, uint32_t* key) {
    BYTE* animator;
    BYTE* rig;
    BYTE* recipe;
    if (!readable_range(component, 332)) return NULL;
    animator = *(BYTE**)(component + 28);
    if (!readable_range(animator, 392)) return NULL;
    rig = *(BYTE**)(animator + 388);
    if (!readable_range(rig, 760)) return NULL;
    recipe = *(BYTE**)rig;
    /* 0x969700 initializes the runtime template's instance/group at +792/+796
     * from the CRT key. Baby flags in the group are not part of its identity. */
    if (!readable_range(recipe, 804)) return NULL;
    key[0] = *(uint32_t*)(recipe + 792);
    key[1] = 0x2b978c46;
    key[2] = *(uint32_t*)(recipe + 796) & 0xe0ffffff;
    return fang_mod_is_bound_key(key) ? rig : NULL;
}

// Modifies: clears a tracked dance, stops its owned native sound handle, and logs the stop.
static void dance_end(mod_dance* dance) {
    uint32_t handle = dance->handle;
    memset(dance, 0, sizeof(*dance));
    if (handle) {
        native_stop(handle, 1);
        trace_client_state("mod_dance_audio_stopped", handle);
    }
}

// Modifies: stops and clears tracked dance audio belonging to an animation component.
static void dance_end_component(BYTE* component) {
    unsigned int index;
    for (index = 0; index < sizeof(dances) / sizeof(dances[0]); ++index)
        if (dances[index].component == component) dance_end(&dances[index]);
}

// Reads/modifies: resolves a bound voice, allocates and plays native audio, stores dance tracking, and logs results.
static void dance_begin(BYTE* component, uint32_t state, enum fang_mod_voice_kind kind) {
    BYTE* actor;
    BYTE* rig;
    uint32_t key[3];
    uint32_t event;
    unsigned int index;
    mod_dance* dance = NULL;
    trace_client_state("mod_dance_animation", state);
    rig = dance_rig(component, key);
    if (!rig) { trace_client_state("mod_dance_bound_rig_missing", 1); return; }
    actor = *(BYTE**)component;
    if (!readable_range(actor, 656) || native_resolve(*(uint32_t*)actor) != actor) return;
    event = fang_mod_vocalization(rig, UINT32_MAX, kind);
    if (!event) { trace_client_state("mod_dance_voice_unmapped", key[0]); return; }
    for (index = 0; index < sizeof(dances) / sizeof(dances[0]); ++index) {
        if (!dances[index].object_id) { dance = &dances[index]; break; }
    }
    if (!dance) { trace_client_state("mod_dance_audio_capacity", 1); return; }
    /* Agent world position is the float3 at +24 (same position used by the
     * client locomotion path). Do not retain references to native objects. */
    memcpy(dance->position, actor + 24, sizeof(dance->position));
    for (index = 0; index < 3; ++index)
        if (!isfinite(dance->position[index])) return;
    dance->handle = native_handle();
    if (!dance->handle) return;
    dance->actor = actor;
    dance->component = component;
    dance->rig = rig;
    dance->object_id = *(uint32_t*)actor;
    dance->state = state;
    memcpy(dance->key, key, sizeof(key));
    native_play(event, dance->handle, dance->position[0], dance->position[1], dance->position[2]);
    trace_client_state("mod_emote_voice_kind", kind);
    trace_client_state("mod_dance_audio_event", event);
    trace_client_state("mod_dance_audio_started", dance->object_id);
}

// Reads/modifies: prepares supported emotes, dispatches native animation changes, and starts or stops tracked dance audio.
static void* FANG_THISCALL mod_dance_state(void* manager, void* component,
    uint32_t state, int is_forced, float scale) {
    if (fang_mod_is_emote(state)) {
        uint32_t key[3];
        if (!dance_rig(component, key) || !fang_mod_prepare_emote(manager, state)) {
            trace_client_state("mod_emote_rejected", state);
            return component;
        }
        trace_client_state("mod_emote_started", state);
    }
    void* result = native_state(manager, component, state, is_forced, scale);
    dance_end_component(component);
    if (dance_is_state(state)) dance_begin(component, state, MOD_VOICE_DANCE);
    else if (state == dance_hash("emote_taunt_all")) dance_begin(component, state, MOD_VOICE_TAUNT);
    return result;
}

// Modifies: dispatches the native overlay and stops tracked dance audio when an overlay starts.
static void* FANG_THISCALL mod_dance_overlay(void* manager, void* component, uint32_t state) {
    void* result = native_overlay(manager, component, state);
    if (state) dance_end_component(component);
    return result;
}

// Reads/modifies: validates tracked actors and animations, updates native sound positions, and stops invalidated dance audio.
void fang_mod_update_dance(void) {
    unsigned int index;
    if (!is_dance_enabled) return;
    for (index = 0; index < sizeof(dances) / sizeof(dances[0]); ++index) {
        mod_dance* dance = &dances[index];
        BYTE* actor;
        BYTE* component;
        uint32_t key[3];
        float position[3];
        unsigned int axis;
        if (!dance->object_id) continue;
        actor = native_resolve(dance->object_id);
        if (actor != dance->actor || !readable_range(actor, 656) ||
            *(uint32_t*)(actor + 172) != dance->state || !*(void**)(actor + 652)) {
            dance_end(dance);
            continue;
        }
        component = native_component(*(void**)(actor + 652));
        if (component != dance->component || dance_rig(component, key) != dance->rig ||
            memcmp(key, dance->key, sizeof(key)) != 0) {
            dance_end(dance);
            continue;
        }
        /* Native primary current/next/requested state words. An internally
         * completed or interrupted animation can leave the network state alone.
         */
        if (*(uint32_t*)(component + 36) != dance->state &&
            *(uint32_t*)(component + 40) != dance->state &&
            *(uint32_t*)(component + 44) != dance->state &&
            *(uint32_t*)(component + 48) != dance->state) {
            dance_end(dance);
            continue;
        }
        memcpy(position, actor + 24, sizeof(position));
        for (axis = 0; axis < 3; ++axis) {
            if (!isfinite(position[axis]) || fabsf(position[axis] - dance->position[axis]) > .01f) break;
        }
        if (axis != 3) { dance_end(dance); continue; }
        native_position(dance->handle, position[0], position[1], position[2]);
    }
}

// Modifies: stores native audio targets, patches animation dispatch calls, and logs or rolls back installation failures.
void fang_mod_install_dance(BYTE* base) {
    static const BYTE resolve_signature[] = {0x8b, 0x44, 0x24, 0x04, 0x50};
    if (!readable_range(base + 0x5d94f0, sizeof(resolve_signature)) ||
        memcmp(base + 0x5d94f0, resolve_signature, sizeof(resolve_signature)) != 0) {
        trace_client_state("mod_dance_audio_unsupported", 1);
        return;
    }
    native_state = (dance_state_fn)(base + 0xdaae0);
    native_overlay = (dance_overlay_fn)(base + 0xdab20);
    native_resolve = (dance_resolve_fn)(base + 0x5d94f0);
    native_component = (dance_component_fn)(base + 0xf3db0);
    native_handle = (dance_handle_fn)(base + 0x25110);
    native_play = (dance_play_fn)(base + 0xdcb10);
    native_stop = (dance_stop_fn)(base + 0x25180);
    native_position = (dance_position_fn)(base + 0xdcbb0);
    if (!patch_call(base + 0xf3f79, native_state, mod_dance_state)) {
        trace_client_state("mod_dance_audio_hook_failed", 1);
        return;
    }
    /* Overlay call address is verified against the native target by patch_call. */
    if (!patch_call(base + 0xf3fe0, native_overlay, mod_dance_overlay)) {
        if (!patch_call(base + 0xf3f79, mod_dance_state, native_state))
            trace_client_state("mod_dance_audio_rollback_failed", 1);
        trace_client_state("mod_dance_audio_hook_failed", 2);
        return;
    }
    is_dance_enabled = 1;
    trace_client_state("mod_dance_audio_enabled", 1);
}

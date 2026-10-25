#include "hook.h"

#include <stdint.h>

typedef void (FANG_THISCALL* preview_idle_fn)(BYTE*, float, float, unsigned int);
typedef unsigned char (FANG_THISCALL* preview_message_fn)(BYTE*, BYTE*);

static BYTE* preview_base;
static preview_idle_fn preview_idle;
static preview_message_fn preview_message;
static unsigned int preview_trace_count;
static BYTE* preview_owner;
static unsigned int preview_actor;
static int is_preview_testing;
static DWORD preview_next_action;
static DWORD preview_observe_after;
static unsigned int preview_sequence;
static uint32_t preview_requested;
static DWORD preview_next_target;
static int is_preview_walking;
static int is_preview_walk_right;
static unsigned int preview_emote;

/* Installed animation catalogue identities only; the native animation loader
 * owns all clip data. Test mode starts playing without requiring a UI action. */
static const uint32_t preview_clips[] = {
    97953726u,  /* Extended creature-editor idle. */
    72529482u,  /* Walk loop. */
    197396817u, /* Disco 2. */
    197653587u, /* Robot. */
    197653584u, /* Thriller. */
    197396829u, /* Moonwalk. */
    201030487u, /* Breakdance. */
    201784887u, /* Twist. */
    149094998u /* Battle roar. */
};

static BYTE* preview_native_actor(BYTE* editor, BYTE** controller, uint32_t* id) {
    BYTE* actor;
    if (!preview_base || !fang_mod_editor_is_scene(editor) ||
        !readable_range(editor, 952) || *(unsigned int*)(editor + 856) != 2 ||
        !fang_mod_editor_preview_ready(editor)) return NULL;
    *controller = *(BYTE**)(editor + 908);
    *id = *(uint32_t*)(editor + 912);
    if (!*id || !readable_range(*controller, 60)) return NULL;
    actor = ((BYTE* (FANG_THISCALL*)(BYTE*, uint32_t))(preview_base + 0x33e930))(*controller, *id);
    if (!readable_range(actor, 400) || !readable_range(*(void**)actor, 92) ||
        (*(void***)actor)[22] != preview_base + 0x541720) return NULL;
    return actor;
}

int fang_mod_editor_is_preview_rig(BYTE* rig) {
    BYTE* controller;
    uint32_t id;
    BYTE* actor = preview_native_actor(fang_mod_editor_active(), &controller, &id);
    return actor && *(BYTE**)(actor + 388) == rig;
}

static void preview_target(BYTE* controller, uint32_t id, float x, float y, float z) {
    /* Native controller target, with interpolation and ground collision;
     * neither teleport the actor nor move the editable creature model. */
    ((void* (FANG_THISCALL*)(BYTE*, uint32_t, float, float, float, unsigned char, unsigned char))
        (preview_base + 0x33ede0))(controller, id, x, y, z, 0, 1);
}

static void preview_walk(BYTE* controller, uint32_t id, DWORD now) {
    if (!is_preview_walking || (LONG)(now - preview_next_target) < 0) return;
    preview_target(controller, id, is_preview_walk_right ? 1.5f : -1.5f, 0, 0);
    is_preview_walk_right = !is_preview_walk_right;
    preview_next_target = now + 2500;
    trace_client_state("mod_editor_preview_walk_target", is_preview_walk_right);
}

static int preview_play(unsigned int action) {
    BYTE* controller;
    uint32_t id;
    BYTE* actor;
    if (action >= sizeof(preview_clips) / sizeof(preview_clips[0])) return 0;
    actor = preview_native_actor(fang_mod_editor_active(), &controller, &id);
    if (!actor) return 0;
    /* 73E9D0 queues behind the current looping idle. The native 73EA40 path
     * creates a track, selects replacement mode (1), then starts it. This is
     * the controller's existing interrupting playback operation. */
    ((void* (FANG_THISCALL*)(BYTE*, uint32_t, uint32_t))
        (preview_base + 0x33ea40))(controller, id, preview_clips[action]);
    if (is_preview_walking && action != 1)
        preview_target(controller, id, *(float*)(actor + 4), *(float*)(actor + 8), *(float*)(actor + 12));
    is_preview_walking = action == 1;
    preview_next_target = GetTickCount();
    preview_walk(controller, id, preview_next_target);
    preview_requested = preview_clips[action];
    preview_observe_after = GetTickCount() + 1000;
    preview_next_action = GetTickCount() + 8000;
    trace_client_state("mod_editor_preview_animation_requested", preview_requested);
    /* Track activation and asset loading can finish on a later frame. Do not
     * label an immediate getter result as proof of successful playback. */
    return 1;
}


int fang_mod_editor_preview_action(unsigned int action) {
    unsigned int count = sizeof(preview_clips) / sizeof(preview_clips[0]);
    unsigned int selected;
    if (action > 2) return 0;
    selected = action;
    if (action == 2) selected = 2 + preview_emote % (count - 2);
    if (!preview_play(selected)) return 0;
    if (action == 2) ++preview_emote;
    return 1;
}

void fang_mod_editor_preview_update(void) {
    BYTE* editor = fang_mod_editor_active();
    BYTE* controller;
    BYTE* actor;
    uint32_t id;
    DWORD now = GetTickCount();
    if (!fang_mod_editor_is_scene(editor) || !readable_range(editor, 952) ||
        *(unsigned int*)(editor + 856) != 2) {
        preview_owner = NULL;
        preview_actor = 0;
        is_preview_testing = 0;
        preview_observe_after = 0;
        preview_next_action = 0;
        preview_sequence = 0;
        is_preview_walking = 0;
        is_preview_walk_right = 1;
        preview_emote = 0;
        return;
    }
    actor = preview_native_actor(editor, &controller, &id);
    if (!actor) return;
    if (preview_owner != editor || preview_actor != id || !is_preview_testing) {
        preview_owner = editor;
        preview_actor = id;
        is_preview_testing = 1;
        preview_sequence = 1;
        preview_next_action = now;
    }
    if (preview_observe_after && (LONG)(now - preview_observe_after) >= 0) {
        uint32_t current = 0;
        float time = 0;
        ((void (FANG_THISCALL*)(BYTE*, uint32_t*, float*, uint32_t*, int*))
            (*(void***)actor)[22])(actor, &current, &time, NULL, NULL);
        trace_client_state("mod_editor_preview_animation_current", current);
        trace_client_state("mod_editor_preview_animation_time_ms", time > 0 && time < 1000000 ? (unsigned int)(time * 1000) : 0);
        if (current != preview_requested)
            trace_client_state("mod_editor_preview_animation_not_active", preview_requested);
        preview_observe_after = 0;
    }
    preview_walk(controller, id, now);
    if ((LONG)(now - preview_next_action) < 0) return;
    if (!preview_play(preview_sequence)) return;
    preview_sequence = (preview_sequence + 1) % (sizeof(preview_clips) / sizeof(preview_clips[0]));
}

int fang_mod_editor_preview_ready(BYTE* editor) {
    BYTE* skin;
    if (!fang_mod_editor_is_scene(editor) || !readable_range(editor, 952) ||
        !editor[813] || !*(void**)(editor + 908) ||
        fang_mod_editor_is_save_busy(editor)) return 0;
    skin = *(BYTE**)(editor + 408);
    /* 71E6D0 -> 6A00A0(skin, 1): the native Test transition requires this
     * completed baked model. Also require its animation-world owner, which
     * 734180 dereferences when leaving Test. Never bypass bake readiness. */
    return readable_range(skin, 32) && *(void**)(skin + 28) != NULL;
}

static unsigned int* preview_enter(BYTE* editor, unsigned int* previous) {
    BYTE* property;
    unsigned int* flag;
    if (!readable_range(editor, 952) || editor != fang_mod_editor_active() ||
        (!fang_mod_editor_is_scene(editor) && *(unsigned int*)(editor + 856) != 0) ||
        !editor[813] || !*(void**)(editor + 176) || !*(void**)(editor + 408) ||
        !*(void**)(editor + 908)) return NULL;
    property = *(BYTE**)(preview_base + 0x105db94);
    if (!readable_range(property, 60) || *(unsigned int*)(property + 52) <= 55) return NULL;
    flag = *(unsigned int**)(property + 56);
    if (!readable_range(flag, 56 * sizeof(*flag))) return NULL;
    flag += 55;
    *previous = *flag;
    /* Imported scenes and explicit Mod Body mode need native bake-completion
     * handling as well as idle display. 726870 and 72AE10 share fast property
     * slot55. Scope that gate to the
     * retained native operation, including its synchronous animation events;
     * restore it before returning to the player editor's frame dispatcher.
     * Do not change property defaults, preferences, or other editor entries. */
    *flag = 1;
    if (preview_trace_count++ < 4) trace_client_state("mod_editor_preview_animation_gate", *previous);
    return flag;
}

static void FANG_THISCALL preview_update_idle(BYTE* editor, float x, float y, unsigned int elapsed) {
    unsigned int previous = 0;
    unsigned int* flag = preview_enter(editor, &previous);
    preview_idle(editor, x, y, elapsed);
    if (flag) *flag = previous;
}

static unsigned char FANG_THISCALL preview_handle_message(BYTE* editor, BYTE* message) {
    unsigned int previous = 0;
    unsigned int* flag = preview_enter(editor, &previous);
    unsigned char result = preview_message(editor, message);
    if (flag) *flag = previous;
    return result;
}

void fang_mod_install_editor_preview(BYTE* base) {
    preview_base = base;
    preview_idle = (preview_idle_fn)(base + 0x326870);
    preview_message = (preview_message_fn)(base + 0x32ae10);
    if (!patch_call(base + 0x337068, preview_idle, preview_update_idle)) {
        trace_client_state("mod_editor_preview_idle_hook_failed", 1);
        return;
    }
    if (!patch_call(base + 0x336f63, preview_message, preview_handle_message)) {
        if (!patch_call(base + 0x337068, preview_update_idle, preview_idle))
            trace_client_state("mod_editor_preview_rollback_failed", 1);
        trace_client_state("mod_editor_preview_message_hook_failed", 1);
        return;
    }
    trace_client_state("mod_editor_preview_enabled", 1);
}

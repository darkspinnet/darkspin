#include "hook.h"

#include <stdint.h>

typedef unsigned char (FANG_THISCALL* skin_mode_fn)(BYTE*, unsigned int);

static BYTE* skin_base;
static skin_mode_fn skin_native_mode;
static unsigned int skin_trace_count;
static BYTE* skin_hover_owner;
static unsigned int skin_hover_actor;
static int is_skin_hover_visible;

typedef void (FANG_THISCALL* skin_render_groups_fn)(BYTE*, const uint32_t*, const uint32_t*, unsigned int);

static int skin_show_hover(BYTE* editor, int is_visible) {
    BYTE* world = *(BYTE**)(editor + 136);
    BYTE* controller = *(BYTE**)(editor + 908);
    BYTE* actor;
    BYTE* rig;
    uint32_t first_draw_groups[2];
    uint32_t second_draw_groups[2];
    uint32_t empty_groups[2] = {0, 0};
    unsigned int group_offset;
    skin_render_groups_fn set_groups;
    if (!readable_range(world, 4) || !readable_range(*(void**)world, 304) ||
        (*(void***)world)[75] != (void*)(skin_base + 0x4ac0b0)) return 0;
    if (is_visible) {
        if (!readable_range(controller, 60) || !*(unsigned int*)(editor + 912)) return 0;
        actor = ((BYTE* (FANG_THISCALL*)(BYTE*, unsigned int))(skin_base + 0x33e930))
            (controller, *(unsigned int*)(editor + 912));
        if (!readable_range(actor, 396)) return 0;
        rig = *(BYTE**)(actor + 392);
        if (!readable_range(rig, 8) ||
            (*(uint32_t*)(rig + 4) & 0x13001) != 0x3001) return 0;
    }
    /* 721B10 selects these same two draw sets. Slot 51 is SetEffectsRunning,
     * not mesh visibility; toggling it never selected the painted geometry.
     * Override only render groups, preserving 940/941, picking and manipulators.
     * Keep the native overlay groups so bone highlights remain visible. */
    group_offset = is_visible || editor[940] ? 644 : 676;
    first_draw_groups[0] = *(uint32_t*)(editor + group_offset) | *(uint32_t*)(editor + 692);
    first_draw_groups[1] = *(uint32_t*)(editor + group_offset + 4) | *(uint32_t*)(editor + 696);
    second_draw_groups[0] = *(uint32_t*)(editor + group_offset + 8) | *(uint32_t*)(editor + 692);
    second_draw_groups[1] = *(uint32_t*)(editor + group_offset + 12) | *(uint32_t*)(editor + 696);
    set_groups = (skin_render_groups_fn)(*(void***)world)[75];
    set_groups(world, first_draw_groups, empty_groups, 0);
    set_groups(world, second_draw_groups, empty_groups, 1);
    return 1;
}

void fang_mod_editor_update_skin(void) {
    BYTE* editor = fang_mod_editor_active();
    BYTE* skin;
    unsigned int actor;
    int is_hover;
    if (!skin_base || !readable_range(editor, 952)) {
        skin_hover_owner = NULL;
        is_skin_hover_visible = 0;
        return;
    }
    actor = *(unsigned int*)(editor + 912);
    if (skin_hover_owner != editor || skin_hover_actor != actor) {
        skin_hover_owner = editor;
        skin_hover_actor = actor;
        is_skin_hover_visible = 0;
    }
    skin = *(BYTE**)(editor + 408);
    is_hover = fang_mod_editor_is_body(editor) && editor[813] &&
        readable_range(skin, 32) && *(void**)(skin + 28) && actor &&
        *(void**)(editor + 224) && !*(void**)(editor + 400) &&
        !*(unsigned int*)(editor + 948) && !(GetAsyncKeyState(VK_LBUTTON) & 0x8000);
    if (is_hover) {
        /* Native editor updates can restore draw groups every frame. */
        if (skin_show_hover(editor, 1)) {
            if (!is_skin_hover_visible) trace_client_state("mod_editor_body_hover_skin", 1);
            is_skin_hover_visible = 1;
        }
    } else if (!is_hover && is_skin_hover_visible) {
        /* Real manipulation uses the live geometry. Never leave a stale baked
         * silhouette over a drag; native baking restores it after the edit. */
        if (*(unsigned int*)(editor + 856) == 0) skin_show_hover(editor, 0);
        is_skin_hover_visible = 0;
        trace_client_state("mod_editor_body_hover_skin", 0);
    }
}

static unsigned char FANG_THISCALL skin_build_mode(BYTE* editor, unsigned int mode) {
    BYTE* caller = (BYTE*)__builtin_return_address(0);
    /* Slot 25 also controls picking, manipulators and animation interruption.
     * Keep Body as Build at those callers. In the skin lifecycle and the
     * baked-display creator, use the same skin branch as Paint: 729CE0 would
     * otherwise reject Body before creating editor+912, and its later query
     * would configure a non-animated editing instance instead of the display
     * instance. Both queries must agree with 734180's lifecycle decisions. */
    if ((caller == skin_base + 0x3343a5 || caller == skin_base + 0x334433 ||
         caller == skin_base + 0x334442 || caller == skin_base + 0x329d05 ||
         caller == skin_base + 0x329efc) && mode == 0 &&
        editor == fang_mod_editor_active() && editor[813] && *(void**)(editor + 408)) {
        if (skin_trace_count++ < 16) trace_client_state("mod_editor_body_skin_transition", 1);
        return 0;
    }
    return skin_native_mode(editor, mode);
}

void fang_mod_install_editor_skin(BYTE* base) {
    skin_base = base;
    skin_native_mode = (skin_mode_fn)(base + 0xb82e0);
    if (!readable_range(base + 0xbd9934, 4) ||
        !readable_range(base + 0x3343a3, 2) ||
        !readable_range(base + 0x334431, 2) ||
        !readable_range(base + 0x334440, 2) ||
        !readable_range(base + 0x329d03, 2) ||
        !readable_range(base + 0x329efa, 2) ||
        base[0x3343a3] != 0xff || base[0x3343a4] != 0xd0 ||
        base[0x334431] != 0xff || base[0x334432] != 0xd2 ||
        base[0x334440] != 0xff || base[0x334441] != 0xd2 ||
        base[0x329d03] != 0xff || base[0x329d04] != 0xd2 ||
        base[0x329efa] != 0xff || base[0x329efb] != 0xd2 ||
        !patch_pointer((void**)(base + 0xbd9934), skin_native_mode, skin_build_mode)) {
        trace_client_state("mod_editor_body_skin_hook_failed", 1);
        return;
    }
    trace_client_state("mod_editor_body_skin_enabled", 1);
}

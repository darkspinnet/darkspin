#include "hook.h"

#include <stdint.h>
#include <string.h>
#include <wchar.h>

/* The explicit Mod entry keeps the player-editor subclass, request identity,
 * inventory, validation and save callbacks. Only this editor instance receives
 * the general creator's additional body manipulators. No shared property list
 * or packaged UI is changed. Build 103: 4B9106 -> 72DCE0; request at editor+524.
 */
typedef int (FANG_THISCALL* editor_config_fn)(BYTE*);
typedef void* (FANG_THISCALL* editor_resize_fn)(void*, unsigned int);
typedef BYTE* (__cdecl* editor_manager_fn)(void);
typedef BYTE* (FANG_THISCALL* editor_agent_fn)(BYTE*, uint32_t);
typedef BYTE* (__cdecl* editor_creature_fn)(uint32_t, uint32_t);
typedef void (__cdecl* editor_edit_fn)(BYTE*);
typedef BYTE* (FANG_THISCALL* editor_hit_fn)(BYTE*, BYTE*, float, float, unsigned int);
typedef void (FANG_THISCALL* editor_exit_fn)(BYTE*);
typedef void (FANG_THISCALL* editor_part_flag_fn)(BYTE*, unsigned int, unsigned char);
typedef void (FANG_THISCALL* editor_ref_fn)(BYTE*);
typedef void (FANG_THISCALL* editor_glow_fn)(BYTE*, float, float, float, float);

typedef struct editor_part {
    BYTE* part;
    unsigned char is_locked;
} editor_part;

static BYTE* editor_base;
static editor_config_fn editor_config;
static uint32_t editor_identity[3];
static DWORD editor_deadline;
static int is_editor_pending;
static int is_editor_enabled;
static editor_edit_fn editor_native_edit;
static BYTE* active_editor;
static editor_hit_fn editor_native_hit;
static editor_hit_fn editor_body_hit;
static editor_exit_fn editor_native_exit;
static editor_part parts[256];
static unsigned int part_count;
static unsigned int editor_hit_count;
static BYTE editor_pick_flags[2];
static int is_body_picking;
static BYTE* editor_tuning;
static float editor_colors[4][4];
static editor_glow_fn editor_native_glow;
static unsigned int editor_glow_count;

BYTE* fang_mod_editor_active(void) {
    if (!is_editor_enabled || !readable_range(active_editor, 1172)) return NULL;
    return active_editor;
}

static void FANG_THISCALL editor_body_glow(BYTE* part, float red, float green, float blue, float alpha) {
    BYTE* editor = fang_mod_editor_active();
    if (editor && *(unsigned int*)(editor + 856) == 0 &&
        readable_range(part, 68) && *(uint64_t*)(part + 40) == 0 &&
        *(BYTE**)(part + 48) == *(BYTE**)(editor + 176)) {
        /* 6A8130 creates the native temporary highlight mesh. Its RGB is
         * presentation state, separate from the creature's paint recipe.
         * Preserve native activation, opacity, and teardown on rollover exit. */
        int is_selected = part[64] != 0;
        /* Keep the part highlight darker than cyan bones and white joints. */
        red = is_selected ? 0.15f : 0.08f;
        green = is_selected ? 0.4f : 0.25f;
        blue = is_selected ? 0.6f : 0.4f;
        if (editor_glow_count++ < 16)
            trace_client_state("mod_editor_body_glow", is_selected ? 2 : 1);
    }
    editor_native_glow(part, red, green, blue, alpha);
}

static void editor_install_glow(BYTE* base) {
    static const unsigned int calls[] = {0x2a961e, 0x2a9699};
    unsigned int index;
    editor_native_glow = (editor_glow_fn)(base + 0x2a8130);
    for (index = 0; index < sizeof(calls) / sizeof(calls[0]); ++index) {
        if (!patch_call(base + calls[index], editor_native_glow, editor_body_glow)) break;
    }
    if (index == sizeof(calls) / sizeof(calls[0])) {
        trace_client_state("mod_editor_glow_enabled", 1);
        return;
    }
    while (index) {
        --index;
        if (!patch_call(base + calls[index], editor_body_glow, editor_native_glow))
            trace_client_state("mod_editor_glow_rollback_failed", index + 1);
    }
    trace_client_state("mod_editor_glow_hook_failed", 1);
}

static void editor_restore_colors(void) {
    if (!editor_tuning) return;
    if (*(BYTE**)(editor_base + 0x1046a94) == editor_tuning &&
        readable_range(editor_tuning + 32, sizeof(editor_colors)))
        memcpy(editor_tuning + 32, editor_colors, sizeof(editor_colors));
    editor_tuning = NULL;
    trace_client_state("mod_editor_overlay_palette", 0);
}

static void editor_update_colors(int is_build) {
    static const float colors[4][4] = {
        {0.1f, 0.9f, 1.f, 0.8f},   /* Torso rollover. */
        {0.1f, 0.9f, 1.f, 0.95f},  /* Selected torso/manipulator. */
        {1.f, 1.f, 1.f, 1.f},      /* Highlighted vertebra. */
        {0.1f, 0.9f, 1.f, 0.f}     /* Native fade-to-hidden target. */
    };
    BYTE* tuning;
    if (!is_build) {
        editor_restore_colors();
        return;
    }
    tuning = *(BYTE**)(editor_base + 0x1046a94);
    if (tuning == editor_tuning) return;
    editor_restore_colors();
    if (!readable_range(tuning, 96)) return;
    /* 771390 loads these four RGBA states; 71F1D0 applies their native
     * hover/selection and fade logic. Keep its timing and skin materials.
     * This singleton belongs to the current editor, so restore its palette
     * whenever the explicit Mod Body entry loses ownership. */
    memcpy(editor_colors, tuning + 32, sizeof(editor_colors));
    memcpy(tuning + 32, colors, sizeof(colors));
    editor_tuning = tuning;
    trace_client_state("mod_editor_overlay_palette", 1);
}

static void editor_restore_parts(void) {
    while (part_count) {
        editor_part* part = &parts[--part_count];
        ((editor_part_flag_fn)(editor_base + 0x2a25e0))(part->part, 1, part->is_locked);
        ((editor_ref_fn)(*(void***)part->part)[2])(part->part);
        memset(part, 0, sizeof(*part));
    }
}

void fang_mod_editor_update_body(void) {
    BYTE* editor = fang_mod_editor_active();
    int is_build;
    if (!editor) {
        editor_restore_colors();
        editor_restore_parts();
        return;
    }
    is_build = *(unsigned int*)(editor + 856) == 0;
    editor_update_colors(is_build);
    /* 723730 excludes the body-control render groups at +750/+751 and
     * skips the bone controller's 6A00C0 ray test entirely when +751 is set.
     * Enabling manipulator IDs alone cannot make those controls pickable.
     * Keep the authored player-editor filters in all other modes. */
    editor[750] = is_build ? 0 : editor_pick_flags[0];
    editor[751] = is_build ? 0 : editor_pick_flags[1];
    if (is_build != is_body_picking) {
        is_body_picking = is_build;
        trace_client_state("mod_editor_body_picking", is_build);
    }
    if (!is_build) editor_restore_parts();
}

static void FANG_THISCALL editor_leave(BYTE* editor) {
    if (active_editor == editor) {
        fang_mod_editor_close_body_ui();
        editor_restore_parts();
        editor_restore_colors();
        editor[750] = editor_pick_flags[0];
        editor[751] = editor_pick_flags[1];
        is_body_picking = 0;
        active_editor = NULL;
    }
    editor_native_exit(editor);
}

/* 4BA7A0 calls the normal creature hit test, then discards non-inventory
 * parts absent from the hero's authored chassis whitelist. Imported torsos
 * and limbs fail that whitelist even with manipulators enabled. In explicit
 * Build mode only, retain the base hit test and unlock the hit core part's
 * UI selection flag. Inventory parts and every other native mode stay native.
 * Both build-103 routines take four stack arguments and return with RET 16.
 */
static BYTE* FANG_THISCALL editor_hit(BYTE* editor, BYTE* hit, float x, float y, unsigned int flags) {
    BYTE* result;
    BYTE* part;
    unsigned int index;
    if (editor != fang_mod_editor_active() || *(unsigned int*)(editor + 856) != 0)
        return editor_native_hit(editor, hit, x, y, flags);
    fang_mod_editor_update_body();
    result = editor_body_hit(editor, hit, x, y, flags);
    if (!readable_range(hit, 28)) return result;
    part = *(BYTE**)hit;
    if (!readable_range(part, 3236) ||
        *(uint64_t*)(part + 40) != 0 || *(BYTE**)(part + 48) != *(BYTE**)(editor + 176)) return result;
    for (index = 0; index < part_count; ++index) {
        if (parts[index].part == part) return result;
    }
    if (part_count == sizeof(parts) / sizeof(parts[0])) return result;
    if (!(*(unsigned int*)(part + 3228) & 2)) return result;
    ((editor_ref_fn)(*(void***)part)[1])(part);
    parts[part_count].part = part;
    parts[part_count++].is_locked = 1;
    ((editor_part_flag_fn)(editor_base + 0x2a25e0))(part, 1, 0);
    if (editor_hit_count++ < 16) trace_client_state("mod_editor_body_part_unlocked", part_count);
    return result;
}

static BYTE* editor_collection(void) {
    BYTE* controller = *(BYTE**)(editor_base + 0xdbeaa0);
    if (!readable_range(controller, 0xd10) ||
        *(void**)controller != editor_base + 0xbce048 || !controller[920]) return NULL;
    return controller;
}

/* +12 in a context-menu record is a position, not a resource key. Resolve
 * the current actor each time: scrolling can reuse a menu for another hero.
 * 417AA0 uses the actor's definition+92 as its catalogue noun; 41B4F0 looks
 * up the owned creature with actor+8/+12 before opening the player editor. */
int fang_mod_editor_target(BYTE* context) {
    BYTE* manager;
    BYTE* agent;
    BYTE* definition;
    BYTE* creature;
    if (!is_editor_enabled || !editor_collection() || !readable_range(context, 72) || !context[56]) return 0;
    manager = ((editor_manager_fn)(editor_base + 0x5bc740))();
    if (!readable_range(manager, 8)) return 0;
    agent = ((editor_agent_fn)(editor_base + 0x5d1730))(manager, *(uint32_t*)(context + 8));
    if (!readable_range(agent, 16)) return 0;
    definition = *(BYTE**)(agent + 4);
    if (!readable_range(definition, 96)) return 0;
    if (!*(uint32_t*)(definition + 92)) return 0;
    creature = ((editor_creature_fn)(editor_base + 0xe63a0))(
        *(uint32_t*)(agent + 8), *(uint32_t*)(agent + 12));
    return readable_range(creature, 64);
}

void __cdecl fang_mod_editor_open(BYTE* context) {
    BYTE* manager;
    BYTE* agent;
    BYTE* creature;
    BYTE* controller;
    is_editor_pending = 0;
    if (!fang_mod_editor_target(context)) return;
    manager = ((editor_manager_fn)(editor_base + 0x5bc740))();
    agent = ((editor_agent_fn)(editor_base + 0x5d1730))(manager, *(uint32_t*)(context + 8));
    if (!readable_range(agent, 16)) return;
    creature = ((editor_creature_fn)(editor_base + 0xe63a0))(
        *(uint32_t*)(agent + 8), *(uint32_t*)(agent + 12));
    if (!readable_range(creature, 64)) return;
    memcpy(editor_identity, creature, sizeof(editor_identity));
    /* Native Edit retains the account permission check and fade/transition. */
    editor_native_edit(context);
    controller = editor_collection();
    if (!controller || *(uint32_t*)(controller + 3320) != *(uint32_t*)(context + 8)) return;
    editor_deadline = GetTickCount() + 30000;
    is_editor_pending = 1;
    trace_client_state("mod_editor_requested", editor_identity[0]);
}

static void __cdecl editor_standard_open(BYTE* context) {
    is_editor_pending = 0;
    editor_native_edit(context);
}

static int FANG_THISCALL editor_configure(BYTE* editor) {
    static const uint32_t body_manipulators[] = {
        0x8665f54d, 0xe931544d, 0xa9910288, 0x5fde642e, 0xbb46d99a,
        0x889cdbff, 0x9ed74a02, 0x9f48c094, 0x855de44d, 0x7ad67439
    };
    uint32_t manipulators[64];
    uint32_t* begin;
    uint32_t* end;
    BYTE* request;
    unsigned int count;
    unsigned int index;
    unsigned int cursor;
    int result = editor_config(editor);
    int is_requested = is_editor_pending;
    is_editor_pending = 0; /* A request cannot leak into a later editor. */
    if (!is_requested || (LONG)(GetTickCount() - editor_deadline) >= 0 ||
        !readable_range(editor, 1170)) return result;
    request = *(BYTE**)(editor + 524);
    if (!readable_range(request, 120) ||
        memcmp(request + 104, editor_identity, sizeof(editor_identity)) != 0) {
        trace_client_state("mod_editor_request_mismatch", 1);
        return result;
    }
    begin = *(uint32_t**)(editor + 860);
    end = *(uint32_t**)(editor + 864);
    if ((uintptr_t)end < (uintptr_t)begin ||
        ((uintptr_t)end - (uintptr_t)begin) % 4 != 0 ||
        (uintptr_t)end - (uintptr_t)begin > 54 * sizeof(uint32_t)) return result;
    count = ((uintptr_t)end - (uintptr_t)begin) / sizeof(uint32_t);
    if (count && !readable_range(begin, count * sizeof(uint32_t))) return result;
    if (count) memcpy(manipulators, begin, count * sizeof(uint32_t));
    for (index = 0; index < sizeof(body_manipulators) / sizeof(body_manipulators[0]); ++index) {
        for (cursor = 0; cursor < count; ++cursor) {
            if (manipulators[cursor] == body_manipulators[index]) break;
        }
        if (cursor == count) manipulators[count++] = body_manipulators[index];
    }
    /* Use the native EASTL owner/allocator; its destructor frees this vector. */
    ((editor_resize_fn)(editor_base + 0x32c990))(editor + 860, count);
    begin = *(uint32_t**)(editor + 860);
    end = *(uint32_t**)(editor + 864);
    if ((uintptr_t)end - (uintptr_t)begin != count * sizeof(uint32_t) ||
        !readable_range(begin, count * sizeof(uint32_t))) return result;
    memcpy(begin, manipulators, count * sizeof(uint32_t));
    editor[746] = 1; /* editorShowVertebrae */
    editor[1169] = 1; /* editorShowBoneLengthHandles */
    editor_pick_flags[0] = editor[750];
    editor_pick_flags[1] = editor[751];
    is_body_picking = 0;
    active_editor = editor;
    trace_client_state("mod_editor_body_controls", count);
    return result;
}

void fang_mod_install_editor(BYTE* base, const wchar_t* manifest) {
    static const BYTE agent_signature[] = {0x8b, 0x49, 0x04, 0xe9};
    static const BYTE config_signature[] = {0x83, 0xec, 0x08, 0x56, 0x8b, 0xf1, 0xe8};
    if (GetPrivateProfileIntW(L"mod", L"creature_editor", 0, manifest) != 1) return;
    if (sizeof(void*) != 4 || !readable_range(base + 0xdbeaa0, 4) ||
        !readable_range(base + 0x5d1730, sizeof(agent_signature)) ||
        memcmp(base + 0x5d1730, agent_signature, sizeof(agent_signature)) != 0 ||
        !readable_range(base + 0xb9100, sizeof(config_signature)) ||
        memcmp(base + 0xb9100, config_signature, sizeof(config_signature)) != 0 ||
        !readable_range(base + 0x19fcb, 5) || base[0x19fcb] != 0x68 ||
        *(void**)(base + 0x19fcc) != base + 0xe920 ||
        !readable_range(base + 0xbd99d0, 4) || *(void**)(base + 0xbd99d0) != base + 0xba7a0 ||
        !readable_range(base + 0xbd98fc, 4) || *(void**)(base + 0xbd98fc) != base + 0xb9470) {
        trace_client_state("mod_editor_unsupported", 1);
        return;
    }
    editor_base = base;
    editor_native_edit = (editor_edit_fn)(base + 0xe920);
    editor_native_hit = (editor_hit_fn)(base + 0xba7a0);
    editor_body_hit = (editor_hit_fn)(base + 0x323730);
    editor_native_exit = (editor_exit_fn)(base + 0xb9470);
    editor_config = (editor_config_fn)(base + 0x32dce0);
    if (!patch_call(base + 0xb9106, editor_config, editor_configure)) {
        trace_client_state("mod_editor_config_hook_failed", 1);
        return;
    }
    /* This PUSH operand registers ordinary Edit, not our additional entry.
     * Cancel an armed body request if the player chooses ordinary Edit during
     * the fade, including when both buttons refer to the same hero. */
    if (!patch_pointer((void**)(base + 0x19fcc), base + 0xe920, editor_standard_open)) {
        if (!patch_call(base + 0xb9106, editor_configure, editor_config))
            trace_client_state("mod_editor_config_rollback_failed", 1);
        trace_client_state("mod_editor_standard_hook_failed", 1);
        return;
    }
    FlushInstructionCache(GetCurrentProcess(), base + 0x19fcb, 5);
    if (!patch_pointer((void**)(base + 0xbd99d0), editor_native_hit, editor_hit)) goto rollback_standard;
    if (!patch_pointer((void**)(base + 0xbd98fc), editor_native_exit, editor_leave)) goto rollback_hit;
    if (!fang_mod_install_editor_ui(base)) {
        trace_client_state("mod_editor_ui_hook_failed", 1);
        if (!patch_pointer((void**)(base + 0xbd98fc), editor_leave, editor_native_exit))
            trace_client_state("mod_editor_exit_rollback_failed", 1);
        goto rollback_hit;
    }
    is_editor_enabled = 1;
    editor_install_glow(base);
    fang_mod_install_validation(base);
    trace_client_state("mod_editor_enabled", 1);
    trace_client_state("mod_editor_owned_heroes", 1);
    return;
rollback_hit:
    if (!patch_pointer((void**)(base + 0xbd99d0), editor_hit, editor_native_hit))
        trace_client_state("mod_editor_hit_rollback_failed", 1);
rollback_standard:
    if (!patch_pointer((void**)(base + 0x19fcc), editor_standard_open, base + 0xe920))
        trace_client_state("mod_editor_standard_rollback_failed", 1);
    FlushInstructionCache(GetCurrentProcess(), base + 0x19fcb, 5);
    if (!patch_call(base + 0xb9106, editor_configure, editor_config))
        trace_client_state("mod_editor_config_rollback_failed", 1);
    trace_client_state("mod_editor_body_hook_failed", 1);
}

#include "hook.h"

#include <stdint.h>

/* Retain PlayerEditor, owned hero request, camera, inventory and save flow.
 * Only its explicit Mod Creature Edit instance selects the privately mounted
 * Spore backdrop and lighting through the inherited background loader.
 * The player level remains available if the imported model cannot instantiate.
 */
typedef int (__cdecl* scene_property_fn)(void*, uint32_t, uint32_t*);
typedef unsigned char (FANG_THISCALL* scene_background_fn)(BYTE*);
typedef int (__cdecl* scene_request_fn)(int, uint32_t, uint32_t, uint32_t, void*);
typedef BYTE* (__cdecl* scene_manager_fn)(void);
typedef BYTE* (FANG_THISCALL* scene_world_fn)(BYTE*, uint32_t);
typedef unsigned char (FANG_THISCALL* scene_visible_fn)(BYTE*);
typedef void (FANG_THISCALL* scene_visibility_fn)(BYTE*, unsigned char);
typedef void (FANG_THISCALL* scene_ref_fn)(BYTE*);

static BYTE* scene_base;
static scene_property_fn scene_property;
static scene_background_fn scene_background;
static BYTE* scene_loading;
static int is_scene_enabled;
static scene_request_fn scene_request;
static BYTE* scene_world;
static BYTE* scene_world_owner;
static unsigned char is_world_visible;

int fang_mod_scene_available(void) {
    return is_scene_enabled;
}

static void scene_restore_world(void) {
    if (!scene_world) return;
    ((scene_visibility_fn)(*(void***)scene_world)[81])(scene_world, is_world_visible);
    ((scene_ref_fn)(*(void***)scene_world)[1])(scene_world);
    scene_world = NULL;
    scene_world_owner = NULL;
    trace_client_state("mod_editor_scene_room_hidden", 0);
}

void fang_mod_scene_leave(BYTE* editor) {
    if (editor == scene_world_owner) scene_restore_world();
}

void fang_mod_update_scene(void) {
    BYTE* editor = fang_mod_editor_active();
    BYTE* manager;
    BYTE* world;
    if (!is_scene_enabled || !fang_mod_editor_is_scene(editor) || !*(void**)(editor + 192)) {
        scene_restore_world();
        return;
    }
    manager = ((scene_manager_fn)(scene_base + 0x3aec30))();
    if (!readable_range(manager, 4) || !readable_range(*(void**)manager, 32)) return;
    /* 4F10E0 names this GameModelWorld; 4F7F30 hides it during editor exit.
     * Runtime observation confirms it differs from the editor creature and
     * backdrop worlds (+136/+144). Keep loading and updating the level, but
     * hide its room/platform geometry only after our backdrop instantiated. */
    world = ((scene_world_fn)(*(void***)manager)[7])(manager, 112776369);
    if (!readable_range(world, 4) || !readable_range(*(void**)world, 332) ||
        world == *(BYTE**)(editor + 136) || world == *(BYTE**)(editor + 144)) return;
    if (world != scene_world || editor != scene_world_owner) {
        scene_restore_world();
        ((scene_ref_fn)(*(void***)world)[0])(world);
        scene_world = world;
        scene_world_owner = editor;
        is_world_visible = ((scene_visible_fn)(*(void***)world)[82])(world);
        trace_client_state("mod_editor_scene_room_hidden", 1);
    }
    ((scene_visibility_fn)(*(void***)world)[81])(world, 0);
}

static int __cdecl scene_switch_request(int is_user, uint32_t instance,
    uint32_t type, uint32_t group, void* source) {
    /* 44A160's owned-hero switch message creates a fresh editor request.
     * Arm its exact identity before native teardown/configuration runs. */
    if (is_user) fang_mod_editor_continue_scene(instance, type, group);
    return scene_request(is_user, instance, type, group, source);
}

static int __cdecl scene_select_property(void* property, uint32_t id, uint32_t* result) {
    if (fang_mod_editor_is_scene(scene_loading) &&
        *(void**)(scene_loading + 40) == property && result) {
        if (id == 0x021b35a0) {
            *result = FANG_EDITOR_SCENE_MODEL;
            trace_client_state("mod_editor_scene_model_selected", *result);
            return 1;
        }
        if (id == 0x026f337b) {
            *result = FANG_EDITOR_SCENE_LIGHT;
            return 1;
        }
    }
    return scene_property(property, id, result);
}

static unsigned char FANG_THISCALL scene_load_background(BYTE* editor) {
    unsigned char result;
    BYTE* previous = scene_loading;
    if (fang_mod_editor_is_scene(editor)) scene_loading = editor;
    result = scene_background(editor);
    if (scene_loading == editor) {
        trace_client_state("mod_editor_scene_model_loaded", *(void**)(editor + 192) != NULL);
        trace_client_state("mod_editor_scene_light", *(uint32_t*)(editor + 700));
    }
    scene_loading = previous;
    return result;
}

void fang_mod_install_scene(BYTE* base) {
    static const unsigned int calls[] = {0x320d67, 0x320eb8, 0x320ee9};
    unsigned int index;
    if (!readable_range(base + 0xbd9994, 4) ||
        *(void**)(base + 0xbd9994) != base + 0x320be0) {
        trace_client_state("mod_editor_scene_unsupported", 1);
        return;
    }
    scene_base = base;
    scene_property = (scene_property_fn)(scene_base + 0x3b47c0);
    scene_background = (scene_background_fn)(scene_base + 0x320be0);
    scene_request = (scene_request_fn)(scene_base + 0xe8610);
    for (index = 0; index < sizeof(calls) / sizeof(calls[0]); ++index) {
        if (!patch_call(base + calls[index], scene_property, scene_select_property)) break;
    }
    if (index != sizeof(calls) / sizeof(calls[0])) goto rollback;
    if (!patch_call(base + 0x4a248, scene_request, scene_switch_request)) goto rollback;
    if (patch_pointer((void**)(base + 0xbd9994), scene_background, scene_load_background)) {
        is_scene_enabled = 1;
        trace_client_state("mod_editor_scene_enabled", 1);
        return;
    }
    if (!patch_call(base + 0x4a248, scene_switch_request, scene_request))
        trace_client_state("mod_editor_scene_switch_rollback_failed", 1);
rollback:
    while (index) {
        --index;
        if (!patch_call(base + calls[index], scene_select_property, scene_property))
            trace_client_state("mod_editor_scene_rollback_failed", index + 1);
    }
    trace_client_state("mod_editor_scene_hook_failed", 1);
}

#include "mod/mod.h"
#include "hook.h"

#include <stdint.h>

/* Retain PlayerEditor, owned hero request, inventory and save flow. Its
 * explicit Import Editor instance starts the source editor's private entry
 * effect through the inherited scene loader. The old backdrop remains the
 * fallback when the source directory cannot prepare or start.
 */
typedef int (__cdecl* scene_property_fn)(void*, uint32_t, uint32_t*);
typedef unsigned char (FANG_THISCALL* scene_background_fn)(BYTE*);
typedef int (__cdecl* scene_request_fn)(int, uint32_t, uint32_t, uint32_t, void*);
typedef BYTE* (__cdecl* scene_manager_fn)(void);
typedef BYTE* (FANG_THISCALL* scene_world_fn)(BYTE*, uint32_t);
typedef unsigned char (FANG_THISCALL* scene_visible_fn)(BYTE*);
typedef void (FANG_THISCALL* scene_visibility_fn)(BYTE*, unsigned char);
typedef void (FANG_THISCALL* scene_ref_fn)(BYTE*);
typedef void (FANG_THISCALL* scene_render_fn)(BYTE*, unsigned int, unsigned int, void*, void*);

static BYTE* scene_base;
static scene_property_fn scene_property;
static scene_background_fn scene_background;
static BYTE* scene_loading;
static int is_scene_enabled;
static scene_request_fn scene_request;
static BYTE* scene_world;
static BYTE* scene_world_owner;
static unsigned char is_world_visible;
static scene_render_fn scene_native_render;
static BYTE* scene_render_owner;

static void FANG_THISCALL scene_render_level(BYTE* renderer, unsigned int flags,
    unsigned int pass, void* camera, void* target) {
    BYTE* editor = fang_mod_editor_active();
    /* The PlayerEditor pipeline's renderModels callback draws the loaded
     * lobby level's static-model octree (491520 -> 9F7CB0). It does not use
     * GameModelWorld visibility. Creature animation and the inherited
     * backdrop world have separate rendering paths. Gate just this call. */
    if (fang_mod_editor_is_scene(editor) &&
        (fang_mod_environment_active(editor) || *(void**)(editor + 192))) {
        if (scene_render_owner != editor) {
            scene_render_owner = editor;
            trace_client_state("mod_editor_scene_level_render_hidden", 1);
        }
        return;
    }
    scene_render_owner = NULL;
    scene_native_render(renderer, flags, pass, camera, target);
}

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
    fang_mod_environment_leave(editor);
    if (editor == scene_world_owner) scene_restore_world();
    if (editor == scene_render_owner) scene_render_owner = NULL;
}

void fang_mod_update_scene(void) {
    BYTE* editor = fang_mod_editor_active();
    BYTE* manager;
    BYTE* world;
    if (!is_scene_enabled || !fang_mod_editor_is_scene(editor) ||
        (!fang_mod_environment_active(editor) && !*(void**)(editor + 192))) {
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
        /* 720BE0 creates these optional models in the creature world (+136),
         * not GameModelWorld. Hiding that whole world would hide the creature
         * and its manipulators too. Omit only the stock pedestal and preview
         * terrain for this separately requested imported environment. */
        if (id == 0xb00db7bd || id == 0x026f3355) {
            *result = 0;
            trace_client_state("mod_editor_scene_native_model_skipped", id);
            return 0;
        }
        if (id == 0x021b35a0) {
            if (fang_mod_environment_active(scene_loading)) {
                *result = 0;
                return 0;
            }
            *result = FANG_EDITOR_SCENE_MODEL;
            trace_client_state("mod_editor_scene_model_selected", *result);
            return 1;
        }
        /* 720DEB applies editorLight to the scene world. editorSkyBoxEffect
         * is an effect ID, not a lighting resource, and must remain native. */
        if (id == 0xb00db8c8) {
            *result = FANG_EDITOR_SCENE_LIGHT;
            trace_client_state("mod_editor_scene_light_selected", *result);
            return 1;
        }
        if (id == 0x026f337b && fang_mod_environment_active(scene_loading)) {
            *result = FANG_EDITOR_ENV_ENTRY;
            return 1;
        }
    }
    return scene_property(property, id, result);
}

static unsigned char FANG_THISCALL scene_load_background(BYTE* editor) {
    unsigned char result;
    BYTE* previous = scene_loading;
    if (fang_mod_editor_is_scene(editor)) {
        scene_loading = editor;
        fang_mod_environment_prepare(scene_base, editor);
    }
    result = scene_background(editor);
    if (scene_loading == editor && fang_mod_environment_active(editor) &&
        !fang_mod_environment_started(editor)) {
        fang_mod_environment_leave(editor);
        trace_client_state("mod_editor_environment_backdrop_fallback", 1);
        result = scene_background(editor);
    }
    if (scene_loading == editor) {
        trace_client_state("mod_editor_scene_model_loaded", *(void**)(editor + 192) != NULL);
        trace_client_state("mod_editor_scene_skybox_effect", *(uint32_t*)(editor + 700));
        trace_client_state("mod_editor_scene_pedestal_present", *(void**)(editor + 184) != NULL);
        trace_client_state("mod_editor_scene_test_model_present", *(void**)(editor + 188) != NULL);
    }
    scene_loading = previous;
    return result;
}

void fang_mod_install_scene(BYTE* base) {
    static const unsigned int calls[] = {0x320c52, 0x320cd6, 0x320d67, 0x320deb, 0x320eb8, 0x320ee9};
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
    scene_native_render = (scene_render_fn)(scene_base + 0x91520);
    for (index = 0; index < sizeof(calls) / sizeof(calls[0]); ++index) {
        if (!patch_call(base + calls[index], scene_property, scene_select_property)) break;
    }
    if (index != sizeof(calls) / sizeof(calls[0])) goto rollback;
    if (!patch_call(base + 0x4a248, scene_request, scene_switch_request)) goto rollback;
    if (!patch_call(base + 0xee5ac, scene_native_render, scene_render_level)) goto rollback_switch;
    if (patch_pointer((void**)(base + 0xbd9994), scene_background, scene_load_background)) {
        is_scene_enabled = 1;
        trace_client_state("mod_editor_scene_enabled", 1);
        return;
    }
    if (!patch_call(base + 0xee5ac, scene_render_level, scene_native_render))
        trace_client_state("mod_editor_scene_render_rollback_failed", 1);
rollback_switch:
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

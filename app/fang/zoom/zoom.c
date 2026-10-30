#include "zoom/zoom.h"
#include "zoom/camera.h"

#include <math.h>

static const fang_client103_camera_bindings* native_camera;
/* The feature owns installation state; read-only camera bindings are shared. */
static fang_hook_set zoom_hooks;
static void* zoom_camera;
static void* zoom_level;

// Reads: checks that the camera is in the supported native follow and zoom modes.
static int is_follow_camera(BYTE* camera) {
    return fang_client103_camera_is_valid(native_camera, camera, camera_read_size) &&
        camera[camera_override] == 0 &&
        *(unsigned int*)(camera + camera_mode) == 5 &&
        *(unsigned int*)(camera + camera_zoom_mode) == 3 &&
        *(unsigned int*)(camera + camera_follow_mode) == 4;
}

// Reads: returns a distance constrained to the supplied interval.
static float camera_clamp(float distance, float minimum, float maximum) {
    if (distance < minimum) {
        return minimum;
    }
    if (distance > maximum) {
        return maximum;
    }
    return distance;
}

/* Only the local gameplay update's call to sub_44ABB0 is redirected. The
 * spectator/transition callers and scripted camera setters stay native.
 * Let the refresh update the hero pose, angles and authored mission distance,
 * then reopen its collapsed zoom interval and retain the native wheel target.
 * No input is synthesized and no camera position or orientation is replaced. */
// Modifies: runs native camera refresh, reopens mission zoom, preserves wheel distance, and updates tracking and traces.
static void* __cdecl refresh_mission_camera(unsigned int hero_id) {
    BYTE* camera = (BYTE*)native_camera->camera();
    void* level = native_camera->level(native_camera->root());
    float previous_distance = 0;
    float previous_target = 0;
    float previous_minimum = 0;
    float previous_maximum = 0;
    float minimum;
    float maximum;
    float native_maximum;
    int is_preserved = 0;
    void* result;

    if (hero_id != 0 && level != NULL && camera == zoom_camera &&
        level == zoom_level && is_follow_camera(camera)) {
        previous_distance = *(float*)(camera + camera_distance);
        previous_target = *(float*)(camera + camera_distance_target);
        previous_minimum = *(float*)(camera + camera_zoom_min);
        previous_maximum = *(float*)(camera + camera_zoom_max);
        is_preserved = isfinite(previous_distance) && isfinite(previous_target) &&
            isfinite(previous_minimum) && isfinite(previous_maximum) &&
            previous_minimum > 0 && previous_minimum < previous_maximum;
    }

    result = native_camera->refresh(hero_id);
    if (hero_id == 0 || level == NULL || native_camera->camera() != camera ||
        !is_follow_camera(camera) ||
        *(unsigned int*)(camera + camera_target) != hero_id) {
        zoom_camera = NULL;
        zoom_level = NULL;
        return result;
    }

    minimum = *(float*)(camera + camera_native_min);
    native_maximum = *(float*)(camera + camera_native_max);
    maximum = *(float*)(camera + camera_zoom_max);
    /* Touch only the equal-endpoint mission refresh. A different authored or
     * scripted interval, invalid properties, or fixed camera is left alone. */
    if (!isfinite(minimum) || !isfinite(native_maximum) || !isfinite(maximum) ||
        minimum <= 0 || minimum >= maximum || maximum > native_maximum ||
        *(float*)(camera + camera_zoom_min) != maximum) {
        zoom_camera = NULL;
        zoom_level = NULL;
        return result;
    }

    *(float*)(camera + camera_zoom_min) = minimum;
    if (is_preserved && previous_minimum == minimum && previous_maximum == maximum) {
        *(float*)(camera + camera_distance) =
            camera_clamp(previous_distance, minimum, maximum);
        *(float*)(camera + camera_distance_target) =
            camera_clamp(previous_target, minimum, maximum);
    } else {
        trace_client_state("camera_zoom_min_milli", (unsigned int)(minimum * 1000));
        trace_client_state("camera_zoom_max_milli", (unsigned int)(maximum * 1000));
    }
    zoom_camera = camera;
    zoom_level = level;
    return result;
}

// Modifies: injects the mission camera zoom hook and updates installation bookkeeping.
int fang_install_camera_zoom(HMODULE executable) {
    void* instruction;
    if (zoom_hooks.is_committed) return 1;
    if (!fang_client103_camera_zoom_bind(executable)) return 0;
    native_camera = fang_client103_camera_get_bindings();
    if (native_camera == NULL || native_camera->refresh == NULL) return 0;
    instruction = fang_client103_at(&native_camera->image, camera_refresh_call_rva, 5);
    if (instruction == NULL) return 0;
    fang_hook_begin(&zoom_hooks, "zoom");
    if (!fang_hook_call(&zoom_hooks, "missionRefresh", instruction,
        (void*)native_camera->refresh, (void*)refresh_mission_camera,
        "build103 local gameplay call to sub_44ABB0 at RVA51971")) {
        if (!fang_hook_rollback(&zoom_hooks)) {
            trace_client_state("camera_zoom_rollback_failed", 1);
        }
        return 0;
    }
    fang_hook_commit(&zoom_hooks);
    return 1;
}

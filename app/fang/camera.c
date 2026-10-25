#include "hook.h"
#include "control.h"

#include <math.h>
#include <stdint.h>
#include <string.h>

typedef void* (__cdecl* camera_getter_fn)(void);
typedef void* (__cdecl* camera_refresh_fn)(unsigned int hero_id);
typedef void* (__cdecl* camera_level_fn)(void* root);
typedef void* (__cdecl* camera_object_fn)(unsigned int handle);

static BYTE* camera_client_base;
static camera_getter_fn native_camera;
static camera_getter_fn native_root;
static camera_level_fn native_level;
static camera_refresh_fn native_refresh;
static camera_object_fn native_camera_object;
static void* zoom_camera;
static void* zoom_level;

enum {
    camera_target = 24,
    camera_override = 30,
    camera_zoom_mode = 32,
    camera_follow_mode = 40,
    camera_mode = 44,
    camera_distance = 176,
    camera_distance_target = 180,
    camera_zoom_max = 192,
    camera_zoom_min = 196,
    camera_native_min = 416,
    camera_native_max = 420,
    camera_read_size = 424,
    camera_snapshot_size = 464
};

static void trace_camera_vector(const char* x_kind, const char* y_kind,
    const char* z_kind, const BYTE* vector) {
    trace_client_state(x_kind, *(const unsigned int*)vector);
    trace_client_state(y_kind, *(const unsigned int*)(vector + 4));
    trace_client_state(z_kind, *(const unsigned int*)(vector + 8));
}

/* Mirror 103 sub_52D610's source selection without calling any camera getter
 * that can rebuild the cached transform or reset its smoothing state. */
static void trace_camera_follow_anchor(unsigned int handle) {
    const BYTE* object;
    const BYTE* control;
    const BYTE* render;
    const BYTE* anchor;
    if (handle == 0 || native_camera_object == NULL) {
        trace_client_state("camera_anchor_unavailable", 1);
        return;
    }
    object = (const BYTE*)native_camera_object(handle);
    if (!readable_range(object, 668)) {
        trace_client_state("camera_anchor_unavailable", 2);
        return;
    }
    trace_client_state("camera_anchor_resolved_handle", *(const unsigned int*)object);
    if (*(const unsigned int*)object != handle) {
        trace_client_state("camera_anchor_unavailable", 6);
        return;
    }
    trace_camera_vector("camera_object_x_bits", "camera_object_y_bits",
        "camera_object_z_bits", object + 24);
    control = *(const BYTE* const*)(object + 664);
    render = *(const BYTE* const*)(object + 652);
    if (control != NULL && !readable_range(control, 484)) {
        trace_client_state("camera_anchor_unavailable", 3);
        return;
    }
    trace_client_state("camera_control_mode", control == NULL ? 0 :
        *(const unsigned int*)(control + 480));
    if (control != NULL && *(const unsigned int*)(control + 480) != 0) {
        if (!readable_range(control, 540)) {
            trace_client_state("camera_anchor_unavailable", 4);
            return;
        }
        anchor = control + 528;
        trace_client_state("camera_anchor_source", 1);
    } else if (render != NULL) {
        if (!readable_range(render, 28)) {
            trace_client_state("camera_anchor_unavailable", 5);
            return;
        }
        anchor = render + 16;
        trace_client_state("camera_anchor_source", 2);
    } else {
        anchor = object + 24;
        trace_client_state("camera_anchor_source", 3);
    }
    trace_camera_vector("camera_source_x_bits", "camera_source_y_bits",
        "camera_source_z_bits", anchor);
}

static int is_follow_camera(BYTE* camera) {
    return readable_range(camera, camera_read_size) &&
        *(void**)camera == camera_client_base + 0xBE1968 &&
        camera[camera_override] == 0 &&
        *(unsigned int*)(camera + camera_mode) == 5 &&
        *(unsigned int*)(camera + camera_zoom_mode) == 3 &&
        *(unsigned int*)(camera + camera_follow_mode) == 4;
}

#if FANG_SCENARIO
static int copy_camera_vector(float* destination, const BYTE* source) {
    unsigned int index;
    for (index = 0; index < 3; index++) {
        uint32_t bits;
        memcpy(&bits, source + index * sizeof(float), sizeof(bits));
        if ((bits & 0x7F800000u) == 0x7F800000u) return 0;
    }
    memcpy(destination, source, 3 * sizeof(float));
    return 1;
}

/* Read only the already-validated native follow-camera cache. Do not invoke
 * sub_52FCA0: its lazy transform refresh would change the evidence. */
void fang_control_camera_read(fang_control_camera_snapshot* snapshot) {
    const BYTE* camera;
    const BYTE* object;
    const BYTE* control;
    const BYTE* render;
    const BYTE* source;
    memset(snapshot, 0, sizeof(*snapshot));
    if (native_camera == NULL || camera_client_base == NULL) return;
    camera = (const BYTE*)native_camera();
    if (!readable_range(camera, camera_snapshot_size) ||
        *(void* const*)camera != camera_client_base + 0xBE1968) return;
    snapshot->is_available = 1;
    snapshot->target_handle = *(const uint32_t*)(camera + camera_target);
    snapshot->is_override_active = camera[camera_override] != 0;
    snapshot->mode = *(const uint32_t*)(camera + camera_mode);
    snapshot->follow_mode = *(const uint32_t*)(camera + camera_follow_mode);
    snapshot->zoom_mode = *(const uint32_t*)(camera + camera_zoom_mode);
    if (copy_camera_vector(snapshot->center, camera + 48)) snapshot->fields |= 1;
    if (copy_camera_vector(snapshot->desired, camera + 60)) snapshot->fields |= 2;
    if (copy_camera_vector(snapshot->anchor, camera + 80)) snapshot->fields |= 4;
    if (copy_camera_vector(snapshot->offset_anchor, camera + 92)) snapshot->fields |= 8;
    memcpy(&snapshot->hold_timer, camera + 460, sizeof(float));
    memcpy(&snapshot->distance, camera + camera_distance, sizeof(float));
    memcpy(&snapshot->distance_target, camera + camera_distance_target, sizeof(float));
    if (snapshot->target_handle == 0 || native_camera_object == NULL) return;
    object = (const BYTE*)native_camera_object(snapshot->target_handle);
    if (!readable_range(object, 668) || *(const uint32_t*)object != snapshot->target_handle) return;
    control = *(const BYTE* const*)(object + 664);
    render = *(const BYTE* const*)(object + 652);
    if (control != NULL && !readable_range(control, 484)) return;
    snapshot->control_mode = control == NULL ? 0 : *(const uint32_t*)(control + 480);
    if (snapshot->control_mode != 0) {
        if (!readable_range(control, 540)) return;
        source = control + 528;
        snapshot->anchor_source = 1;
    } else if (render != NULL) {
        if (!readable_range(render, 28)) return;
        source = render + 16;
        snapshot->anchor_source = 2;
    } else {
        source = object + 24;
        snapshot->anchor_source = 3;
    }
    snapshot->is_anchor_available = copy_camera_vector(snapshot->source, source);
}
#endif

/* Snapshot-only observation. Never refresh, retarget or override a scripted
 * camera while collecting evidence of a lost follow target. */
void fang_trace_camera_state(void) {
    BYTE* camera;
    unsigned int distance_bits;
    unsigned int target_bits;
    if (native_camera == NULL || camera_client_base == NULL) {
        trace_client_state("camera_snapshot_unavailable", 1);
        return;
    }
    camera = (BYTE*)native_camera();
    if (!readable_range(camera, camera_snapshot_size) ||
        *(void**)camera != camera_client_base + 0xBE1968) {
        trace_client_state("camera_snapshot_unsupported", 1);
        return;
    }
    trace_client_state("camera_follow_target", *(unsigned int*)(camera + camera_target));
    trace_client_state("camera_override", camera[camera_override]);
    trace_client_state("camera_mode", *(unsigned int*)(camera + camera_mode));
    trace_client_state("camera_follow_mode", *(unsigned int*)(camera + camera_follow_mode));
    trace_client_state("camera_zoom_mode", *(unsigned int*)(camera + camera_zoom_mode));
    memcpy(&distance_bits, camera + camera_distance, sizeof(distance_bits));
    memcpy(&target_bits, camera + camera_distance_target, sizeof(target_bits));
    trace_client_state("camera_distance_bits", distance_bits);
    trace_client_state("camera_distance_target_bits", target_bits);
    /* sub_52D750 writes the desired center and both anchor endpoints;
     * sub_531860 smooths current center toward desired center. */
    trace_camera_vector("camera_center_x_bits", "camera_center_y_bits",
        "camera_center_z_bits", camera + 48);
    trace_camera_vector("camera_desired_x_bits", "camera_desired_y_bits",
        "camera_desired_z_bits", camera + 60);
    trace_camera_vector("camera_anchor_x_bits", "camera_anchor_y_bits",
        "camera_anchor_z_bits", camera + 80);
    trace_camera_vector("camera_offset_anchor_x_bits", "camera_offset_anchor_y_bits",
        "camera_offset_anchor_z_bits", camera + 92);
    trace_client_state("camera_hold_timer_bits", *(const unsigned int*)(camera + 460));
    /* sub_52F7A0 caches a native transform at +324. Capture its translation,
     * scale and basis without the lazy rebuild performed by sub_52FCA0. */
    trace_client_state("camera_transform_dirty", camera[380]);
    trace_client_state("camera_transform_flags", *(const unsigned short*)(camera + 324));
    trace_client_state("camera_transform_revision", *(const unsigned short*)(camera + 326));
    trace_camera_vector("camera_transform_x_bits", "camera_transform_y_bits",
        "camera_transform_z_bits", camera + 328);
    trace_client_state("camera_transform_scale_bits", *(const unsigned int*)(camera + 340));
    trace_camera_vector("camera_basis0_x_bits", "camera_basis0_y_bits",
        "camera_basis0_z_bits", camera + 344);
    trace_camera_vector("camera_basis1_x_bits", "camera_basis1_y_bits",
        "camera_basis1_z_bits", camera + 356);
    trace_camera_vector("camera_basis2_x_bits", "camera_basis2_y_bits",
        "camera_basis2_z_bits", camera + 368);
    trace_camera_follow_anchor(*(const unsigned int*)(camera + camera_target));
}

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
static void* __cdecl refresh_mission_camera(unsigned int hero_id) {
    BYTE* camera = (BYTE*)native_camera();
    void* level = native_level(native_root());
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

    result = native_refresh(hero_id);
    if (hero_id == 0 || level == NULL || native_camera() != camera ||
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

int fang_install_camera_zoom(HMODULE executable) {
    /* Build-103 call site and field-access signatures. Reject other layouts
     * before changing the sole call instruction in process memory. */
    static const BYTE refresh_call[] = {
        0xE8, 0xF0, 0x3E, 0x09, 0x00, 0x50,
        0xE8, 0x3A, 0x92, 0xFF, 0xFF,
        0x6A, 0x00, 0xE8, 0x53, 0x4B, 0x09, 0x00,
    };
    static const BYTE getter_signature[] = {
        0xE8, 0x2B, 0x11, 0x28, 0x00, 0x8B, 0x10, 0x8B, 0xC8,
        0x8B, 0x42, 0x6C, 0xFF, 0xD0, 0x8B, 0x10, 0x8B, 0xC8,
        0x8B, 0x42, 0x38, 0xFF, 0xD0,
    };
    static const BYTE distance_signature[] = {
        0xF3, 0x0F, 0x10, 0x54, 0x24, 0x04,
        0xF3, 0x0F, 0x10, 0x44, 0x24, 0x08,
        0x0F, 0x2F, 0xC2, 0xF3, 0x0F, 0x11, 0x91, 0xC0, 0x00, 0x00, 0x00,
    };
    static const BYTE level_signature[] = {
        0x8B, 0x44, 0x24, 0x04, 0x85, 0xC0, 0x74, 0x07,
        0x8B, 0x80, 0x8C, 0x74, 0x00, 0x00, 0xC3,
    };
    static const BYTE object_signature[] = {
        0x8B, 0x44, 0x24, 0x04, 0x50,
        0xE8, 0x46, 0x32, 0xFE, 0xFF, 0x8B, 0xC8,
        0xE8, 0x2F, 0x82, 0xFF, 0xFF, 0xC3,
    };
    BYTE* base = (BYTE*)executable;
    IMAGE_DOS_HEADER* dos = (IMAGE_DOS_HEADER*)base;
    IMAGE_NT_HEADERS* nt;
    if (sizeof(void*) != 4 || !readable_range(dos, sizeof(*dos)) ||
        dos->e_magic != IMAGE_DOS_SIGNATURE || dos->e_lfanew <= 0) {
        return 0;
    }
    nt = (IMAGE_NT_HEADERS*)(base + dos->e_lfanew);
    if (!readable_range(nt, sizeof(*nt)) || nt->Signature != IMAGE_NT_SIGNATURE ||
        nt->FileHeader.Machine != IMAGE_FILE_MACHINE_I386 ||
        nt->OptionalHeader.SizeOfImage < 0xBE1970 ||
        !readable_range(base + 0x5196B, sizeof(refresh_call)) ||
        !readable_range(base + 0x12DA80, sizeof(getter_signature)) ||
        !readable_range(base + 0x12E1B0, sizeof(distance_signature)) ||
        !readable_range(base + 0x5BCC30, sizeof(level_signature))) {
        return 0;
    }
    if (memcmp(base + 0x5196B, refresh_call, sizeof(refresh_call)) != 0 ||
        memcmp(base + 0x12DA80, getter_signature, sizeof(getter_signature)) != 0 ||
        memcmp(base + 0x12E1B0, distance_signature, sizeof(distance_signature)) != 0 ||
        memcmp(base + 0x5BCC30, level_signature, sizeof(level_signature)) != 0) {
        return 0;
    }
    camera_client_base = base;
    native_camera = (camera_getter_fn)(base + 0x12DA80);
    native_root = (camera_getter_fn)(base + 0x5BCBE0);
    native_level = (camera_level_fn)(base + 0x5BCC30);
    native_refresh = (camera_refresh_fn)(base + 0x4ABB0);
    if (readable_range(base + 0x5D94F0, sizeof(object_signature)) &&
        memcmp(base + 0x5D94F0, object_signature, sizeof(object_signature)) == 0) {
        native_camera_object = (camera_object_fn)(base + 0x5D94F0);
    }
    return patch_call(base + 0x51971, (void*)native_refresh, (void*)refresh_mission_camera);
}

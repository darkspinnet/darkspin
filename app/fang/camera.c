#include "hook.h"

#include <math.h>
#include <stdint.h>
#include <string.h>

typedef void* (__cdecl* camera_getter_fn)(void);
typedef void* (__cdecl* camera_refresh_fn)(unsigned int hero_id);
typedef void* (__cdecl* camera_level_fn)(void* root);

static BYTE* camera_client_base;
static camera_getter_fn native_camera;
static camera_getter_fn native_root;
static camera_level_fn native_level;
static camera_refresh_fn native_refresh;
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
    camera_read_size = 424
};

static int is_follow_camera(BYTE* camera) {
    return readable_range(camera, camera_read_size) &&
        *(void**)camera == camera_client_base + 0xBE1968 &&
        camera[camera_override] == 0 &&
        *(unsigned int*)(camera + camera_mode) == 5 &&
        *(unsigned int*)(camera + camera_zoom_mode) == 3 &&
        *(unsigned int*)(camera + camera_follow_mode) == 4;
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
    return patch_call(base + 0x51971, (void*)native_refresh, (void*)refresh_mission_camera);
}

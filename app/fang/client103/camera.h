#ifndef DARKSPIN_FANG_CLIENT103_CAMERA_H
#define DARKSPIN_FANG_CLIENT103_CAMERA_H

#include "client103/image.h"
#include <stddef.h>

typedef void* (__cdecl* camera_getter_fn)(void);
typedef void* (__cdecl* camera_refresh_fn)(unsigned int hero_id);
typedef void* (__cdecl* camera_level_fn)(void* root);
typedef void* (__cdecl* camera_object_fn)(unsigned int handle);

/* Verified accessed spans, not the complete native object size. Build103
 * sub_52D610/52D750/52F7A0/531860 and sub_44ABB0 own these fields. */
enum {
    camera_target = 24, camera_override = 30, camera_zoom_mode = 32,
    camera_follow_mode = 40, camera_mode = 44,
    camera_center = 48, camera_desired = 60, camera_anchor = 80,
    camera_offset_anchor = 92,
    camera_distance = 176, camera_distance_target = 180,
    camera_zoom_max = 192, camera_zoom_min = 196,
    camera_transform_flags = 324, camera_transform_revision = 326,
    camera_transform_position = 328, camera_transform_scale = 340,
    camera_transform_basis0 = 344, camera_transform_basis1 = 356,
    camera_transform_basis2 = 368, camera_transform_dirty = 380,
    camera_native_min = 416, camera_native_max = 420,
    camera_read_size = 424, camera_hold_timer = 460, camera_snapshot_size = 464,
    camera_object_position = 24, camera_object_render = 652,
    camera_object_control = 664, camera_object_read_size = 668,
    camera_control_mode = 480, camera_control_mode_size = 484,
    camera_control_anchor = 528, camera_control_read_size = 540,
    camera_render_anchor = 16, camera_render_read_size = 28,
    camera_refresh_call_rva = 0x51971
};

/* A verified field view ending at zoom_min, not a complete camera class. */
typedef struct fang_client103_camera_zoom_view {
    BYTE before_distance[176];
    float distance;
    float distance_target;
    BYTE before_zoom_limit[8];
    float zoom_max;
    float zoom_min;
} fang_client103_camera_zoom_view;

_Static_assert(offsetof(fang_client103_camera_zoom_view, distance) == camera_distance, "camera distance offset");
_Static_assert(offsetof(fang_client103_camera_zoom_view, distance_target) == camera_distance_target, "camera target offset");
_Static_assert(offsetof(fang_client103_camera_zoom_view, zoom_max) == camera_zoom_max, "camera maximum offset");
_Static_assert(offsetof(fang_client103_camera_zoom_view, zoom_min) == camera_zoom_min, "camera minimum offset");

typedef struct fang_client103_camera_bindings {
    fang_client103_image image;
    camera_getter_fn camera;
    camera_object_fn object;
    camera_getter_fn root;
    camera_level_fn level;
    camera_refresh_fn refresh;
    const void* vtable;
} fang_client103_camera_bindings;

int fang_client103_camera_bind(HMODULE executable);
int fang_client103_camera_zoom_bind(HMODULE executable);
const fang_client103_camera_bindings* fang_client103_camera_get_bindings(void);
int fang_client103_camera_is_valid(const fang_client103_camera_bindings* bindings,
    const BYTE* camera, size_t accessed_size);

#endif

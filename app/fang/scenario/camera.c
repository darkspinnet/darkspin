#include "scenario/camera.h"
#include "client103/camera.h"
#include "hook.h"
#include "scenario/control.h"

#include <string.h>

static void trace_camera_vector(const char* x_kind, const char* y_kind,
    const char* z_kind, const BYTE* vector) {
    trace_client_state(x_kind, *(const unsigned int*)vector);
    trace_client_state(y_kind, *(const unsigned int*)(vector + 4));
    trace_client_state(z_kind, *(const unsigned int*)(vector + 8));
}

/* Mirror 103 sub_52D610's source selection without calling any camera getter
 * that can rebuild the cached transform or reset its smoothing state. */
static void trace_camera_follow_anchor(unsigned int handle) {
    const fang_client103_camera_bindings* bindings = fang_client103_camera_get_bindings();
    const BYTE* object;
    const BYTE* control;
    const BYTE* render;
    const BYTE* anchor;
    if (handle == 0 || bindings == NULL || bindings->object == NULL) {
        trace_client_state("camera_anchor_unavailable", 1);
        return;
    }
    object = (const BYTE*)bindings->object(handle);
    if (!readable_range(object, camera_object_read_size)) {
        trace_client_state("camera_anchor_unavailable", 2);
        return;
    }
    trace_client_state("camera_anchor_resolved_handle", *(const unsigned int*)object);
    if (*(const unsigned int*)object != handle) {
        trace_client_state("camera_anchor_unavailable", 6);
        return;
    }
    trace_camera_vector("camera_object_x_bits", "camera_object_y_bits",
        "camera_object_z_bits", object + camera_object_position);
    control = *(const BYTE* const*)(object + camera_object_control);
    render = *(const BYTE* const*)(object + camera_object_render);
    if (control != NULL && !readable_range(control, camera_control_mode_size)) {
        trace_client_state("camera_anchor_unavailable", 3);
        return;
    }
    trace_client_state("camera_control_mode", control == NULL ? 0 :
        *(const unsigned int*)(control + camera_control_mode));
    if (control != NULL && *(const unsigned int*)(control + camera_control_mode) != 0) {
        if (!readable_range(control, camera_control_read_size)) {
            trace_client_state("camera_anchor_unavailable", 4);
            return;
        }
        anchor = control + camera_control_anchor;
        trace_client_state("camera_anchor_source", 1);
    } else if (render != NULL) {
        if (!readable_range(render, camera_render_read_size)) {
            trace_client_state("camera_anchor_unavailable", 5);
            return;
        }
        anchor = render + camera_render_anchor;
        trace_client_state("camera_anchor_source", 2);
    } else {
        anchor = object + camera_object_position;
        trace_client_state("camera_anchor_source", 3);
    }
    trace_camera_vector("camera_source_x_bits", "camera_source_y_bits",
        "camera_source_z_bits", anchor);
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
    const fang_client103_camera_bindings* bindings = fang_client103_camera_get_bindings();
    const BYTE* camera;
    const BYTE* object;
    const BYTE* control;
    const BYTE* render;
    const BYTE* source;
    memset(snapshot, 0, sizeof(*snapshot));
    if (bindings == NULL) return;
    camera = (const BYTE*)bindings->camera();
    if (!fang_client103_camera_is_valid(bindings, camera, camera_snapshot_size)) return;
    snapshot->is_available = 1;
    snapshot->target_handle = *(const uint32_t*)(camera + camera_target);
    snapshot->is_override_active = camera[camera_override] != 0;
    snapshot->mode = *(const uint32_t*)(camera + camera_mode);
    snapshot->follow_mode = *(const uint32_t*)(camera + camera_follow_mode);
    snapshot->zoom_mode = *(const uint32_t*)(camera + camera_zoom_mode);
    if (copy_camera_vector(snapshot->center, camera + camera_center)) snapshot->fields |= 1;
    if (copy_camera_vector(snapshot->desired, camera + camera_desired)) snapshot->fields |= 2;
    if (copy_camera_vector(snapshot->anchor, camera + camera_anchor)) snapshot->fields |= 4;
    if (copy_camera_vector(snapshot->offset_anchor, camera + camera_offset_anchor)) snapshot->fields |= 8;
    memcpy(&snapshot->hold_timer, camera + camera_hold_timer, sizeof(float));
    memcpy(&snapshot->distance, camera + camera_distance, sizeof(float));
    memcpy(&snapshot->distance_target, camera + camera_distance_target, sizeof(float));
    if (snapshot->target_handle == 0 || bindings->object == NULL) return;
    object = (const BYTE*)bindings->object(snapshot->target_handle);
    if (!readable_range(object, camera_object_read_size) || *(const uint32_t*)object != snapshot->target_handle) return;
    control = *(const BYTE* const*)(object + camera_object_control);
    render = *(const BYTE* const*)(object + camera_object_render);
    if (control != NULL && !readable_range(control, camera_control_mode_size)) return;
    snapshot->control_mode = control == NULL ? 0 : *(const uint32_t*)(control + camera_control_mode);
    if (snapshot->control_mode != 0) {
        if (!readable_range(control, camera_control_read_size)) return;
        source = control + camera_control_anchor;
        snapshot->anchor_source = 1;
    } else if (render != NULL) {
        if (!readable_range(render, camera_render_read_size)) return;
        source = render + camera_render_anchor;
        snapshot->anchor_source = 2;
    } else {
        source = object + camera_object_position;
        snapshot->anchor_source = 3;
    }
    snapshot->is_anchor_available = copy_camera_vector(snapshot->source, source);
}
#endif

/* Snapshot/history observation. Never refresh, retarget or override a scripted
 * camera while collecting evidence of a lost follow target. */
void fang_trace_camera_state(void) {
    const fang_client103_camera_bindings* bindings = fang_client103_camera_get_bindings();
    BYTE* camera;
    unsigned int distance_bits;
    unsigned int target_bits;
    if (bindings == NULL) {
        trace_client_state("camera_snapshot_unavailable", 1);
        return;
    }
    camera = (BYTE*)bindings->camera();
    if (!fang_client103_camera_is_valid(bindings, camera, camera_snapshot_size)) {
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
        "camera_center_z_bits", camera + camera_center);
    trace_camera_vector("camera_desired_x_bits", "camera_desired_y_bits",
        "camera_desired_z_bits", camera + camera_desired);
    trace_camera_vector("camera_anchor_x_bits", "camera_anchor_y_bits",
        "camera_anchor_z_bits", camera + camera_anchor);
    trace_camera_vector("camera_offset_anchor_x_bits", "camera_offset_anchor_y_bits",
        "camera_offset_anchor_z_bits", camera + camera_offset_anchor);
    trace_client_state("camera_hold_timer_bits", *(const unsigned int*)(camera + camera_hold_timer));
    /* sub_52F7A0 caches a native transform at +324. Capture its translation,
     * scale and basis without the lazy rebuild performed by sub_52FCA0. */
    trace_client_state("camera_transform_dirty", camera[camera_transform_dirty]);
    trace_client_state("camera_transform_flags", *(const unsigned short*)(camera + camera_transform_flags));
    trace_client_state("camera_transform_revision", *(const unsigned short*)(camera + camera_transform_revision));
    trace_camera_vector("camera_transform_x_bits", "camera_transform_y_bits",
        "camera_transform_z_bits", camera + camera_transform_position);
    trace_client_state("camera_transform_scale_bits", *(const unsigned int*)(camera + camera_transform_scale));
    trace_camera_vector("camera_basis0_x_bits", "camera_basis0_y_bits",
        "camera_basis0_z_bits", camera + camera_transform_basis0);
    trace_camera_vector("camera_basis1_x_bits", "camera_basis1_y_bits",
        "camera_basis1_z_bits", camera + camera_transform_basis1);
    trace_camera_vector("camera_basis2_x_bits", "camera_basis2_y_bits",
        "camera_basis2_z_bits", camera + camera_transform_basis2);
    trace_camera_follow_anchor(*(const unsigned int*)(camera + camera_target));
}


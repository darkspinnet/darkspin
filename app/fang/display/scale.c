#include "display/display.h"

#include <math.h>
#include <stdio.h>
#include <string.h>

#if defined(__GNUC__)
#define SCALE_CALLER() __builtin_return_address(0)
#else
#include <intrin.h>
#define SCALE_CALLER() _ReturnAddress()
#endif

/* Build 103 only. BE3890 supplies SPUI's logical root and inverse hit geometry.
 * Native frame/image/glyph presentation is scaled at the 912EF0 GPU submission
 * interface below; changing window transforms alone did not enlarge it.
 *
 * The logical root still shrinks for docking. 9193A0's shared mouse ratios and
 * 908D20's world-picking bounds need the physical presentation rectangle.
 * Only their byte-confirmed getter calls receive that physical rectangle.
 * All other callers see the native logical area. No shared ratio is patched,
 * no Flash viewport changes, and no installed layout resource is modified. */
typedef BYTE* (__cdecl* scale_manager_fn)(void);
typedef LONG (FANG_THISCALL* scale_resize_fn)(BYTE*, float, float);
typedef const float* (FANG_THISCALL* scale_area_fn)(BYTE*);
typedef unsigned char (FANG_THISCALL* scale_event_fn)(BYTE*, BYTE*, BYTE*,
    const BYTE*, unsigned char, unsigned char);
typedef void (FANG_THISCALL* scale_dirty_fn)(BYTE*, BYTE*);
typedef void (FANG_THISCALL* scale_redraw_fn)(BYTE*);
typedef unsigned char (FANG_THISCALL* scale_mouse_fn)(BYTE*, float, float, unsigned int);
typedef unsigned char (FANG_THISCALL* scale_button_fn)(BYTE*, float, float,
    unsigned int, unsigned int);
typedef void (__stdcall* scale_render_fn)(const BYTE*, void*);

extern int fang_ui_scale_load(void);
extern int fang_ui_scale_save(int percent);

static BYTE* scale_image;
static scale_manager_fn native_manager;
static scale_resize_fn native_resize;
static scale_area_fn native_area;
static scale_event_fn native_event;
static scale_dirty_fn native_dirty;
static scale_redraw_fn native_redraw;
static scale_mouse_fn native_mouse;
static scale_button_fn native_button_down;
static scale_button_fn native_button_up;
static scale_button_fn native_wheel;
static scale_render_fn native_render;
static int reported_render_percent;
static int reported_pick_percent;
static int requested_percent = 100;
static struct {
    BYTE* owner;
    float physical_areas[4];
    int percent;
    int applied_percent;
    int reported_percent;
    int reported_input_percent;
    int is_installed;
    DWORD next_snapshot;
    unsigned int mouse_count;
    float mouse_x;
    float mouse_y;
} scale = {NULL, {0}, 100, 100, 0, 0, 0, 0, 0, 0, 0};

static void trace_scale_number(const char* name, float coordinate);

/* Build 103: 912EF0 -> 912AB0 submits SPUI geometry. The draw state contains
 * a clip-space matrix pointer, packed tint, and a pixel-space clipping rect.
 * 912AB0 copies the matrix into its GPU batch before returning. GFx has its
 * own renderer and never consumes this interface. Keep both temporaries local
 * rather than modifying a shared cached matrix or the installed UI resource. */
// Modifies: scales native SPUI frame, image and glyph batches at GPU submission.
static void __stdcall scaled_render(const BYTE* state, void* primitives) {
    BYTE scaled_state[24];
    float matrix[16];
    const float* source;
    float factor;
    unsigned int row;
    int percent = scale.applied_percent;
    if (percent == 100 || !readable_range(state, sizeof(scaled_state))) {
        native_render(state, primitives);
        return;
    }
    source = *(const float* const*)state;
    if (!readable_range(source, sizeof(matrix))) {
        native_render(state, primitives);
        return;
    }
    factor = percent / 100.0f;
    memcpy(scaled_state, state, sizeof(scaled_state));
    memcpy(matrix, source, sizeof(matrix));
    /* Screen origin is (-1,+1) in this renderer's clip space. Compose about
     * that corner, including the homogeneous column for non-affine batches. */
    for (row = 0; row < 4; ++row) {
        matrix[row * 4] = matrix[row * 4] * factor + matrix[row * 4 + 3] * (factor - 1.0f);
        matrix[row * 4 + 1] = matrix[row * 4 + 1] * factor - matrix[row * 4 + 3] * (factor - 1.0f);
    }
    *(const float**)scaled_state = matrix;
    for (row = 2; row < 6; ++row) ((float*)scaled_state)[row] *= factor;
    if (reported_render_percent != percent) {
        reported_render_percent = percent;
        trace_client_state("display_ui_scale_gpu_submit", percent);
        trace_scale_number("display_ui_scale_gpu_source_x_bits", source[0]);
        trace_scale_number("display_ui_scale_gpu_source_y_bits", source[5]);
    }
    native_render(scaled_state, primitives);
}

// Reads: reverses SPUI's root-local conversion only at the Flash forwarding boundary.
static float flash_pointer_factor(BYTE* root) {
    BYTE* manager = native_manager();
    if (manager != scale.owner || !readable_range(manager, 8) ||
        *(BYTE**)(manager + 4) != root) return 1.0f;
    return scale.applied_percent / 100.0f;
}

// Modifies: delivers physical presentation coordinates to Flash mouse movement.
static unsigned char FANG_THISCALL scaled_root_mouse(BYTE* root, float x,
    float y, unsigned int modifier) {
    float factor = flash_pointer_factor(root);
    ++scale.mouse_count;
    scale.mouse_x = x * factor;
    scale.mouse_y = y * factor;
    return native_mouse(root, scale.mouse_x, scale.mouse_y, modifier);
}

// Modifies: keeps Flash button presses in the same coordinate space as movement.
static unsigned char FANG_THISCALL scaled_root_button_down(BYTE* root, float x,
    float y, unsigned int button, unsigned int modifier) {
    float factor = flash_pointer_factor(root);
    return native_button_down(root, x * factor, y * factor, button, modifier);
}

// Modifies: preserves the press/release coordinate pair for Flash dragging.
static unsigned char FANG_THISCALL scaled_root_button_up(BYTE* root, float x,
    float y, unsigned int button, unsigned int modifier) {
    float factor = flash_pointer_factor(root);
    return native_button_up(root, x * factor, y * factor, button, modifier);
}

// Modifies: sends wheel positions through the same Flash-only conversion.
static unsigned char FANG_THISCALL scaled_root_wheel(BYTE* root, float x,
    float y, unsigned int modifier, unsigned int delta) {
    float factor = flash_pointer_factor(root);
    return native_wheel(root, x * factor, y * factor, modifier, delta);
}

// Reads: records bounded build-103 geometry without changing native UI state.
static void trace_scale_number(const char* name, float coordinate) {
    unsigned int bits;
    memcpy(&bits, &coordinate, sizeof(bits));
    trace_client_state(name, bits);
}

// Reads: compares window geometry with the cached packet actually sent to Draw.
static void trace_scale_window(BYTE* window, unsigned int index) {
    char name[96];
    BYTE* packet;
    if (!readable_range(window, 500)) return;
    _snprintf(name, sizeof(name), "display_ui_scale_window_%u_address", index);
    trace_client_state(name, (unsigned int)(ULONG_PTR)window);
    _snprintf(name, sizeof(name), "display_ui_scale_window_%u_viewer", index);
    trace_client_state(name, *(unsigned int*)(window + 452));
    _snprintf(name, sizeof(name), "display_ui_scale_window_%u_flags", index);
    trace_client_state(name, *(unsigned int*)(window + 44));
    _snprintf(name, sizeof(name), "display_ui_scale_window_%u_draw_x_bits", index);
    trace_scale_number(name, *(float*)(window + 296));
    _snprintf(name, sizeof(name), "display_ui_scale_window_%u_draw_y_bits", index);
    trace_scale_number(name, *(float*)(window + 316));
    _snprintf(name, sizeof(name), "display_ui_scale_window_%u_input_x_bits", index);
    trace_scale_number(name, *(float*)(window + 364));
    _snprintf(name, sizeof(name), "display_ui_scale_window_%u_input_y_bits", index);
    trace_scale_number(name, *(float*)(window + 384));
    packet = *(BYTE**)(window + 84);
    if (!readable_range(packet, 164)) return;
    _snprintf(name, sizeof(name), "display_ui_scale_window_%u_packet_x_bits", index);
    trace_scale_number(name, *(float*)(packet + 72));
    _snprintf(name, sizeof(name), "display_ui_scale_window_%u_packet_y_bits", index);
    trace_scale_number(name, *(float*)(packet + 92));
}

// Reads: samples the shared input ratios and SPUI render boundary every 5 seconds.
static void trace_scale_geometry(BYTE* manager, BYTE* root) {
    DWORD now = GetTickCount();
    BYTE* windows[1024];
    BYTE* window;
    BYTE* sentinel;
    BYTE* link;
    unsigned int index, count = 1, popup_count = 0;
    if (scale.next_snapshot && (LONG)(now - scale.next_snapshot) < 0) return;
    scale.next_snapshot = now + 5000;
    trace_client_state("display_ui_scale_snapshot", scale.applied_percent);
    trace_scale_number("display_ui_scale_physical_width_bits", scale.physical_areas[2]);
    trace_scale_number("display_ui_scale_physical_height_bits", scale.physical_areas[3]);
    trace_scale_number("display_ui_scale_root_width_bits", *(float*)(root + 108) - *(float*)(root + 100));
    trace_scale_number("display_ui_scale_root_height_bits", *(float*)(root + 112) - *(float*)(root + 104));
    trace_scale_number("display_ui_scale_mouse_ratio_x_bits", *(float*)(scale_image + 0xD5462C));
    trace_scale_number("display_ui_scale_mouse_ratio_y_bits", *(float*)(scale_image + 0xD54630));
    trace_scale_number("display_ui_scale_mouse_x_bits", *(float*)(manager + 188));
    trace_scale_number("display_ui_scale_mouse_y_bits", *(float*)(manager + 192));
    trace_client_state("display_ui_scale_flash_mouse_count", scale.mouse_count);
    trace_scale_number("display_ui_scale_flash_mouse_x_bits", scale.mouse_x);
    trace_scale_number("display_ui_scale_flash_mouse_y_bits", scale.mouse_y);
    trace_scale_window(root, 0);
    /* Popup items are descendants of the layer windows. The previous direct-
     * child snapshot stopped at those containers and never sampled the menu. */
    windows[0] = root;
    for (index = 0; index < count; ++index) {
        window = windows[index];
        if (!readable_range(window, 500)) continue;
        if (*(BYTE**)window == scale_image + 0xC15990 ||
            *(BYTE**)window == scale_image + 0xC154B0) {
            trace_scale_window(window, ++popup_count);
        }
        sentinel = window + 60;
        link = *(BYTE**)sentinel;
        while (link != sentinel && count < sizeof(windows) / sizeof(windows[0])) {
            if (!readable_range(link, 8)) break;
            windows[count++] = link - 8;
            link = *(BYTE**)link;
        }
    }
    trace_client_state("display_ui_scale_popup_count", popup_count);
}

// Reads: returns this launch's native-interface scale as a percentage.
int fang_native_ui_scale(void) {
    return scale.percent;
}

// Reads: returns the saved choice for the next launch's options UI.
int fang_native_ui_scale_requested(void) {
    return requested_percent;
}

// Modifies: persists the next launch's scale without changing this session.
int fang_request_native_ui_scale(int percent) {
    if (percent < 100 || percent > 300 || percent % 5) return 0;
    if (percent == requested_percent) return 1;
    if (!scale.is_installed) return 0;
    if (!fang_ui_scale_save(percent)) {
        trace_client_state("display_ui_scale_save_failed", percent);
        return 0;
    }
    requested_percent = percent;
    trace_client_state("display_ui_scale_requested", percent);
    return 1;
}

// Reads: resolves the current SPUI root without retaining its window pointer.
static BYTE* scale_root(BYTE* manager) {
    BYTE* root;
    if (!readable_range(manager, 8)) return NULL;
    root = *(BYTE**)(manager + 4);
    if (!readable_range(root, 500)) return NULL;
    return root;
}

// Reads: keeps shared mouse ratios and world picking bounds in physical pixels.
static const float* FANG_THISCALL physical_input_area(BYTE* window) {
    BYTE* caller = (BYTE*)SCALE_CALLER();
    BYTE* manager = native_manager();
    BYTE* root = scale_root(manager);
    /* 9193DC and 91942E are the two indirect GetArea calls in 9193A0.
     * 908D42 is the screen-bounds getter used by Arsenal picking (4132F0).
     * It compares 919130's physical mouse position before casting the ray.
     * Match exact return addresses while leaving SPUI layout getters logical. */
    if (root && window == root + 4 && scale.owner == manager &&
        (caller == scale_image + 0x5193DE || caller == scale_image + 0x519430 ||
         caller == scale_image + 0x508D44)) {
        if (caller == scale_image + 0x508D44 &&
            reported_pick_percent != scale.applied_percent) {
            reported_pick_percent = scale.applied_percent;
            trace_client_state("display_ui_scale_pick_bounds_preserved", scale.applied_percent);
        }
        if (scale.reported_input_percent != scale.applied_percent) {
            scale.reported_input_percent = scale.applied_percent;
            trace_client_state("display_ui_scale_input_preserved", scale.applied_percent);
        }
        return scale.physical_areas;
    }
    return native_area(window);
}

// Modifies: composes native SPUI rendering and inverse hit geometry together.
static unsigned char FANG_THISCALL scaled_transform(BYTE* manager, BYTE* target,
    BYTE* window, const BYTE* event, unsigned char is_bubbling, unsigned char is_recursive) {
    unsigned char result = native_event(manager, target, window, event,
        is_bubbling, is_recursive);
    BYTE* root = scale_root(manager);
    float* transform;
    float factor, center_x, center_y;
    unsigned int row;
    if (!root || manager != scale.owner || window != root + 4 ||
        !readable_range(event, 28) || *(unsigned int*)(event + 8) != 20 ||
        scale.applied_percent == 100) return result;
    transform = *(float**)(event + 24);
    if (!readable_range(transform, 68)) return result;
    factor = scale.applied_percent / 100.0f;
    center_x = (*(float*)(root + 100) + *(float*)(root + 108)) * 0.5f;
    center_y = (*(float*)(root + 104) + *(float*)(root + 112)) * 0.5f;
    /* Native transforms pivot about the window center. Compensate that pivot
     * so a logical top-left of (0, 0) still presents at physical (0, 0). */
    for (row = 0; row < 4; ++row) {
        transform[row * 4] *= factor;
        transform[row * 4 + 1] *= factor;
    }
    transform[12] += (factor - 1.0f) * center_x;
    transform[13] += (factor - 1.0f) * center_y;
    *(unsigned int*)(transform + 16) = 1;
    if (scale.reported_percent != scale.applied_percent) {
        scale.reported_percent = scale.applied_percent;
        trace_client_state("display_ui_scale_transform", scale.applied_percent);
    }
    /* BE3890 next copies and inverts this exact matrix, then propagates it
     * to descendants. Do not adjust incoming mouse positions a second time. */
    return result;
}

// Modifies: records physical resolution before queuing the logical SPUI size.
static LONG FANG_THISCALL scaled_resize(BYTE* manager, float width, float height) {
    LONG result;
    BYTE* root;
    if (manager != native_manager() || !isfinite(width) || !isfinite(height) ||
        width <= 0 || height <= 0) return native_resize(manager, width, height);
    scale.owner = manager;
    scale.physical_areas[0] = 0;
    scale.physical_areas[1] = 0;
    scale.physical_areas[2] = width;
    scale.physical_areas[3] = height;
    scale.applied_percent = scale.percent;
    result = native_resize(manager, width * 100.0f / scale.percent,
        height * 100.0f / scale.percent);
    root = scale_root(manager);
    if (root) {
        native_dirty(manager, root);
        native_redraw(manager);
    }
    return result;
}

// Modifies: queues layout and transform invalidation on the native UI thread.
void fang_update_native_ui_scale(void) {
    BYTE* manager;
    BYTE* root;
    const float* area;
    if (!scale.is_installed) return;
    manager = native_manager();
    root = scale_root(manager);
    if (!root) {
        scale.owner = NULL;
        return;
    }
    if (scale.owner != manager) {
        area = native_area(root + 4);
        if (!readable_range(area, 16) || !isfinite(area[2]) || !isfinite(area[3]) ||
            area[2] <= area[0] || area[3] <= area[1]) return;
        memcpy(scale.physical_areas, area, sizeof(scale.physical_areas));
        scale.owner = manager;
        scale.applied_percent = 100;
    }
    trace_scale_geometry(manager, root);
    if (scale.applied_percent == scale.percent) return;
    scale.applied_percent = scale.percent;
    native_resize(manager, (scale.physical_areas[2] - scale.physical_areas[0]) * 100.0f / scale.percent,
        (scale.physical_areas[3] - scale.physical_areas[1]) * 100.0f / scale.percent);
    native_dirty(manager, root);
    /* BE20B0 invalidates transforms only. BE1AE0 also discards the incremental
     * packet-update queue and requests BE4A50's complete paint-tree rebuild,
     * including the popup frame and its separately cached text draw commands. */
    native_redraw(manager);
    trace_client_state("display_ui_scale_applied", scale.applied_percent);
}

// Modifies: validates build-103 seams and installs the paired layout/input hooks.
int fang_install_native_ui_scale(BYTE* base) {
    void** resize_slot = (void**)(base + 0xC5E7A4);
    void** area_slot = (void**)(base + 0xC16474);
    void** mouse_slot = (void**)(base + 0xC165A4);
    void** down_slot = (void**)(base + 0xC1659C);
    void** up_slot = (void**)(base + 0xC165A0);
    void** wheel_slot = (void**)(base + 0xC165A8);
    void** render_slot = (void**)(base + 0xC15E44);
    static const BYTE dirty_signature[] = {0x8B, 0x54, 0x24, 0x04, 0xF6, 0x42, 0x30, 0x20};
    static const BYTE redraw_signature[] = {0x53, 0x33, 0xDB, 0x38, 0x59, 0x25};
    if (!readable_range(resize_slot, 4) || !readable_range(area_slot, 4) ||
        !readable_range(mouse_slot, 4) || *mouse_slot != base + 0x5198F0 ||
        !readable_range(down_slot, 4) || *down_slot != base + 0x519660 ||
        !readable_range(up_slot, 4) || *up_slot != base + 0x5197C0 ||
        !readable_range(wheel_slot, 4) || *wheel_slot != base + 0x5199C0 ||
        !readable_range(render_slot, 4) || *render_slot != base + 0x512EF0 ||
        !readable_range(base + 0x512EF0, 11) ||
        *(unsigned short*)(base + 0x512EF0) != 0x0D8B ||
        *(BYTE**)(base + 0x512EF2) != base + 0x10BBAD8 ||
        base[0x512EF6] != 0xE9 ||
        base + 0x512EFB + *(int*)(base + 0x512EF7) != base + 0x512AB0 ||
        !readable_range(base + 0x7E1900, 6) ||
        !readable_range(base + 0x508D3F, 5) ||
        memcmp(base + 0x508D3F, "\x8B\x42\x34\xFF\xD0", 5) != 0 ||
        !readable_range(base + 0x5193DC, 2) || !readable_range(base + 0x51942E, 2) ||
        !readable_range(base + 0x7E20B0, sizeof(dirty_signature)) ||
        memcmp(base + 0x7E20B0, dirty_signature, sizeof(dirty_signature)) ||
        !readable_range(base + 0x7E1AE0, sizeof(redraw_signature)) ||
        memcmp(base + 0x7E1AE0, redraw_signature, sizeof(redraw_signature)) ||
        base[0x7E1900] != 0xA1 || base[0x7E1905] != 0xC3 ||
        *(BYTE**)(base + 0x7E1901) != base + 0x11DE67C ||
        *(unsigned short*)(base + 0x5193DC) != 0xD0FF ||
        *(unsigned short*)(base + 0x51942E) != 0xD0FF ||
        *resize_slot != base + 0x7E4020 || *area_slot != base + 0x7DE930) return 0;
    scale_image = base;
    native_manager = (scale_manager_fn)(base + 0x7E1900);
    native_resize = (scale_resize_fn)(base + 0x7E4020);
    native_area = (scale_area_fn)(base + 0x7DE930);
    native_event = (scale_event_fn)(base + 0x7E2C50);
    native_dirty = (scale_dirty_fn)(base + 0x7E20B0);
    native_redraw = (scale_redraw_fn)(base + 0x7E1AE0);
    native_mouse = (scale_mouse_fn)(base + 0x5198F0);
    native_button_down = (scale_button_fn)(base + 0x519660);
    native_button_up = (scale_button_fn)(base + 0x5197C0);
    native_wheel = (scale_button_fn)(base + 0x5199C0);
    native_render = (scale_render_fn)(base + 0x512EF0);
    scale.percent = fang_ui_scale_load();
    requested_percent = scale.percent;
    if (!patch_pointer(area_slot, native_area, physical_input_area)) return 0;
    if (!patch_call(base + 0x7E38D7, native_event, scaled_transform)) goto restore_area;
    if (!patch_pointer(resize_slot, native_resize, scaled_resize)) goto restore_transform;
    if (!patch_pointer(mouse_slot, native_mouse, scaled_root_mouse)) goto restore_resize;
    if (!patch_pointer(down_slot, native_button_down, scaled_root_button_down)) goto restore_mouse;
    if (!patch_pointer(up_slot, native_button_up, scaled_root_button_up)) goto restore_down;
    if (!patch_pointer(wheel_slot, native_wheel, scaled_root_wheel)) goto restore_up;
    if (!patch_pointer(render_slot, native_render, scaled_render)) goto restore_wheel;
    scale.is_installed = 1;
    trace_client_state("display_ui_scale_loaded", scale.percent);
    return 1;
restore_wheel:
    if (!patch_pointer(wheel_slot, scaled_root_wheel, native_wheel))
        trace_client_state("display_ui_scale_wheel_rollback_failed", 1);
restore_up:
    if (!patch_pointer(up_slot, scaled_root_button_up, native_button_up))
        trace_client_state("display_ui_scale_up_rollback_failed", 1);
restore_down:
    if (!patch_pointer(down_slot, scaled_root_button_down, native_button_down))
        trace_client_state("display_ui_scale_down_rollback_failed", 1);
restore_mouse:
    if (!patch_pointer(mouse_slot, scaled_root_mouse, native_mouse))
        trace_client_state("display_ui_scale_mouse_rollback_failed", 1);
restore_resize:
    if (!patch_pointer(resize_slot, scaled_resize, native_resize))
        trace_client_state("display_ui_scale_resize_rollback_failed", 1);
restore_transform:
    if (!patch_call(base + 0x7E38D7, scaled_transform, native_event))
        trace_client_state("display_ui_scale_transform_rollback_failed", 1);
restore_area:
    if (!patch_pointer(area_slot, physical_input_area, native_area))
        trace_client_state("display_ui_scale_area_rollback_failed", 1);
    return 0;
}

#include "display/display.h"

#include <string.h>

/* Build 103 Options.swf: add live controls using its CheckBox and Label symbols.
 * Native settings (including No Blood) and packaged movies remain intact. */
typedef struct display_value {
    void* object_interface;
    unsigned int type;
    union { double number; const char* string; void* object; } data;
} display_value;

typedef char display_value_size_must_be_16[sizeof(display_value) == 16 ? 1 : -1];
typedef unsigned char (FANG_THISCALL* member_fn)(void*, void*,
    const char*, display_value*, int);
typedef unsigned char (FANG_THISCALL* invoke_fn)(display_value*,
    const char*, display_value*, const display_value*, unsigned int);
typedef void (FANG_THISCALL* release_fn)(display_value*);
typedef void (__cdecl* event_fn)(BYTE*, const char*, display_value*);
typedef void (FANG_THISCALL* show_fn)(BYTE*, unsigned char);
typedef void (FANG_THISCALL* destroy_fn)(BYTE*);

static member_fn get_member;
static member_fn set_member;
static invoke_fn invoke;
static release_fn release_value;
static event_fn get_graphics;
static event_fn save_settings;
static show_fn show_options;
static destroy_fn destroy_options;
static BYTE* options_controller;
static struct {
    int is_pending;
    int is_borderless;
    int is_overlay;
    unsigned int stage;
} selection;

// Reads: returns a Scaleform boolean value for the requested state.
static display_value boolean_value(int is_enabled) {
    display_value result = {0};
    result.type = 2;
    *(unsigned char*)&result.data = is_enabled != 0;
    return result;
}

// Reads: returns a Scaleform numeric value.
static display_value number_value(double number) {
    display_value result = {0};
    result.type = 3;
    result.data.number = number;
    return result;
}

// Reads: returns a Scaleform string value referencing the supplied text.
static display_value string_value(const char* string) {
    display_value result = {0};
    result.type = 4;
    result.data.string = string;
    return result;
}

// Reads: checks whether a Scaleform value has an object type and interface.
static int is_object(display_value* property) {
    unsigned int type = property->type & 0x8F;
    return type >= 6 && type <= 8 && property->object_interface != NULL;
}

// Modifies: retrieves a Scaleform member into the output value through the native accessor.
static int get(display_value* parent, const char* name, display_value* result) {
    if (!is_object(parent)) {
        return 0;
    }
    return get_member(parent->object_interface, parent->data.object, name, result,
        (parent->type & 0x8F) == 8);
}

// Modifies: assigns a Scaleform member through the native setter.
static int set(display_value* parent, const char* name, display_value property) {
    if (!is_object(parent)) {
        return 0;
    }
    return set_member(parent->object_interface, parent->data.object, name, &property,
        (parent->type & 0x8F) == 8);
}

// Modifies: writes a boolean member to the caller's output and releases the native temporary value.
static int read_boolean(display_value* parent, const char* name, int* is_enabled) {
    display_value property = {0};
    int is_valid = get(parent, name, &property) && (property.type & 0x8F) == 2;
    if (is_valid) {
        *is_enabled = *(unsigned char*)&property.data != 0;
    }
    release_value(&property);
    return is_valid;
}

// Modifies: writes a numeric member to the caller's output and releases the native temporary value.
static int read_number(display_value* parent, const char* name, double* number) {
    display_value property = {0};
    int is_valid = get(parent, name, &property) && (property.type & 0x8F) == 3;
    if (is_valid) {
        *number = property.data.number;
    }
    release_value(&property);
    return is_valid;
}

// Modifies: updates a Scaleform boolean member only when its current value differs.
static int set_boolean(display_value* parent, const char* name, int is_enabled) {
    int is_previous;
    if (read_boolean(parent, name, &is_previous) && is_previous == is_enabled) {
        return 1;
    }
    return set(parent, name, boolean_value(is_enabled));
}

// Modifies: resolves the graphics tab into an output value and releases temporary native references.
static int graphics_tab(BYTE* controller, display_value* graphics) {
    display_value scene = {0}, container = {0};
    int is_found = controller != NULL &&
        get((display_value*)(controller + 8), "mainScene", &scene) &&
        get(&scene, "emptyMC", &container) && get(&container, "tab0", graphics) &&
        is_object(graphics);
    release_value(&container);
    release_value(&scene);
    return is_found;
}

// Modifies: resolves a sibling label by movie depth into the output value and releases the depth value.
static int sibling_label(display_value* graphics, display_value* button,
    int depth_offset, display_value* label) {
    display_value depth = {0};
    int is_found = 0;
    if (invoke(button, "getDepth", &depth, NULL, 0) && (depth.type & 0x8F) == 3) {
        depth.data.number += depth_offset;
        is_found = invoke(graphics, "getInstanceAtDepth", label, &depth, 1) &&
            is_object(label);
    }
    release_value(&depth);
    return is_found;
}

// Modifies: creates and initializes a live options control through native movie methods and properties.
static int clone_control(display_value* graphics, display_value* source,
    const char* name, int depth, const char* caption, int is_checkbox) {
    display_value clone = {0};
    display_value arguments[2] = {string_value(name), number_value(depth)};
    double width, height;
    int is_ready = 0, is_complete = 0;
    if (!get(graphics, name, &clone) || !is_object(&clone)) {
        /* duplicateMovieClip may create the child without returning it. */
        invoke(source, "duplicateMovieClip", NULL, arguments, 2);
        release_value(&clone);
        memset(&clone, 0, sizeof(clone));
        if (!get(graphics, name, &clone) || !is_object(&clone)) {
            goto done;
        }
    }
    if (read_boolean(&clone, "isFangDisplayReady", &is_ready) && is_ready) {
        is_complete = 1;
        goto done;
    }
    if (!read_number(source, "_width", &width) || !read_number(source, "_height", &height) ||
        !set(&clone, "__width", number_value(width)) ||
        !set(&clone, "__height", number_value(height)) ||
        !set(&clone, "bindingEnabled", boolean_value(0)) ||
        !set(&clone, "_autoSize", boolean_value(0))) {
        goto done;
    }
    /* Dynamic instances lack authored placement initialization. Run the stock
     * lifecycle so CheckBox installs its mouse handlers and toggle behavior. */
    if (is_checkbox) {
        arguments[0] = string_value("up");
        if (!set(&clone, "_label", string_value("")) ||
            !invoke(&clone, "gotoAndStop", NULL, arguments, 1) ||
            !invoke(&clone, "onLoadImpl", NULL, NULL, 0) ||
            !invoke(&clone, "setState", NULL, arguments, 1) ||
            !set_boolean(&clone, "selected", 0)) {
            goto done;
        }
    } else {
        if (!set(&clone, "_text", string_value(caption)) ||
            !invoke(&clone, "onLoadImpl", NULL, NULL, 0) ||
            !set(&clone, "text", string_value(caption))) {
            goto done;
        }
    }
    arguments[0] = number_value(width);
    arguments[1] = number_value(height);
    is_complete = invoke(&clone, "setSize", NULL, arguments, 2) &&
        invoke(&clone, "validateNow", NULL, NULL, 0) &&
        set_boolean(&clone, "disabled", 0) && set(&clone, "_visible", boolean_value(1)) &&
        set(&clone, "_alpha", number_value(100)) &&
        set(&clone, "isFangDisplayReady", boolean_value(1));
done:
    release_value(&clone);
    return is_complete;
}

// Modifies: creates the live borderless and taskbar overlay checkboxes and labels.
static int create_controls(display_value* graphics) {
    display_value source = {0}, label = {0};
    int is_created = get(graphics, "noBlood", &source) &&
        sibling_label(graphics, &source, -2, &label) &&
        clone_control(graphics, &source, "fangBorderless", 18000, "", 1) &&
        clone_control(graphics, &label, "fangBorderlessLabel", 18001, "Borderless Mode", 0) &&
        clone_control(graphics, &source, "fangTaskbarOverlay", 18002, "", 1) &&
        clone_control(graphics, &label, "fangTaskbarOverlayLabel", 18003, "Taskbar Overlay", 0);
    release_value(&label);
    release_value(&source);
    return is_created;
}

// Modifies: returns a control's original vertical position and caches it on the live control when absent.
static int original_y(display_value* control, double* y) {
    if (read_number(control, "fangOriginalY", y)) {
        return 1;
    }
    return read_number(control, "_y", y) && set(control, "fangOriginalY", number_value(*y));
}

// Modifies: caches original positions and repositions the nine live graphics options rows.
static int layout_controls(display_value* graphics) {
    /* Nine rows fit in the authored seven-row panel by reducing only spacing.
     * Keep checkbox and label sizes, and place the two display modes together.
     * Label offsets are relative live depths, including Scaleform's offset. */
    static const struct { const char* name; int label_offset; } rows[] = {
        {"cloudShadows", 24}, {"antialiasing", 24}, {"fullScreen", 24},
        {"fangBorderless", 1}, {"fangTaskbarOverlay", 1}, {"vsync", 4},
        {"spotlightShadows", 4}, {"noBlood", -2}, {"insaneGrass", -2},
    };
    display_value first = {0}, last = {0}, source = {0}, source_label = {0};
    double top, bottom, button_x, label_x, source_y, label_y;
    int is_laid_out = 0;
    if (!get(graphics, "cloudShadows", &first) || !get(graphics, "insaneGrass", &last) ||
        !original_y(&first, &top) || !original_y(&last, &bottom) || bottom <= top ||
        !get(graphics, "noBlood", &source) || !sibling_label(graphics, &source, -2, &source_label) ||
        !read_number(&source, "_x", &button_x) || !read_number(&source_label, "_x", &label_x) ||
        !original_y(&source, &source_y) || !original_y(&source_label, &label_y)) {
        goto done;
    }
    for (unsigned int index = 0; index < sizeof(rows) / sizeof(rows[0]); ++index) {
        display_value button = {0}, label = {0};
        double button_y, caption_y;
        double y = top + (bottom - top) * index / 8;
        int is_row_ready = get(graphics, rows[index].name, &button) &&
            sibling_label(graphics, &button, rows[index].label_offset, &label);
        if (is_row_ready && (index == 3 || index == 4)) {
            button_y = source_y;
            caption_y = label_y;
            is_row_ready = set(&button, "_x", number_value(button_x)) &&
                set(&label, "_x", number_value(label_x));
        } else if (is_row_ready) {
            is_row_ready = original_y(&button, &button_y) && original_y(&label, &caption_y);
        }
        is_row_ready = is_row_ready && set(&button, "_y", number_value(y)) &&
            set(&label, "_y", number_value(y + caption_y - button_y));
        release_value(&label);
        release_value(&button);
        if (!is_row_ready) {
            goto done;
        }
    }
    is_laid_out = 1;
done:
    release_value(&source_label);
    release_value(&source);
    release_value(&last);
    release_value(&first);
    return is_laid_out;
}

// Modifies: creates and synchronizes display controls, reconciles selections, and logs UI progress.
void fang_update_display_ui(void) {
    display_value graphics = {0}, borderless = {0}, overlay = {0}, fullscreen = {0};
    int is_fullscreen, is_borderless, is_layout_ready = 0;
    unsigned int stage = 1;
    if (options_controller == NULL || !options_controller[0x28]) {
        return;
    }
    if (!graphics_tab(options_controller, &graphics)) {
        goto done;
    }
    stage = 2;
    if (!read_boolean(&graphics, "isFangDisplayLayoutReady", &is_layout_ready) || !is_layout_ready) {
        if (!create_controls(&graphics) || !layout_controls(&graphics) ||
            !set(&graphics, "isFangDisplayLayoutReady", boolean_value(1))) {
            goto done;
        }
    }
    stage = 3;
    if (!get(&graphics, "fangBorderless", &borderless) ||
        !get(&graphics, "fangTaskbarOverlay", &overlay) ||
        !get(&graphics, "fullScreen", &fullscreen)) {
        goto done;
    }
    if (selection.is_pending) {
        if (!set_boolean(&borderless, "selected", selection.is_borderless) ||
            !set_boolean(&overlay, "selected", selection.is_overlay)) {
            goto done;
        }
        selection.is_pending = 0;
    }
    if (!read_boolean(&fullscreen, "selected", &is_fullscreen) ||
        !read_boolean(&borderless, "selected", &is_borderless)) {
        goto done;
    }
    /* Both modes may be off for an ordinary decorated window. */
    if (is_fullscreen && is_borderless) {
        if (!set_boolean(&borderless, "selected", 0)) {
            goto done;
        }
        is_borderless = 0;
    }
    if (set_boolean(&borderless, "disabled", is_fullscreen) &&
        set_boolean(&fullscreen, "disabled", is_borderless)) {
        stage = 4;
    }
done:
    if (selection.stage != stage) {
        selection.stage = stage;
        trace_client_state("display_options_ui_stage", stage);
    }
    release_value(&fullscreen);
    release_value(&overlay);
    release_value(&borderless);
    release_value(&graphics);
}

// Modifies: queues current or default display selections for the options UI.
static void load_selection(int is_default) {
    selection.is_borderless = !is_default && fang_borderless_requested();
    selection.is_overlay = !is_default && fang_taskbar_overlay_requested();
    selection.is_pending = 1;
}

// Modifies: tracks options visibility, queues display selections, and invokes the native show handler.
static void FANG_THISCALL hooked_show(BYTE* controller, unsigned char is_visible) {
    options_controller = is_visible ? controller : NULL;
    selection.stage = 0;
    if (is_visible) {
        load_selection(0);
    }
    show_options(controller, is_visible);
}

// Modifies: clears the tracked options controller and invokes native destruction.
static void FANG_THISCALL hooked_destroy(BYTE* controller) {
    if (options_controller == controller) {
        options_controller = NULL;
    }
    destroy_options(controller);
}

// Modifies: queues current or default display selections and invokes the native graphics handler.
static void __cdecl hooked_get_graphics(BYTE* controller, const char* event,
    display_value* arguments) {
    int is_default = arguments != NULL && (arguments[0].type & 0x8F) == 2 &&
        *(unsigned char*)&arguments[0].data != 0;
    load_selection(is_default);
    get_graphics(controller, event, arguments);
}

// Modifies: reads display selections, queues display changes, and invokes native settings saving.
static void __cdecl hooked_save_settings(BYTE* controller, const char* event,
    display_value* arguments) {
    display_value graphics = {0}, borderless = {0}, overlay = {0};
    int is_borderless = selection.is_borderless, is_overlay = selection.is_overlay;
    double fullscreen;
    /* StartSaveAndClose has a fixed list of numeric graphics fields. Read our
     * additional controls directly; leave its complete native DTO intact. */
    int is_valid = arguments != NULL && read_number(arguments, "fullScreen", &fullscreen) &&
        (fullscreen == 0 || fullscreen == 1);
    if (is_valid && !selection.is_pending) {
        is_valid = graphics_tab(controller, &graphics) &&
            get(&graphics, "fangBorderless", &borderless) &&
            get(&graphics, "fangTaskbarOverlay", &overlay) &&
            read_boolean(&borderless, "selected", &is_borderless) &&
            read_boolean(&overlay, "selected", &is_overlay);
    }
    if (is_valid) {
        fang_request_borderless(is_borderless && fullscreen == 0);
        fang_request_taskbar_overlay(is_overlay);
    } else {
        trace_client_state("display_options_selection_invalid", 1);
    }
    release_value(&overlay);
    release_value(&borderless);
    release_value(&graphics);
    save_settings(controller, event, arguments);
}

// Modifies: binds Scaleform operations and patches options callbacks with rollback on failure.
int fang_install_display_ui(BYTE* base) {
    void** show_slot = (void**)(base + 0xBD14A8);
    void** get_slot = (void**)(base + 0xD10240);
    void** save_slot = (void**)(base + 0xD10250);
    if (!readable_range(show_slot, sizeof(void*)) || !readable_range(get_slot, 0x14) ||
        *show_slot != base + 0x3B8C0 || *get_slot != base + 0x3CB50 ||
        *save_slot != base + 0x3E2C0) {
        return 0;
    }
    get_member = (member_fn)(base + 0x14DAE0);
    set_member = (member_fn)(base + 0x14B510);
    invoke = (invoke_fn)(base + 0x15A0);
    release_value = (release_fn)(base + 0x17C0);
    get_graphics = (event_fn)(base + 0x3CB50);
    save_settings = (event_fn)(base + 0x3E2C0);
    show_options = (show_fn)(base + 0x3B8C0);
    destroy_options = (destroy_fn)(base + 0x3C760);
    if (!patch_pointer(show_slot, show_options, hooked_show)) {
        return 0;
    }
    if (!patch_pointer(get_slot, get_graphics, hooked_get_graphics)) {
        goto restore_show;
    }
    if (!patch_pointer(save_slot, save_settings, hooked_save_settings)) {
        goto restore_get;
    }
    if (!patch_call(base + 0x3C8A3, destroy_options, hooked_destroy)) {
        goto restore_save;
    }
    return 1;
restore_save:
    if (!patch_pointer(save_slot, hooked_save_settings, save_settings)) {
        trace_client_state("display_save_hook_restore_failed", 1);
    }
restore_get:
    if (!patch_pointer(get_slot, hooked_get_graphics, get_graphics)) {
        trace_client_state("display_get_hook_restore_failed", 1);
    }
restore_show:
    if (!patch_pointer(show_slot, hooked_show, show_options)) {
        trace_client_state("display_show_hook_restore_failed", 1);
    }
    return 0;
}

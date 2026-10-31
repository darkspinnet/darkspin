#include "display/display.h"

#include <string.h>
#include <stdio.h>
#include <math.h>

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
static BYTE* display_image;
enum display_control_kind { DISPLAY_LABEL, DISPLAY_CHECKBOX, DISPLAY_SLIDER };
static struct {
    int is_pending;
    int is_borderless;
    int is_overlay;
    int scale_percent;
    int is_scale_pending;
    int is_scale_save_failed;
    int failed_scale_percent;
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

/* Build 103: Party.swf HUDParty.ShowContextMenu calls Container.ShowContextMenu,
 * which attaches a clip named ContextMenu to mContainer.mRoot. 442350 retains
 * Party's gData at controller+1360 and marks it ready at +1376; 4258E0 resolves
 * that controller from GameUI+1732. Scale this clip only. GFx uses its display
 * transform for button hit testing and local mouse coordinates as well. */
// Modifies: magnifies only the live portrait context menu, preserving its anchor.
static void update_context_menu_scale(void) {
    BYTE* gameui;
    BYTE* party;
    display_value container = {0}, root = {0}, menu = {0};
    double x_scale = 0, y_scale = 0;
    int percent = fang_native_ui_scale();
    if (!display_image) return;
    gameui = *(BYTE**)(display_image + 0xDBEC40);
    if (!readable_range(gameui, 1736)) return;
    party = *(BYTE**)(gameui + 1732);
    if (!readable_range(party, 1377) || !party[1376]) return;
    if (!get((display_value*)(party + 1360), "mContainer", &container) ||
        !get(&container, "mRoot", &root) || !get(&root, "ContextMenu", &menu) ||
        !is_object(&menu)) goto done;
    if (read_number(&menu, "_xscale", &x_scale) && x_scale == percent &&
        read_number(&menu, "_yscale", &y_scale) && y_scale == percent) goto done;
    if (set(&menu, "_xscale", number_value(percent)) &&
        set(&menu, "_yscale", number_value(percent))) {
        trace_client_state("display_context_menu_scale_applied", percent);
    }
done:
    release_value(&menu);
    release_value(&root);
    release_value(&container);
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

// Modifies: initializes a cloned slider's native thumb or track mouse handlers.
static int initialize_slider_button(display_value* slider, const char* name) {
    display_value button = {0};
    display_value state = string_value("up");
    /* Build 103 Button.configUI installs onPress/onRelease. A duplicated
     * compound control must initialize its children before Slider.configUI
     * subscribes to their press events. onLoadImpl alone can return early
     * when the duplicate already has initialized set. */
    int is_ready = get(slider, name, &button) && is_object(&button) &&
        set(&button, "bindingEnabled", boolean_value(0)) &&
        invoke(&button, "gotoAndStop", NULL, &state, 1) &&
        set(&button, "initialized", boolean_value(0)) &&
        invoke(&button, "onLoadImpl", NULL, NULL, 0) &&
        invoke(&button, "setState", NULL, &state, 1) &&
        set(&button, "disabled", boolean_value(0)) &&
        set(&button, "enabled", boolean_value(1));
    release_value(&button);
    return is_ready;
}

// Modifies: creates and initializes a live options control through native movie methods and properties.
static int clone_control(display_value* graphics, display_value* source,
    const char* name, int depth, const char* caption, enum display_control_kind kind) {
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
    if (kind == DISPLAY_SLIDER) {
        display_value state = string_value("default");
        if (!invoke(&clone, "gotoAndStop", NULL, &state, 1) ||
            !initialize_slider_button(&clone, "track") ||
            !initialize_slider_button(&clone, "thumb") ||
            !set(&clone, "maximum", number_value(300)) ||
            !set(&clone, "minimum", number_value(100)) ||
            !set(&clone, "snapInterval", number_value(5)) ||
            !set_boolean(&clone, "snapping", 1) ||
            !set_boolean(&clone, "liveDragging", 1) ||
            !set(&clone, "offsetLeft", number_value(0)) ||
            !set(&clone, "offsetRight", number_value(0)) ||
            !set(&clone, "value", number_value(100)) ||
            !set(&clone, "initialized", boolean_value(0)) ||
            !set(&clone, "_disabled", boolean_value(0)) ||
            !set(&clone, "enabled", boolean_value(1)) ||
            !set(&clone, "focusEnabled", boolean_value(1)) ||
            !set(&clone, "tabEnabled", boolean_value(1)) ||
            !invoke(&clone, "onLoadImpl", NULL, NULL, 0)) goto done;
    } else if (kind == DISPLAY_CHECKBOX) {
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
        clone_control(graphics, &source, "fangBorderless", 18000, "", DISPLAY_CHECKBOX) &&
        clone_control(graphics, &label, "fangBorderlessLabel", 18001, "Borderless Mode (restart required)", DISPLAY_LABEL) &&
        clone_control(graphics, &source, "fangTaskbarOverlay", 18002, "", DISPLAY_CHECKBOX) &&
        clone_control(graphics, &label, "fangTaskbarOverlayLabel", 18003, "Taskbar Overlay", DISPLAY_LABEL);
    release_value(&label);
    release_value(&source);
    return is_created;
}

/* Build 103 Options_Graphics.swf: brightness is at depth 176; its panel,
 * heading, and readout are at 22, 128, and 180. Resolve relative live depths
 * so GFx's depth offset is retained. All symbols come from installed assets. */
// Modifies: clones the complete installed brightness grouping for UI scaling.
static int create_scale_control(display_value* graphics) {
    display_value source = {0}, panel = {0}, clone = {0}, label = {0}, level = {0};
    display_value arguments[2] = {string_value("fangNativeScalePanel"), number_value(17999)};
    int is_created = get(graphics, "brightness", &source) &&
        sibling_label(graphics, &source, -154, &panel) &&
        sibling_label(graphics, &source, -48, &label) &&
        get(graphics, "brightnessLevel", &level);
    if (!is_created) goto done;
    if (!get(graphics, "fangNativeScalePanel", &clone) || !is_object(&clone)) {
        invoke(&panel, "duplicateMovieClip", NULL, arguments, 2);
        release_value(&clone);
        memset(&clone, 0, sizeof(clone));
        is_created = get(graphics, "fangNativeScalePanel", &clone) && is_object(&clone);
    }
    is_created = is_created &&
        clone_control(graphics, &source, "fangNativeScale", 18004, "", DISPLAY_SLIDER) &&
        clone_control(graphics, &label, "fangNativeScaleLabel", 18008, "Native UI Scale (restart required)", DISPLAY_LABEL) &&
        clone_control(graphics, &level, "fangNativeScaleLevel", 18006, "100%", DISPLAY_LABEL);
    if (is_created) {
        display_value readout = {0}, text = {0};
        double width;
        /* brightnessLevel has an authored 31.67% horizontal placement scale.
         * clone_control already applies its visible width through setSize;
         * retaining that placement scale would squeeze the new text twice. */
        is_created = get(graphics, "fangNativeScaleLevel", &readout) &&
            set(&readout, "_xscale", number_value(100)) &&
            read_number(&level, "_width", &width) &&
            get(&readout, "textField", &text) &&
            set(&text, "_x", number_value(0)) &&
            set(&text, "_width", number_value(width));
        release_value(&text);
        release_value(&readout);
    }
done:
    release_value(&level);
    release_value(&label);
    release_value(&clone);
    release_value(&panel);
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

/* The stock heading strips are part of one static shape containing several
 * unrelated rows. A duplicated Label contains only text. Draw a new beveled
 * strip sized from the live panel and label instead of embedding that asset. */
// Modifies: supplies the scale panel's missing title background.
static int layout_scale_heading(display_value* graphics, display_value* panel) {
    display_value heading = {0}, label = {0};
    display_value arguments[2] = {string_value("fangNativeScaleHeading"), number_value(18007)};
    double x, y, width, label_y, label_height, height, corner;
    int is_ready = 0;
    if (!get(graphics, "fangNativeScaleHeading", &heading) || !is_object(&heading)) {
        invoke(graphics, "createEmptyMovieClip", NULL, arguments, 2);
        release_value(&heading);
        memset(&heading, 0, sizeof(heading));
        if (!get(graphics, "fangNativeScaleHeading", &heading) || !is_object(&heading)) goto done;
    }
    if (!get(graphics, "fangNativeScaleLabel", &label) ||
        !read_number(panel, "_x", &x) || !read_number(panel, "_y", &y) ||
        !read_number(panel, "_width", &width) ||
        !read_number(&label, "_y", &label_y) || !read_number(&label, "_height", &label_height)) goto done;
    x += 2;
    y += 2;
    width -= 4;
    height = label_y - y + label_height;
    corner = height / 5;
    arguments[0] = number_value(0xFFFFFF);
    arguments[1] = number_value(15);
    if (!invoke(&heading, "clear", NULL, NULL, 0) ||
        !invoke(&heading, "beginFill", NULL, arguments, 2)) goto done;
    const double points[][2] = {
        {x + corner, y}, {x + width - corner, y},
        {x + width, y + corner}, {x + width, y + height - corner},
        {x + width - corner, y + height}, {x + corner, y + height},
        {x, y + height + corner}, {x, y + corner}, {x + corner, y},
    };
    for (unsigned int index = 0; index < sizeof(points) / sizeof(points[0]); ++index) {
        arguments[0] = number_value(points[index][0]);
        arguments[1] = number_value(points[index][1]);
        if (!invoke(&heading, index == 0 ? "moveTo" : "lineTo", NULL, arguments, 2)) goto done;
    }
    is_ready = invoke(&heading, "endFill", NULL, NULL, 0);
done:
    release_value(&label);
    release_value(&heading);
    return is_ready;
}

// Modifies: extends the left frame and lowers the restart message below both columns.
static int layout_graphics_footer(display_value* graphics, display_value* brightness,
    display_value* checkbox_panel) {
    display_value left_panel = {0}, tooltip = {0};
    double bottom, height, top;
    int is_ready = sibling_label(graphics, brightness, -174, &left_panel) &&
        get(graphics, "tooltip", &tooltip) &&
        read_number(checkbox_panel, "_y", &bottom) &&
        read_number(checkbox_panel, "_height", &height) &&
        read_number(&left_panel, "_y", &top);
    if (is_ready) {
        bottom += height;
        is_ready = set(&left_panel, "_height", number_value(bottom - top)) &&
            set(&tooltip, "_y", number_value(bottom + 16));
    }
    release_value(&tooltip);
    release_value(&left_panel);
    return is_ready;
}

// Modifies: places the scale grouping below brightness and moves the checkbox panel.
static int layout_scale_group(display_value* graphics, double* checkbox_shift) {
    display_value brightness = {0}, panel = {0}, checkbox_panel = {0}, scale_panel = {0};
    static const struct { const char* source; const char* target; int depth_offset; } controls[] = {
        {"brightness", "fangNativeScale", 0},
        {"brightness", "fangNativeScaleLabel", -48},
        {"brightnessLevel", "fangNativeScaleLevel", 0},
    };
    double panel_y, checkbox_y, x, y;
    int is_ready = 0;
    if (!get(graphics, "brightness", &brightness) ||
        !sibling_label(graphics, &brightness, -154, &panel) ||
        !sibling_label(graphics, &brightness, -144, &checkbox_panel) ||
        !get(graphics, "fangNativeScalePanel", &scale_panel) ||
        !original_y(&panel, &panel_y) || !original_y(&checkbox_panel, &checkbox_y) ||
        !read_number(&panel, "_x", &x)) goto done;
    *checkbox_shift = checkbox_y - panel_y;
    if (*checkbox_shift <= 0 || !set(&scale_panel, "_x", number_value(x)) ||
        !set(&scale_panel, "_y", number_value(checkbox_y)) ||
        !set(&checkbox_panel, "_y", number_value(checkbox_y + *checkbox_shift)) ||
        !set(&checkbox_panel, "_height", number_value(128))) goto done;
    for (unsigned int index = 0; index < sizeof(controls) / sizeof(controls[0]); ++index) {
        display_value source = {0}, target = {0}, heading = {0};
        int is_positioned = get(graphics, controls[index].source, &source);
        display_value* origin = &source;
        if (is_positioned && controls[index].depth_offset) {
            is_positioned = sibling_label(graphics, &source, controls[index].depth_offset, &heading);
            origin = &heading;
        }
        is_positioned = is_positioned && get(graphics, controls[index].target, &target) &&
            read_number(origin, "_x", &x) && read_number(origin, "_y", &y) &&
            set(&target, "_x", number_value(x)) &&
            set(&target, "_y", number_value(y + *checkbox_shift));
        release_value(&heading);
        release_value(&target);
        release_value(&source);
        if (!is_positioned) goto done;
    }
    is_ready = layout_scale_heading(graphics, &scale_panel) &&
        layout_graphics_footer(graphics, &brightness, &checkbox_panel);
done:
    release_value(&scale_panel);
    release_value(&checkbox_panel);
    release_value(&panel);
    release_value(&brightness);
    return is_ready;
}

// Modifies: caches original positions and repositions the nine live graphics options rows.
static int layout_controls(display_value* graphics) {
    /* Nine checkbox rows fit below the separate native scale grouping.
     * Keep checkbox and label sizes, and place the two display modes together.
     * Label offsets are relative live depths, including Scaleform's offset. */
    static const struct { const char* name; int label_offset; } rows[] = {
        {"cloudShadows", 24}, {"antialiasing", 24}, {"fullScreen", 24},
        {"fangBorderless", 1}, {"fangTaskbarOverlay", 1}, {"vsync", 4},
        {"spotlightShadows", 4}, {"noBlood", -2}, {"insaneGrass", -2},
    };
    display_value first = {0}, last = {0}, source = {0}, source_label = {0};
    double top, bottom, button_x, label_x, source_y, label_y, checkbox_shift;
    int is_laid_out = 0;
    if (!get(graphics, "cloudShadows", &first) || !get(graphics, "insaneGrass", &last) ||
        !original_y(&first, &top) || !original_y(&last, &bottom) || bottom <= top ||
        !get(graphics, "noBlood", &source) || !sibling_label(graphics, &source, -2, &source_label) ||
        !read_number(&source, "_x", &button_x) || !read_number(&source_label, "_x", &label_x) ||
        !original_y(&source, &source_y) || !original_y(&source_label, &label_y) ||
        !layout_scale_group(graphics, &checkbox_shift)) {
        goto done;
    }
    for (unsigned int index = 0; index < sizeof(rows) / sizeof(rows[0]); ++index) {
        display_value button = {0}, label = {0};
        double button_y, caption_y;
        double y = top + checkbox_shift + (bottom - top) * index / 10;
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

// Modifies: matches the stock slider artwork without inheriting its input-disabled flag.
static void update_scale_slider_style(display_value* graphics, display_value* slider) {
    display_value brightness = {0};
    static const char* names[] = {"track", "thumb"};
    if (!get(graphics, "brightness", &brightness)) goto done;
    for (unsigned int index = 0; index < sizeof(names) / sizeof(names[0]); ++index) {
        display_value source = {0}, target = {0};
        double frame, current_frame;
        if (get(&brightness, names[index], &source) && get(slider, names[index], &target) &&
            read_number(&source, "_currentframe", &frame) &&
            read_number(&target, "_currentframe", &current_frame) && frame != current_frame) {
            display_value argument = number_value(frame);
            invoke(&target, "gotoAndStop", NULL, &argument, 1);
        }
        release_value(&target);
        release_value(&source);
    }
done:
    release_value(&brightness);
}

// Modifies: labels the live build-103 resolution heading at depth 120.
static void update_screen_size_caption(display_value* graphics) {
    display_value resolution = {0}, caption = {0};
    if (get(graphics, "screenResolutions", &resolution) &&
        sibling_label(graphics, &resolution, -2, &caption)) {
        set(&caption, "text", string_value("Screen Size (restart required)"));
    }
    release_value(&caption);
    release_value(&resolution);
}

// Modifies: creates and synchronizes display controls, reconciles selections, and logs UI progress.
void fang_update_display_ui(void) {
    display_value graphics = {0}, borderless = {0}, overlay = {0}, fullscreen = {0};
    display_value slider = {0}, caption = {0}, level = {0};
    int is_fullscreen, is_borderless, is_layout_ready = 0;
    unsigned int stage = 1;
    update_context_menu_scale();
    if (options_controller == NULL || !options_controller[0x28]) {
        return;
    }
    if (!graphics_tab(options_controller, &graphics)) {
        goto done;
    }
    update_screen_size_caption(&graphics);
    stage = 2;
    if (!read_boolean(&graphics, "isFangDisplayLayoutReady", &is_layout_ready) || !is_layout_ready) {
        if (!create_controls(&graphics) || !create_scale_control(&graphics) || !layout_controls(&graphics) ||
            !set(&graphics, "isFangDisplayLayoutReady", boolean_value(1))) {
            goto done;
        }
    }
    if (!get(&graphics, "fangNativeScale", &slider) ||
        !get(&graphics, "fangNativeScaleLabel", &caption) ||
        !get(&graphics, "fangNativeScaleLevel", &level)) goto done;
    update_scale_slider_style(&graphics, &slider);
    if (selection.is_scale_pending) {
        if (!set(&slider, "value", number_value(selection.scale_percent))) goto done;
        selection.is_scale_pending = 0;
    }
    double percent;
    char scale_caption[64];
    if (read_number(&slider, "value", &percent) && isfinite(percent) && percent >= 100 && percent <= 300) {
        if ((int)percent != selection.failed_scale_percent) selection.is_scale_save_failed = 0;
        if (selection.is_scale_save_failed) {
            _snprintf(scale_caption, sizeof(scale_caption), "Could not save UI scale");
        } else {
            _snprintf(scale_caption, sizeof(scale_caption), "Native UI Scale (restart required)");
        }
        set(&caption, "text", string_value(scale_caption));
        _snprintf(scale_caption, sizeof(scale_caption), "%d%%", (int)percent);
        set(&level, "text", string_value(scale_caption));
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
    release_value(&caption);
    release_value(&slider);
    release_value(&level);
}

// Modifies: queues current or default display selections for the options UI.
static void load_selection(int is_default) {
    selection.is_borderless = !is_default && fang_borderless_requested();
    selection.is_overlay = !is_default && fang_taskbar_overlay_requested();
    selection.scale_percent = is_default ? 100 : fang_native_ui_scale_requested();
    selection.is_scale_pending = 1;
    selection.is_scale_save_failed = 0;
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
    display_value graphics = {0}, borderless = {0}, overlay = {0}, slider = {0};
    int is_borderless = selection.is_borderless, is_overlay = selection.is_overlay;
    double fullscreen;
    double percent = selection.scale_percent;
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
    if (is_valid && !selection.is_scale_pending) {
        is_valid = (is_object(&graphics) || graphics_tab(controller, &graphics)) &&
            get(&graphics, "fangNativeScale", &slider) &&
            read_number(&slider, "value", &percent);
    }
    is_valid = is_valid && isfinite(percent) && percent >= 100 && percent <= 300 &&
        percent == (int)percent && (int)percent % 5 == 0;
    if (is_valid && !fang_request_native_ui_scale((int)percent)) {
        selection.is_scale_save_failed = 1;
        selection.failed_scale_percent = (int)percent;
        display_value caption = {0};
        if (get(&graphics, "fangNativeScaleLabel", &caption))
            set(&caption, "text", string_value("Could not save UI scale"));
        release_value(&caption);
        release_value(&slider);
        release_value(&overlay);
        release_value(&borderless);
        release_value(&graphics);
        return;
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
    release_value(&slider);
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
    display_image = base;
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

#include "hook.h"

#include <stdint.h>
#include <stdio.h>
#include <string.h>

/* Build-103 ContextMenu.swf supplies the themed button and dispatcher.
 * Give the additional control a private receiver/name, so the ordinary Edit
 * button continues to use its native callback. Menu teardown removes our
 * registration before its actor context or GFx objects can be freed.
 */
typedef struct editor_value {
    void* object_interface;
    unsigned int type;
    union { double number; const char* string; void* object; } data;
} editor_value;

typedef char editor_value_size[sizeof(editor_value) == 16 ? 1 : -1];
typedef unsigned char (FANG_THISCALL* editor_member_fn)(void*, void*, const char*, editor_value*, int);
typedef unsigned char (FANG_THISCALL* editor_invoke_fn)(editor_value*, const char*, editor_value*, const editor_value*, unsigned int);
typedef void (FANG_THISCALL* editor_release_fn)(editor_value*);
typedef void (FANG_THISCALL* editor_destroy_fn)(BYTE*);
typedef BYTE* (__cdecl* editor_ui_manager_fn)(void);
typedef void (FANG_THISCALL* editor_register_fn)(BYTE*, BYTE*, const char*, void (__cdecl*)(BYTE*));
typedef void (FANG_THISCALL* editor_unregister_fn)(BYTE*, const char*);
typedef void (FANG_THISCALL* editor_copy_fn)(editor_value*, const editor_value*);
typedef void (FANG_THISCALL* editor_load_fn)(BYTE*, const char*, const char*, double,
    void (__cdecl*)(void*, unsigned int, editor_value*), void*);
typedef void (FANG_THISCALL* editor_mode_fn)(BYTE*, unsigned int);
typedef BYTE* (FANG_THISCALL* editor_find_window_fn)(BYTE*, uint32_t);
typedef const float* (FANG_THISCALL* editor_area_fn)(BYTE*);
typedef float* (FANG_THISCALL* editor_point_fn)(BYTE*, float*, float, float);
typedef BYTE* (FANG_THISCALL* editor_button_fn)(BYTE*, uint32_t);
typedef void (FANG_THISCALL* editor_button_flag_fn)(BYTE*, unsigned int, int);
typedef void (FANG_THISCALL* editor_repaint_fn)(BYTE*);
typedef void (FANG_THISCALL* editor_window_flag_fn)(BYTE*, unsigned int, unsigned char);

static BYTE* editor_ui_base;
static editor_member_fn editor_get_member;
static editor_member_fn editor_set_member;
static editor_invoke_fn editor_invoke;
static editor_release_fn editor_release;
static editor_destroy_fn editor_destroy;
static int is_editor_ui_enabled;
static unsigned int editor_ui_failure_count;
static BYTE body_context[72];
static BYTE* body_owner;
static char body_name[64];
static unsigned int body_generation;
static int is_body_requested;
static HWND body_window;
static WNDPROC body_window_proc;
static RECT body_area;
static int is_body_pressed;
static unsigned int body_mode = UINT32_MAX;
static editor_mode_fn editor_native_mode;
static double body_width;
static double body_height;
static int is_body_selected;
static RECT validation_area;
static unsigned int validation_page;
static unsigned int validation_pages;
static UINT validation_press;
static BYTE scene_context[72];
static unsigned int scene_generation;
static unsigned int scene_target;
static uint32_t scene_identity[3];
static int is_scene_requested;
static char scene_name[64];

static void __cdecl editor_body_toggle(BYTE* context);

/* EditorUI::SetMode changes palettes and editor state, while the ordinary
 * radio buttons update their own selection through their click dispatcher.
 * Our separate control must synchronize those visuals through IButton, using
 * the same GetObject/SetButtonFlag/Invalidate calls as native EditorUI. */
static void editor_body_select_tabs(BYTE* editor, BYTE* ui) {
    static const uint32_t ids[] = {0xf019c2e7, 0xf019c2f3, 0x70218642, 0x0aa07258};
    unsigned int mode = *(unsigned int*)(editor + 856);
    unsigned int index;
    if (body_mode == mode) return;
    for (index = 0; index < sizeof(ids) / sizeof(ids[0]); ++index) {
        BYTE* tab = ((editor_find_window_fn)(*(void***)ui)[18])(ui, ids[index]);
        BYTE* button;
        int is_selected = (index == 0 && mode == 3) ||
            (index == 1 && (mode == 1 || mode == 4)) || (index == 2 && mode == 2) ||
            (index == 3 && mode == 3);
        if (!readable_range(tab, 4) || !readable_range(*(void**)tab, 152)) continue;
        button = ((editor_button_fn)(*(void***)tab)[3])(tab, 0x8ed27e7a);
        if (!readable_range(button, 4) || !readable_range(*(void**)button, 44)) continue;
        ((editor_button_flag_fn)(*(void***)button)[10])(button, 4, is_selected);
        ((editor_repaint_fn)(*(void***)tab)[37])(tab);
    }
    body_mode = mode;
    trace_client_state("mod_editor_tab_selection", mode);
}

static void FANG_THISCALL editor_body_native_mode(BYTE* ui, unsigned int mode) {
    BYTE* editor = fang_mod_editor_active();
    /* The shipped equipment tab also requests the general creator's mode 0.
     * Give it the retained equipment mode 3 for this explicit editor instance;
     * only our Body entry calls the native routine with Build mode 0. */
    if (editor && *(BYTE**)(editor + 128) == ui && mode == 0) mode = 3;
    editor_native_mode(ui, mode);
    if (editor && *(BYTE**)(editor + 128) == ui) fang_mod_editor_update_body();
}

/* The native SPUI editor owns mouse dispatch. A movie drawn over that editor
 * is not an Arsenal Flash input target. Route only our separate tab's click
 * through its owning game window, on the native UI thread. */
static LRESULT CALLBACK editor_body_window_proc(HWND window, UINT message, WPARAM wparam, LPARAM lparam) {
    POINT point = {(short)LOWORD(lparam), (short)HIWORD(lparam)};
    if (is_body_requested && body_context[56] && fang_mod_editor_active() == body_owner) {
        if ((message == WM_LBUTTONDOWN || message == WM_RBUTTONDOWN) &&
            PtInRect(&validation_area, point)) {
            validation_press = message;
            SetCapture(window);
            return 0;
        }
        if (validation_press && ((message == WM_LBUTTONUP && validation_press == WM_LBUTTONDOWN) ||
            (message == WM_RBUTTONUP && validation_press == WM_RBUTTONDOWN))) {
            validation_press = 0;
            if (GetCapture() == window) ReleaseCapture();
            if (!PtInRect(&validation_area, point)) return 0;
            if (message == WM_LBUTTONUP && validation_pages)
                validation_page = (validation_page + 1) % validation_pages;
            if (message == WM_RBUTTONUP) fang_mod_editor_clear_validation();
            return 0;
        }
        if (message == WM_LBUTTONDOWN && PtInRect(&body_area, point)) {
            is_body_pressed = 1;
            SetCapture(window);
            return 0;
        }
        if (message == WM_LBUTTONUP && is_body_pressed) {
            is_body_pressed = 0;
            if (GetCapture() == window) ReleaseCapture();
            if (PtInRect(&body_area, point)) editor_body_toggle(body_context);
            return 0;
        }
    }
    if (message == WM_CAPTURECHANGED || message == WM_KILLFOCUS) {
        is_body_pressed = 0;
        validation_press = 0;
    }
    return CallWindowProcW(body_window_proc, window, message, wparam, lparam);
}

static void editor_body_detach_input(void) {
    if ((is_body_pressed || validation_press) && GetCapture() == body_window) ReleaseCapture();
    is_body_pressed = 0;
    validation_press = 0;
    if (body_window && (WNDPROC)GetWindowLongPtrW(body_window, GWLP_WNDPROC) == editor_body_window_proc) {
        SetLastError(0);
        if (!SetWindowLongPtrW(body_window, GWLP_WNDPROC, (LONG_PTR)body_window_proc) && GetLastError()) {
            trace_client_state("mod_editor_body_input_restore_failed", GetLastError());
            return;
        }
        body_window = NULL;
        body_window_proc = NULL;
    }
}

static int editor_body_attach_input(HWND window) {
    WNDPROC previous;
    if (body_window == window && body_window_proc) return 1;
    editor_body_detach_input();
    if (body_window) return 0;
    SetLastError(0);
    previous = (WNDPROC)SetWindowLongPtrW(window, GWLP_WNDPROC, (LONG_PTR)editor_body_window_proc);
    if (!previous) {
        trace_client_state("mod_editor_body_input_failed", GetLastError());
        return 0;
    }
    body_window_proc = previous;
    body_window = window;
    trace_client_state("mod_editor_body_input_ready", body_generation);
    return 1;
}

static int editor_body_place(BYTE* ui) {
    BYTE* tab = ((editor_find_window_fn)(*(void***)ui)[18])(ui, 0xf019c2e7);
    const float* area;
    float origin[2];
    LONG width;
    LONG height;
    if (!readable_range(tab, 4) || !readable_range(*(void**)tab, 196)) return 0;
    area = ((editor_area_fn)(*(void***)tab)[14])(tab);
    if (!readable_range(area, 4 * sizeof(float))) return 0;
    width = (LONG)(area[2] - area[0]);
    height = (LONG)(area[3] - area[1]);
    if (width < 16 || width > 512 || height < 16 || height > 256) return 0;
    if (!((editor_point_fn)(*(void***)tab)[48])(tab, origin, 0, 0)) return 0;
    /* RealArea is relative to its parent; the native conversion includes the
     * dock's parent origin and layout transforms. Match the Equip tab exactly. */
    body_area.left = (LONG)origin[0] - (fang_mod_editor_is_scene(fang_mod_editor_active()) ? 0 : width + 2);
    body_area.top = (LONG)origin[1];
    body_area.right = body_area.left + width;
    body_area.bottom = body_area.top + height;
    return 1;
}

static editor_value editor_number(double number) {
    editor_value result = {0};
    result.type = 3;
    result.data.number = number;
    return result;
}

static editor_value editor_string(const char* text) {
    editor_value result = {0};
    result.type = 4;
    result.data.string = text;
    return result;
}

static editor_value editor_boolean(int is_enabled) {
    editor_value result = {0};
    result.type = 2;
    *(unsigned char*)&result.data = is_enabled != 0;
    return result;
}

static int editor_get(editor_value* parent, const char* name, editor_value* result) {
    unsigned int type = parent->type & 0x8f;
    if (!parent->object_interface || type < 6 || type > 8) return 0;
    return editor_get_member(parent->object_interface, parent->data.object, name, result, type == 8);
}

static int editor_set(editor_value* parent, const char* name, editor_value property) {
    unsigned int type = parent->type & 0x8f;
    if (!parent->object_interface || type < 6 || type > 8) return 0;
    return editor_set_member(parent->object_interface, parent->data.object, name, &property, type == 8);
}

static BYTE* editor_ui_manager(void) {
    BYTE* manager = ((editor_ui_manager_fn)(editor_ui_base + 0x25920))();
    if (!readable_range(manager, 4) || !readable_range(*(void**)manager, 40)) return NULL;
    return manager;
}

static void editor_callback_name(BYTE* context, char* name, size_t capacity, int is_callback) {
    snprintf(name, capacity, "FangCreatureEditor_%u%s", *(unsigned int*)(context + 60),
        is_callback ? ".OnEditClicked" : "");
}

static void FANG_THISCALL editor_menu_destroy(BYTE* context) {
    BYTE* manager = editor_ui_manager();
    if (manager) {
        char name[80];
        editor_callback_name(context, name, sizeof(name), 1);
        ((editor_unregister_fn)(*(void***)manager)[9])(manager, name);
    }
    editor_destroy(context);
}

static int editor_copy_method(editor_value* source, editor_value* receiver, const char* name) {
    editor_value method = {0};
    int is_copied = editor_get(source, name, &method) &&
        (method.type & 0x8f) == 6 && editor_set(receiver, name, method);
    editor_release(&method);
    return is_copied;
}

static int editor_make_button(BYTE* context, editor_value* button, double width, double height,
    const char* label, void (__cdecl* on_click)(BYTE*)) {
    static const char* constants[] = {
        "kContextMenu_Collect", "kContextMenu_SwapDeck", "kContextMenu_SwapConga",
        "kContextMenu_Edit", "kContextMenu_ViewProfile"
    };
    editor_value receiver = {0};
    editor_value text = {0};
    editor_value arguments[3];
    editor_value* root = (editor_value*)(context + 24);
    editor_value* data = (editor_value*)(context + 40);
    BYTE* manager;
    unsigned int index;
    char name[80];
    int is_ready = 0;
    unsigned int stage = 1;
    arguments[0] = editor_string("fangCreatureEditorReceiver");
    arguments[1] = editor_number(18001);
    if (!editor_get(root, "fangCreatureEditorReceiver", &receiver) || (receiver.type & 0x8f) < 6) {
        editor_release(&receiver);
        memset(&receiver, 0, sizeof(receiver));
        editor_invoke(root, "createEmptyMovieClip", NULL, arguments, 2);
        if (!editor_get(root, "fangCreatureEditorReceiver", &receiver)) goto done;
    }
    stage = 2;
    if (!editor_copy_method(data, &receiver, "CallCallbackFromID") ||
        !editor_copy_method(data, &receiver, "OnContextButtonClicked")) goto done;
    stage = 3;
    editor_callback_name(context, name, sizeof(name), 0);
    if (!editor_set(&receiver, "mName", editor_string(name))) goto done;
    for (index = 0; index < sizeof(constants) / sizeof(constants[0]); ++index) {
        if (!editor_set(&receiver, constants[index], editor_number(index))) goto done;
    }
    stage = 4;
    if ((button->type & 0x8f) < 6) {
        arguments[0] = editor_string("btnContextMenu");
        arguments[1] = editor_string("fangCreatureEditor");
        arguments[2] = editor_number(18000);
        editor_invoke(root, "attachMovie", NULL, arguments, 3);
        editor_release(button);
        memset(button, 0, sizeof(*button));
        if (!editor_get(root, "fangCreatureEditor", button) || (button->type & 0x8f) < 6) goto done;
    }
    editor_set(button, "_visible", editor_boolean(0));
    editor_set(button, "__width", editor_number(width));
    editor_set(button, "__height", editor_number(height));
    editor_set(button, "_autoSize", editor_boolean(0));
    editor_set(button, "_label", editor_string(label));
    arguments[0] = editor_string("up");
    editor_invoke(button, "gotoAndStop", NULL, arguments, 1);
    editor_invoke(button, "onLoadImpl", NULL, NULL, 0);
    editor_invoke(button, "setState", NULL, arguments, 1);
    arguments[0] = editor_number(width);
    arguments[1] = editor_number(height);
    editor_invoke(button, "setSize", NULL, arguments, 2);
    editor_invoke(button, "validateNow", NULL, NULL, 0);
    editor_set(button, "disabled", editor_boolean(0));
    editor_set(button, "label", editor_string(label));
    if (editor_get(button, "textField", &text))
        editor_set(&text, "text", editor_string(label));
    editor_set(button, "btnID", editor_number(3));
    arguments[0] = editor_string("click");
    arguments[1] = receiver;
    arguments[2] = editor_string("OnContextButtonClicked");
    stage = 5;
    if (!editor_invoke(button, "addEventListener", NULL, arguments, 3)) goto done;
    stage = 6;
    manager = editor_ui_manager();
    if (!manager) goto done;
    editor_callback_name(context, name, sizeof(name), 1);
    ((editor_register_fn)(*(void***)manager)[7])(manager, context, name, on_click);
    is_ready = editor_set(button, "fangEditorReady", editor_boolean(1));
    if (is_ready) trace_client_state("mod_editor_button_created", *(unsigned int*)(context + 60));
done:
    if (!is_ready && editor_ui_failure_count++ < 16)
        trace_client_state("mod_editor_button_failed_stage", stage);
    editor_release(&text);
    editor_release(&receiver);
    return is_ready;
}

static void editor_menu_update(BYTE* context) {
    static const char* names[] = {"contextMenu1", "contextMenu2", "contextMenu3"};
    editor_value button = {0};
    editor_value ready = {0};
    editor_value alpha = editor_number(100);
    editor_value* root = (editor_value*)(context + 24);
    double x = 0, last_y = 0, previous_y = 0, width = 180, height = 24;
    unsigned int row_count = 0;
    unsigned int index;
    int is_edit_visible = 0;
    int is_eligible;
    if (!context[56]) return;
    editor_get(root, "fangCreatureEditor", &button);
    is_eligible = fang_mod_editor_target(context);
    for (index = 0; is_eligible && index < sizeof(names) / sizeof(names[0]); ++index) {
        editor_value native = {0}, id = {0}, visible = {0};
        editor_value position = {0}, size = {0};
        if (editor_get(root, names[index], &native) &&
            editor_get(&native, "_visible", &visible) &&
            (visible.type & 0x8f) == 2 && *(unsigned char*)&visible.data) {
            if (editor_get(&native, "_y", &position) && (position.type & 0x8f) == 3) {
                if (!row_count || position.data.number > last_y) {
                    previous_y = last_y;
                    last_y = position.data.number;
                } else if (row_count == 1 || position.data.number > previous_y) {
                    previous_y = position.data.number;
                }
                ++row_count;
            }
            editor_get(&native, "_height", &size);
            if (editor_get(&native, "btnID", &id) && (id.type & 0x8f) == 3 && id.data.number == 3) {
                is_edit_visible = 1;
                editor_release(&position);
                memset(&position, 0, sizeof(position));
                if (editor_get(&native, "_x", &position) && (position.type & 0x8f) == 3)
                    x = position.data.number;
                if ((size.type & 0x8f) == 3 && size.data.number > 0) height = size.data.number;
                editor_release(&size);
                memset(&size, 0, sizeof(size));
                if (editor_get(&native, "_width", &size) && (size.type & 0x8f) == 3 &&
                    size.data.number > width) width = size.data.number;
                editor_release(&alpha);
                memset(&alpha, 0, sizeof(alpha));
                editor_get(&native, "_alpha", &alpha);
            }
        }
        editor_release(&size);
        editor_release(&position);
        editor_release(&visible);
        editor_release(&id);
        editor_release(&native);
    }
    if (!is_edit_visible) {
        editor_set(&button, "_visible", editor_boolean(0));
        goto done;
    }
    if (fang_mod_scene_available()) {
        editor_value visible = {0}, opacity = {0};
        if (editor_get(root, "_visible", &visible) &&
            (visible.type & 0x8f) == 2 && *(unsigned char*)&visible.data &&
            editor_get(root, "_alpha", &opacity) &&
            (opacity.type & 0x8f) == 3 && opacity.data.number > 0 &&
            fang_mod_editor_identity(context, scene_identity))
            scene_target = *(unsigned int*)(context + 8);
        editor_release(&opacity);
        editor_release(&visible);
    }
    if (!editor_get(&button, "fangEditorReady", &ready) ||
        (ready.type & 0x8f) != 2 || !*(unsigned char*)&ready.data) {
        if (!editor_make_button(context, &button, width, height, "CREATURE EDIT", fang_mod_editor_open)) goto done;
    }
    editor_set(&button, "_x", editor_number(x));
    /* Flash artwork bounds include more than the visible row. Continue the
     * native row pitch instead of adding a gap after those bounds. */
    editor_set(&button, "_y", editor_number(last_y +
        (row_count > 1 && last_y > previous_y ? last_y - previous_y : height)));
    if ((alpha.type & 0x8f) == 3) editor_set(&button, "_alpha", alpha);
    editor_set(&button, "_visible", editor_boolean(1));
done:
    editor_release(&alpha);
    editor_release(&ready);
    editor_release(&button);
}

static void __cdecl editor_body_toggle(BYTE* context) {
    BYTE* editor = fang_mod_editor_active();
    BYTE* ui;
    unsigned int mode;
    trace_client_state("mod_editor_body_clicked", body_generation);
    if (context != body_context || !editor || editor != body_owner) return;
    ui = *(BYTE**)(editor + 128);
    if (!readable_range(ui, 159) || !ui[157] || ui[158]) return;
    mode = 0;
    /* EditorUI::SetMode owns both the palette visibility and the editor's
     * native before/after transitions; never set editor+856 directly. */
    editor_native_mode(ui, mode);
    fang_mod_editor_update_body();
    editor_body_select_tabs(editor, ui);
    trace_client_state("mod_editor_body_mode", *(unsigned int*)(editor + 856));
    trace_client_state("mod_editor_body_ui_mode", *(unsigned int*)(ui + 92));
}

static void __cdecl editor_body_loaded(void* owner, unsigned int unused, editor_value* movies) {
    editor_copy_fn copy = (editor_copy_fn)(editor_ui_base + 0x17f0);
    (void)unused;
    trace_client_state("mod_editor_body_tab_callback", (uintptr_t)owner);
    /* The manager routes this callback through our unique registration name.
     * Use the load generation as userdata: a movie's display _name is not a
     * native request identity, and a late callback must not own a newer load. */
    if ((uintptr_t)owner != body_generation || !is_body_requested ||
        body_owner != fang_mod_editor_active() || !readable_range(movies, 32)) return;
    if ((movies[0].type & 0x8f) < 6 || (movies[0].type & 0x8f) > 8 ||
        (movies[1].type & 0x8f) < 6 || (movies[1].type & 0x8f) > 8) {
        trace_client_state("mod_editor_body_tab_invalid", movies[0].type | (movies[1].type << 16));
        return;
    }
    copy((editor_value*)(body_context + 24), &movies[0]);
    copy((editor_value*)(body_context + 40), &movies[1]);
    body_context[56] = 1;
    trace_client_state("mod_editor_body_tab_loaded", body_generation);
}

void fang_mod_editor_close_body_ui(void) {
    BYTE* manager;
    char callback[80];
    fang_mod_editor_clear_validation();
    SetRectEmpty(&validation_area);
    validation_page = 0;
    validation_pages = 0;
    if (!is_body_requested) return;
    editor_body_detach_input();
    is_body_requested = 0;
    body_context[56] = 0;
    manager = editor_ui_manager();
    if (manager) {
        editor_callback_name(body_context, callback, sizeof(callback), 1);
        ((editor_unregister_fn)(*(void***)manager)[9])(manager, callback);
        ((editor_unregister_fn)(*(void***)manager)[6])(manager, body_name);
    }
    editor_release((editor_value*)(body_context + 24));
    editor_release((editor_value*)(body_context + 40));
    memset(body_context, 0, sizeof(body_context));
    body_owner = NULL;
    body_width = 0;
    body_height = 0;
}

static void editor_draw_wrench(editor_value* button) {
    static const double points[][2] = {
        {1, 0}, {7, 5}, {12, 0}, {14, 7}, {10, 12}, {5, 13},
        {-7, 26}, {-12, 26}, {-15, 22}, {-13, 18}, {-2, 7}, {-2, 3}
    };
    editor_value icon = {0};
    editor_value arguments[2];
    unsigned int index;
    if (editor_get(button, "fangWrench", &icon) && (icon.type & 0x8f) >= 6) goto done;
    editor_release(&icon);
    memset(&icon, 0, sizeof(icon));
    arguments[0] = editor_string("fangWrench");
    arguments[1] = editor_number(18002);
    editor_invoke(button, "createEmptyMovieClip", NULL, arguments, 2);
    if (!editor_get(button, "fangWrench", &icon)) goto done;
    editor_set(&icon, "_x", editor_number(36));
    editor_set(&icon, "_y", editor_number(7));
    arguments[0] = editor_number(0xffffff);
    arguments[1] = editor_number(100);
    editor_invoke(&icon, "beginFill", NULL, arguments, 2);
    for (index = 0; index < sizeof(points) / sizeof(points[0]); ++index) {
        arguments[0] = editor_number(points[index][0]);
        arguments[1] = editor_number(points[index][1]);
        editor_invoke(&icon, index ? "lineTo" : "moveTo", NULL, arguments, 2);
    }
    editor_invoke(&icon, "endFill", NULL, NULL, 0);
done:
    editor_release(&icon);
}

static int editor_body_draw(editor_value* root, editor_value* button,
    double width, double height, int is_selected) {
    editor_value arguments[3];
    editor_value icon = {0};
    double points[7][2] = {
        {1, 9}, {9, 1}, {width - 9, 1}, {width - 1, 9},
        {width - 1, height - 1}, {1, height - 1}, {1, 9}
    };
    unsigned int index;
    if (!editor_get(root, "fangBodyTab", button) || (button->type & 0x8f) < 6) {
        editor_release(button);
        memset(button, 0, sizeof(*button));
        arguments[0] = editor_string("fangBodyTab");
        arguments[1] = editor_number(18000);
        editor_invoke(root, "createEmptyMovieClip", NULL, arguments, 2);
        if (!editor_get(root, "fangBodyTab", button) || (button->type & 0x8f) < 6) return 0;
    }
    if (body_width == width && body_height == height && is_body_selected == is_selected) return 1;
    /* Draw in the tab's final coordinate space. Scaling the menu skin's
     * aggregate bounds also scales its authored offset and newly added icon;
     * repeating that each frame made the background collapse into a strip. */
    editor_invoke(button, "clear", NULL, NULL, 0);
    arguments[0] = editor_number(1);
    arguments[1] = editor_number(is_selected ? 0xd9f4ff : 0x82939c);
    arguments[2] = editor_number(100);
    editor_invoke(button, "lineStyle", NULL, arguments, 3);
    arguments[0] = editor_number(is_selected ? 0x365767 : 0x202b32);
    arguments[1] = editor_number(100);
    editor_invoke(button, "beginFill", NULL, arguments, 2);
    for (index = 0; index < sizeof(points) / sizeof(points[0]); ++index) {
        arguments[0] = editor_number(points[index][0]);
        arguments[1] = editor_number(points[index][1]);
        editor_invoke(button, index ? "lineTo" : "moveTo", NULL, arguments, 2);
    }
    editor_invoke(button, "endFill", NULL, NULL, 0);
    editor_draw_wrench(button);
    if (editor_get(button, "fangWrench", &icon)) {
        /* The icon's local bounds are [-15, 14] by [0, 26]. */
        editor_set(&icon, "_xscale", editor_number(75));
        editor_set(&icon, "_yscale", editor_number(75));
        editor_set(&icon, "_x", editor_number(width / 2 + 0.375));
        editor_set(&icon, "_y", editor_number((height - 19.5) / 2));
    }
    editor_release(&icon);
    body_width = width;
    body_height = height;
    is_body_selected = is_selected;
    trace_client_state("mod_editor_body_layout", 2);
    return 1;
}

static void editor_validation_draw(editor_value* root) {
    editor_value field = {0};
    editor_value arguments[6];
    const char* text = fang_mod_editor_validation_text(validation_page, &validation_pages);
    RECT client;
    LONG width;
    LONG x;
    LONG y;
    SetRectEmpty(&validation_area);
    if (!text) {
        validation_page = 0;
        if (editor_get(root, "fangValidation", &field))
            editor_set(&field, "_visible", editor_boolean(0));
        goto done;
    }
    if (!body_window || !GetClientRect(body_window, &client)) goto done;
    width = client.right - client.left - 24;
    if (width > 520) width = 520;
    if (width < 160) goto done;
    x = body_area.left;
    if (x + width > client.right - 12) x = client.right - width - 12;
    if (x < 12) x = 12;
    y = body_area.top - 240;
    if (y < 12) y = 12;
    if (!editor_get(root, "fangValidation", &field) || (field.type & 0x8f) < 6) {
        editor_release(&field);
        memset(&field, 0, sizeof(field));
        arguments[0] = editor_string("fangValidation");
        arguments[1] = editor_number(101);
        arguments[2] = editor_number(0);
        arguments[3] = editor_number(0);
        arguments[4] = editor_number(width);
        arguments[5] = editor_number(228);
        if (!editor_invoke(root, "createTextField", NULL, arguments, 6) ||
            !editor_get(root, "fangValidation", &field) || (field.type & 0x8f) < 6) goto done;
        editor_set(&field, "multiline", editor_boolean(1));
        editor_set(&field, "wordWrap", editor_boolean(1));
        editor_set(&field, "selectable", editor_boolean(0));
        editor_set(&field, "background", editor_boolean(1));
        editor_set(&field, "backgroundColor", editor_number(0x101820));
        editor_set(&field, "border", editor_boolean(1));
        editor_set(&field, "borderColor", editor_number(0xe2b34a));
        editor_set(&field, "textColor", editor_number(0xffffff));
    }
    editor_set(&field, "text", editor_string(text));
    editor_set(&field, "_width", editor_number(width));
    editor_set(&field, "_x", editor_number(x - body_area.left));
    editor_set(&field, "_y", editor_number(y - body_area.top));
    editor_set(&field, "_visible", editor_boolean(1));
    SetRect(&validation_area, x, y, x + width, y + 228);
done:
    editor_release(&field);
}

static void editor_scene_hide_window(BYTE* window) {
    if (!readable_range(window, 44) || !readable_range(*(void**)window, 128)) return;
    /* Native IWindow::SetFlag(Visible, false), verified at BE05B0. These are
     * windows owned by this editor UI, not shared authored layout defaults.
     * Preserve paint/preview panels and the native Accept/Cancel lifecycle. */
    ((editor_window_flag_fn)(*(void***)window)[31])(window, 1, 0);
}

static void editor_scene_present(BYTE* editor, BYTE* ui) {
    BYTE* equipment_tab;
    if (!fang_mod_editor_is_scene(editor)) return;
    if (!readable_range(ui, 312)) return;
    editor_scene_hide_window(*(BYTE**)(ui + 300)); /* Equipment layout root. */
    editor_scene_hide_window(*(BYTE**)(ui + 308)); /* Hero combat-stat panel. */
    equipment_tab = ((editor_find_window_fn)(*(void***)ui)[18])(ui, 0xf019c2e7);
    editor_scene_hide_window(equipment_tab);
}

static void editor_body_update(void) {
    static const char* native_buttons[] = {"contextMenu1", "contextMenu2", "contextMenu3"};
    BYTE* editor = fang_mod_editor_active();
    BYTE* ui;
    BYTE* manager;
    editor_value* root = (editor_value*)(body_context + 24);
    editor_value button = {0};
    HWND window;
    double width;
    double height;
    unsigned int index;
    fang_mod_editor_update_body();
    if (!editor || editor != body_owner) fang_mod_editor_close_body_ui();
    if (!editor) return;
    ui = *(BYTE**)(editor + 128);
    if (!readable_range(ui, 159) || !ui[157] || !*(void**)(editor + 176)) return;
    if (!is_body_requested) {
        manager = editor_ui_manager();
        if (!manager) return;
        /* Give the new Body tab its own explicit selection. The player editor
         * starts with mode 0 even for equipment; use its retained Equip mode
         * until the player selects our general Build-mode entry. */
        if (fang_mod_editor_is_scene(editor)) editor_native_mode(ui, 0);
        else ((editor_mode_fn)(*(void***)ui)[17])(ui, 3);
        trace_client_state("mod_editor_initial_mode", *(unsigned int*)(editor + 856));
        body_owner = editor;
        body_mode = UINT32_MAX;
        *(unsigned int*)(body_context + 60) = UINT32_MAX;
        snprintf(body_name, sizeof(body_name), "FangModBodyTab_%u", ++body_generation);
        is_body_requested = 1;
        ((editor_load_fn)(*(void***)manager)[5])(manager, "ContextMenu.swf", body_name,
            1000.0, editor_body_loaded, (void*)(uintptr_t)body_generation);
        trace_client_state("mod_editor_body_tab_requested", body_generation);
    }
    if (!body_context[56]) return;
    editor_scene_present(editor, ui);
    editor_body_select_tabs(editor, ui);
    if (!editor_body_place(ui)) {
        SetRectEmpty(&body_area);
        editor_set(root, "_visible", editor_boolean(0));
        return;
    }
    width = body_area.right - body_area.left;
    height = body_area.bottom - body_area.top;
    for (index = 0; index < sizeof(native_buttons) / sizeof(native_buttons[0]); ++index) {
        editor_value native = {0};
        if (editor_get(root, native_buttons[index], &native))
            editor_set(&native, "_visible", editor_boolean(0));
        editor_release(&native);
    }
    if (!editor_body_draw(root, &button, width, height, *(unsigned int*)(editor + 856) == 0)) goto done;
    window = GetActiveWindow();
    if (!window) goto done;
    if (!editor_body_attach_input(window)) goto done;
    editor_set(root, "_x", editor_number(body_area.left));
    editor_set(root, "_y", editor_number(body_area.top));
    editor_set(&button, "_x", editor_number(0));
    editor_set(&button, "_y", editor_number(0));
    editor_set(root, "_visible", editor_boolean(1));
    editor_set(root, "_alpha", editor_number(100));
    editor_set(&button, "_visible", editor_boolean(1));
    editor_set(&button, "selected", editor_boolean(*(unsigned int*)(editor + 856) == 0));
    editor_set(&button, "_alpha", editor_number(*(unsigned int*)(editor + 856) == 0 ? 100 : 65));
    editor_set(&button, "disabled", editor_boolean(ui[158]));
    editor_validation_draw(root);
done:
    editor_release(&button);
}

static void editor_scene_close(void) {
    BYTE* manager;
    char callback[80];
    scene_target = 0;
    memset(scene_identity, 0, sizeof(scene_identity));
    if (!is_scene_requested) return;
    is_scene_requested = 0;
    ++scene_generation;
    manager = editor_ui_manager();
    if (manager) {
        editor_callback_name(scene_context, callback, sizeof(callback), 1);
        ((editor_unregister_fn)(*(void***)manager)[9])(manager, callback);
        ((editor_unregister_fn)(*(void***)manager)[6])(manager, scene_name);
    }
    editor_release((editor_value*)(scene_context + 24));
    editor_release((editor_value*)(scene_context + 40));
    memset(scene_context, 0, sizeof(scene_context));
}

static void __cdecl editor_scene_loaded(void* owner, unsigned int unused, editor_value* movies) {
    editor_copy_fn copy = (editor_copy_fn)(editor_ui_base + 0x17f0);
    (void)unused;
    if (!is_scene_requested || (unsigned int)(uintptr_t)owner != scene_generation ||
        !readable_range(movies, 32) || (movies[0].type & 0x8f) < 6 ||
        (movies[0].type & 0x8f) > 8 || (movies[1].type & 0x8f) < 6 ||
        (movies[1].type & 0x8f) > 8) return;
    copy((editor_value*)(scene_context + 24), &movies[0]);
    copy((editor_value*)(scene_context + 40), &movies[1]);
    scene_context[56] = 1;
    trace_client_state("mod_editor_scene_button_loaded", scene_generation);
}

static int editor_scene_has_target(void) {
    uint32_t identity[3];
    return scene_target && fang_mod_editor_identity(scene_context, identity) &&
        memcmp(identity, scene_identity, sizeof(identity)) == 0;
}

static void __cdecl editor_scene_open(BYTE* context) {
    if (context != scene_context || !editor_scene_has_target()) return;
    fang_mod_editor_open_scene(context);
}

static void editor_scene_update(int is_arsenal) {
    static const char* names[] = {"contextMenu1", "contextMenu2", "contextMenu3"};
    editor_value* root = (editor_value*)(scene_context + 24);
    editor_value button = {0}, ready = {0};
    BYTE* manager;
    HWND window;
    RECT client;
    unsigned int index;
    if (!is_arsenal || fang_mod_editor_active() || !fang_mod_scene_available()) {
        editor_scene_close();
        return;
    }
    if (!is_scene_requested) {
        manager = editor_ui_manager();
        if (!manager) return;
        *(unsigned int*)(scene_context + 60) = UINT32_MAX - 1;
        snprintf(scene_name, sizeof(scene_name), "FangModEditorScene_%u", ++scene_generation);
        is_scene_requested = 1;
        ((editor_load_fn)(*(void***)manager)[5])(manager, "ContextMenu.swf", scene_name,
            999.0, editor_scene_loaded, (void*)(uintptr_t)scene_generation);
    }
    if (!scene_context[56]) return;
    *(unsigned int*)(scene_context + 8) = scene_target;
    if (!editor_scene_has_target()) {
        scene_target = 0;
        *(unsigned int*)(scene_context + 8) = 0;
    }
    for (index = 0; index < sizeof(names) / sizeof(names[0]); ++index) {
        editor_value native = {0};
        if (editor_get(root, names[index], &native))
            editor_set(&native, "_visible", editor_boolean(0));
        editor_release(&native);
    }
    editor_get(root, "fangCreatureEditor", &button);
    if (!editor_get(&button, "fangEditorReady", &ready) ||
        (ready.type & 0x8f) != 2 || !*(unsigned char*)&ready.data) {
        if (!editor_make_button(scene_context, &button, 180, 24,
            "CREATURE EDITOR", editor_scene_open)) goto done;
    }
    window = GetActiveWindow();
    if (!window || !GetClientRect(window, &client)) goto done;
    editor_set(root, "_x", editor_number(client.right > 204 ? client.right - 204 : 12));
    editor_set(root, "_y", editor_number(135));
    editor_set(root, "_visible", editor_boolean(1));
    editor_set(&button, "_x", editor_number(0));
    editor_set(&button, "_y", editor_number(0));
    editor_set(&button, "_visible", editor_boolean(1));
    editor_set(&button, "disabled", editor_boolean(!scene_target));
    editor_set(&button, "_alpha", editor_number(scene_target ? 100 : 45));
done:
    editor_release(&ready);
    editor_release(&button);
}

void fang_mod_update_editor(void) {
    static const unsigned int vectors[] = {1840, 2440};
    BYTE* controller;
    unsigned int index;
    if (!is_editor_ui_enabled) return;
    /* Retain the last eligible creature when its hover menu closes so the
     * pointer can travel to the separate Arsenal action. Revalidate the owned
     * identity each frame and on click; clear it on leaving Arsenal. */
    fang_mod_update_scene();
    editor_body_update();
    controller = *(BYTE**)(editor_ui_base + 0xdbeaa0);
    if (!readable_range(controller, 0xd10) || !controller[920] ||
        *(void**)controller != editor_ui_base + 0xbce048) {
        editor_scene_update(0);
        return;
    }
    for (index = 0; index < sizeof(vectors) / sizeof(vectors[0]); ++index) {
        BYTE* begin = *(BYTE**)(controller + vectors[index]);
        BYTE* end = *(BYTE**)(controller + vectors[index] + 4);
        uintptr_t size = (uintptr_t)end - (uintptr_t)begin;
        if ((uintptr_t)end < (uintptr_t)begin || size > 128 * 72 || size % 72 ||
            (size && !readable_range(begin, size))) continue;
        while (begin != end) {
            editor_menu_update(begin);
            begin += 72;
        }
    }
    editor_scene_update(1);
}

int fang_mod_install_editor_ui(BYTE* base) {
    static const unsigned int calls[] = {0x18e27, 0x18ee1};
    unsigned int index;
    editor_get_member = (editor_member_fn)(base + 0x14dae0);
    editor_set_member = (editor_member_fn)(base + 0x14b510);
    editor_invoke = (editor_invoke_fn)(base + 0x15a0);
    editor_release = (editor_release_fn)(base + 0x17c0);
    editor_destroy = (editor_destroy_fn)(base + 0x178c0);
    editor_native_mode = (editor_mode_fn)(base + 0xd0d20);
    editor_ui_base = base;
    for (index = 0; index < sizeof(calls) / sizeof(calls[0]); ++index) {
        if (!patch_call(base + calls[index], editor_destroy, editor_menu_destroy)) break;
    }
    if (index != sizeof(calls) / sizeof(calls[0])) {
        while (index) {
            --index;
            if (!patch_call(base + calls[index], editor_menu_destroy, editor_destroy))
                trace_client_state("mod_editor_ui_rollback_failed", index + 1);
        }
        return 0;
    }
    if (!patch_pointer((void**)(base + 0xbda8fc), editor_native_mode, editor_body_native_mode)) {
        while (index) {
            --index;
            if (!patch_call(base + calls[index], editor_menu_destroy, editor_destroy))
                trace_client_state("mod_editor_ui_rollback_failed", index + 1);
        }
        trace_client_state("mod_editor_mode_hook_failed", 1);
        return 0;
    }
    is_editor_ui_enabled = 1;
    return 1;
}

#include "hook.h"
#if FANG_SCENARIO
#include "control.h"
#endif

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
static RECT preview_area;
static RECT reset_area;
static BYTE reset_context[72];
static int is_reset_pressed;
static DWORD reset_confirm_until;
static DWORD reset_failed_until;
static int is_preview_pressed;
static unsigned int preview_status = UINT32_MAX;
static int is_body_pressed;
static unsigned int body_mode = UINT32_MAX;
static int is_body_selected;
static editor_mode_fn editor_native_mode;
static BYTE scene_context[72];
static unsigned int scene_generation;
static unsigned int scene_target;
static uint32_t scene_identity[3];
static int is_scene_requested;
static char scene_name[64];

static void __cdecl editor_body_toggle(BYTE* context);
static void __cdecl editor_preview_toggle(BYTE* context);
static void __cdecl editor_reset_toggle(BYTE* context);

#if FANG_SCENARIO
int fang_mod_editor_reset_bounds(RECT* area) {
    if (!area || !fang_mod_editor_is_body(fang_mod_editor_active()) || IsRectEmpty(&reset_area)) return 0;
    *area = reset_area;
    return 1;
}
#endif

int fang_mod_editor_is_body(BYTE* editor) {
    return fang_mod_editor_is_body_selected(editor) && *(unsigned int*)(editor + 856) == 0;
}

int fang_mod_editor_is_body_selected(BYTE* editor) {
    return is_body_selected && editor && editor == body_owner && editor == fang_mod_editor_active();
}

/* EditorUI::SetMode changes palettes and editor state, while the ordinary
 * radio buttons update their own selection through their click dispatcher.
 * Our separate control must synchronize those visuals through IButton, using
 * the same GetObject/SetButtonFlag/Invalidate calls as native EditorUI. */
static void editor_body_select_tabs(BYTE* editor, BYTE* ui) {
    static const uint32_t ids[] = {0xf019c2e7, 0xf019c2f3, 0x70218642, 0x0aa07258};
    unsigned int mode = *(unsigned int*)(editor + 856);
    unsigned int index;
    unsigned int selection = mode | (fang_mod_editor_is_body(editor) ? 0x80000000u : 0);
    if (body_mode == selection) return;
    for (index = 0; index < sizeof(ids) / sizeof(ids[0]); ++index) {
        BYTE* tab = ((editor_find_window_fn)(*(void***)ui)[18])(ui, ids[index]);
        BYTE* button;
        int is_selected = (index == 0 && mode == 0 && !fang_mod_editor_is_body(editor)) ||
            (index == 1 && mode == 1) || (index == 2 && mode == 2) ||
            (index == 3 && mode == 3);
        if (!readable_range(tab, 4) || !readable_range(*(void**)tab, 152)) continue;
        button = ((editor_button_fn)(*(void***)tab)[3])(tab, 0x8ed27e7a);
        if (!readable_range(button, 4) || !readable_range(*(void**)button, 44)) continue;
        ((editor_button_flag_fn)(*(void***)button)[10])(button, 4, is_selected);
        ((editor_repaint_fn)(*(void***)tab)[37])(tab);
    }
    body_mode = selection;
    trace_client_state("mod_editor_tab_selection", mode);
}

static void FANG_THISCALL editor_body_native_mode(BYTE* ui, unsigned int mode) {
    BYTE* editor = fang_mod_editor_active();
    /* Equip and Body both use the native Build mode. Keep the native mode
     * for equipment picking and placement; only our explicit Body action
     * selects the private catalogue and general creature manipulators. */
    if (editor && *(BYTE**)(editor + 128) == ui) {
        is_body_selected = 0;
        fang_mod_palette_suspend(editor);
    }
    editor_native_mode(ui, mode);
    if (editor && *(BYTE**)(editor + 128) == ui) fang_mod_editor_update_body();
}

/* The native SPUI editor owns mouse dispatch. A movie drawn over that editor
 * is not an Arsenal Flash input target. Route only our separate tab's click
 * through its owning game window, on the native UI thread. */
static LRESULT CALLBACK editor_body_window_proc(HWND window, UINT message, WPARAM wparam, LPARAM lparam) {
    POINT point = {(short)LOWORD(lparam), (short)HIWORD(lparam)};
    if (fang_mod_editor_preview_input(window, message, wparam, lparam)) return 0;
    if (message == WM_MOUSEWHEEL && fang_mod_editor_validation_scroll(window, wparam, lparam)) return 0;
    if (is_body_requested && body_context[56] && fang_mod_editor_active() == body_owner) {
        if (message == WM_LBUTTONDOWN && PtInRect(&reset_area, point)) {
            is_reset_pressed = 1;
            SetCapture(window);
            return 0;
        }
        if (message == WM_LBUTTONUP && is_reset_pressed) {
            is_reset_pressed = 0;
            if (GetCapture() == window) ReleaseCapture();
            if (PtInRect(&reset_area, point)) editor_reset_toggle(reset_context);
            return 0;
        }
        if (message == WM_LBUTTONDOWN && PtInRect(&preview_area, point)) {
            is_preview_pressed = 1;
            SetCapture(window);
            return 0;
        }
        if (message == WM_LBUTTONUP && is_preview_pressed) {
            is_preview_pressed = 0;
            if (GetCapture() == window) ReleaseCapture();
            if (PtInRect(&preview_area, point)) editor_preview_toggle(body_context);
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
        is_reset_pressed = 0;
    }
    return CallWindowProcW(body_window_proc, window, message, wparam, lparam);
}

static void editor_body_detach_input(void) {
    if (is_body_pressed && GetCapture() == body_window) ReleaseCapture();
    is_body_pressed = 0;
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
    body_area.left = (LONG)origin[0] - width - 2;
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
    int is_reset = context == reset_context;
    const char* button_name = is_reset ? "fangResetBody" : "fangCreatureEditor";
    const char* receiver_name = is_reset ? "fangResetBodyReceiver" : "fangCreatureEditorReceiver";
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
    arguments[0] = editor_string(receiver_name);
    arguments[1] = editor_number(is_reset ? 18003 : 18001);
    if (!editor_get(root, receiver_name, &receiver) || (receiver.type & 0x8f) < 6) {
        editor_release(&receiver);
        memset(&receiver, 0, sizeof(receiver));
        editor_invoke(root, "createEmptyMovieClip", NULL, arguments, 2);
        if (!editor_get(root, receiver_name, &receiver)) goto done;
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
        arguments[1] = editor_string(button_name);
        arguments[2] = editor_number(is_reset ? 18002 : 18000);
        editor_invoke(root, "attachMovie", NULL, arguments, 3);
        editor_release(button);
        memset(button, 0, sizeof(*button));
        if (!editor_get(root, button_name, button) || (button->type & 0x8f) < 6) goto done;
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

static void editor_enter_body(BYTE* editor, BYTE* ui) {
    fang_mod_palette_suspend(editor);
    /* Equip and Body share mode 0. SetMode skips its lifecycle for an
     * unchanged mode, leaving Equip's clay material in the baked display.
     * Let Paint initialize that material before entering editable Body.
     * Both transitions complete on this UI thread without changing parts. */
    if (*(unsigned int*)(editor + 856) == 0 && !fang_mod_editor_is_body(editor))
        editor_native_mode(ui, 1);
    is_body_selected = 1;
    editor_native_mode(ui, 0);
}

static void __cdecl editor_body_toggle(BYTE* context) {
    BYTE* editor = fang_mod_editor_active();
    BYTE* ui;
    trace_client_state("mod_editor_body_clicked", body_generation);
    if (context != body_context || !editor || editor != body_owner) return;
    ui = *(BYTE**)(editor + 128);
    if (!readable_range(ui, 159) || !ui[157] || ui[158]) return;
    /* EditorUI::SetMode owns both the palette visibility and the editor's
     * native before/after transitions; never set editor+856 directly. */
    editor_enter_body(editor, ui);
    fang_mod_editor_update_body();
    editor_body_select_tabs(editor, ui);
    trace_client_state("mod_editor_body_mode", *(unsigned int*)(editor + 856));
    trace_client_state("mod_editor_body_ui_mode", *(unsigned int*)(ui + 92));
}

static void __cdecl editor_reset_toggle(BYTE* context) {
    BYTE* editor = fang_mod_editor_active();
    BYTE* ui;
    DWORD now = GetTickCount();
    if (context != reset_context || editor != body_owner || !fang_mod_editor_is_body(editor) ||
        fang_mod_editor_is_save_busy(editor)) return;
    ui = *(BYTE**)(editor + 128);
    if (!readable_range(ui, 159) || !ui[157] || ui[158]) return;
    if (!reset_confirm_until || (LONG)(now - reset_confirm_until) >= 0) {
        reset_confirm_until = now + 5000;
        return;
    }
    reset_confirm_until = 0;
    if (!fang_mod_editor_reset_body(editor)) {
        reset_failed_until = now + 5000;
        return;
    }
    reset_failed_until = 0;
    /* Rebuild the painted display using the same real mode transitions. */
    editor_native_mode(ui, 1);
    editor_enter_body(editor, ui);
}

static void __cdecl editor_preview_toggle(BYTE* context) {
    BYTE* editor = fang_mod_editor_active();
    BYTE* ui;
    unsigned int mode;
    if (context != body_context || editor != body_owner || !fang_mod_editor_is_scene(editor)) return;
    ui = *(BYTE**)(editor + 128);
    if (!readable_range(ui, 159) || !ui[157] || ui[158]) return;
    mode = *(unsigned int*)(editor + 856) == 2 ? 0 : 2;
    if (mode == 2 && !fang_mod_editor_preview_ready(editor)) {
        trace_client_state("mod_editor_preview_not_ready", 1);
        return;
    }
    is_body_selected = mode == 0;
    fang_mod_palette_suspend(editor);
    editor_native_mode(ui, mode);
    fang_mod_editor_update_body();
    editor_body_select_tabs(editor, ui);
    trace_client_state("mod_editor_preview_mode", *(unsigned int*)(editor + 856));
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
    is_body_selected = 0;
    fang_mod_editor_clear_validation();
    fang_mod_editor_preview_controls_leave();
    fang_mod_editor_tab_leave();
    SetRectEmpty(&preview_area);
    SetRectEmpty(&reset_area);
    is_reset_pressed = 0;
    reset_confirm_until = 0;
    reset_failed_until = 0;
    is_preview_pressed = 0;
    preview_status = UINT32_MAX;
    if (!is_body_requested) return;
    editor_body_detach_input();
    is_body_requested = 0;
    body_context[56] = 0;
    manager = editor_ui_manager();
    if (manager) {
        if (reset_context[56]) {
            editor_callback_name(reset_context, callback, sizeof(callback), 1);
            ((editor_unregister_fn)(*(void***)manager)[9])(manager, callback);
        }
        editor_callback_name(body_context, callback, sizeof(callback), 1);
        ((editor_unregister_fn)(*(void***)manager)[9])(manager, callback);
        ((editor_unregister_fn)(*(void***)manager)[6])(manager, body_name);
    }
    editor_release((editor_value*)(body_context + 24));
    editor_release((editor_value*)(body_context + 40));
    editor_release((editor_value*)(reset_context + 24));
    editor_release((editor_value*)(reset_context + 40));
    memset(reset_context, 0, sizeof(reset_context));
    memset(body_context, 0, sizeof(body_context));
    body_owner = NULL;
}

static void editor_scene_hide_window(BYTE* window) {
    if (!readable_range(window, 44) || !readable_range(*(void**)window, 128)) return;
    /* Native IWindow::SetFlag(Visible, false), verified at BE05B0. These are
     * windows owned by this editor UI, not shared authored layout defaults.
     * Preserve paint/preview panels and the native Accept/Cancel lifecycle. */
    ((editor_window_flag_fn)(*(void***)window)[31])(window, 1, 0);
}

static void editor_scene_present(BYTE* editor, BYTE* ui) {
    if (!fang_mod_editor_is_scene(editor)) return;
    if (!readable_range(ui, 312)) return;
    /* The imported environment shares the player editor's native tabs.
     * SetMode owns equipment/detail visibility; hiding their layout here
     * every frame overrides that transition and leaves empty panels. */
    editor_scene_hide_window(*(BYTE**)(ui + 308)); /* Hero combat-stat panel. */
}

static void editor_preview_label(editor_value* button, const char* label, LONG width, LONG height) {
    editor_value text = {0};
    editor_value format = {0};
    editor_value argument;
    editor_set(button, "_label", editor_string(label));
    editor_set(button, "label", editor_string(label));
    if (!editor_get(button, "textField", &text)) goto done;
    /* btnContextMenu resizes its frame without widening the authored label
     * field. Give this separate preview control an explicit single-line box
     * and retain its installed font while increasing the text size. */
    editor_set(&text, "autoSize", editor_string("none"));
    editor_set(&text, "wordWrap", editor_boolean(0));
    editor_set(&text, "multiline", editor_boolean(0));
    editor_set(&text, "_xscale", editor_number(100));
    editor_set(&text, "_yscale", editor_number(100));
    editor_set(&text, "_x", editor_number(8));
    editor_set(&text, "_y", editor_number(2));
    editor_set(&text, "_width", editor_number(width - 16));
    editor_set(&text, "_height", editor_number(height - 4));
    editor_set(&text, "text", editor_string(label));
    if (!editor_invoke(&text, "getTextFormat", &format, NULL, 0)) goto done;
    editor_set(&format, "size", editor_number(18));
    editor_set(&format, "align", editor_string("center"));
    argument = format;
    editor_invoke(&text, "setNewTextFormat", NULL, &argument, 1);
    editor_invoke(&text, "setTextFormat", NULL, &argument, 1);
done:
    editor_release(&format);
    editor_release(&text);
}

static void editor_reset_draw(editor_value* root, BYTE* editor, BYTE* ui) {
    editor_value button = {0};
    editor_value ready = {0};
    editor_copy_fn copy = (editor_copy_fn)(editor_ui_base + 0x17f0);
    LONG width = 160;
    LONG height = 34;
    LONG x = body_area.left;
    LONG y = body_area.top - height - 8;
    int is_enabled = !ui[158] && !fang_mod_editor_is_save_busy(editor);
    const char* label = reset_confirm_until && (LONG)(GetTickCount() - reset_confirm_until) < 0 ?
        "RESET NOW?" : reset_failed_until && (LONG)(GetTickCount() - reset_failed_until) < 0 ?
        "RESET FAILED" : "RESET BODY";
    SetRectEmpty(&reset_area);
    editor_get(root, "fangResetBody", &button);
    if (!fang_mod_editor_is_body(editor) || x < 12 || y < 12) {
        reset_confirm_until = 0;
        editor_set(&button, "_visible", editor_boolean(0));
        goto done;
    }
    if (!reset_context[56]) {
        copy((editor_value*)(reset_context + 24), root);
        copy((editor_value*)(reset_context + 40), (editor_value*)(body_context + 40));
        *(uint32_t*)(reset_context + 60) = UINT32_MAX - 2;
        reset_context[56] = 1;
    }
    if (!editor_get(&button, "fangEditorReady", &ready) ||
        (ready.type & 0x8f) != 2 || !*(unsigned char*)&ready.data) {
        if (!editor_make_button(reset_context, &button, width, height, label, editor_reset_toggle)) goto done;
    }
    editor_set(&button, "_x", editor_number(x - body_area.left));
    editor_set(&button, "_y", editor_number(y - body_area.top));
    editor_preview_label(&button, label, width, height);
    editor_set(&button, "disabled", editor_boolean(!is_enabled));
    editor_set(&button, "_alpha", editor_number(is_enabled ? 100 : 55));
    editor_set(&button, "_visible", editor_boolean(1));
    if (is_enabled) SetRect(&reset_area, x, y, x + width, y + height);
done:
    editor_release(&ready);
    editor_release(&button);
}

static void editor_preview_draw(editor_value* root, BYTE* editor, BYTE* ui) {
    editor_value button = {0};
    editor_value ready = {0};
    RECT client;
    LONG width = 224;
    LONG height = 34;
    LONG x;
    LONG y = body_area.top - height - 8;
    int is_test = *(unsigned int*)(editor + 856) == 2;
    int is_ready = is_test || fang_mod_editor_preview_ready(editor);
    BYTE* skin = *(BYTE**)(editor + 408);
    unsigned int status;
    const char* label = is_test ? "RETURN TO BODY" : is_ready ? "TEST CREATURE" : "PREPARING PREVIEW";
    SetRectEmpty(&preview_area);
    if (!fang_mod_editor_is_scene(editor) || !body_window ||
        !GetClientRect(body_window, &client)) goto done;
    status = (is_ready ? 1u : 0u) | (editor[941] ? 2u : 0u) |
        (editor[813] ? 4u : 0u) | (*(void**)(editor + 908) ? 8u : 0u) |
        (skin ? 16u : 0u) | (readable_range(skin, 32) && *(void**)(skin + 28) ? 32u : 0u) |
        (fang_mod_editor_is_save_busy(editor) ? 64u : 0u) |
        ((*(unsigned int*)(editor + 948) & 255u) << 8);
    if (status != preview_status) {
        preview_status = status;
        trace_client_state("mod_editor_preview_status", status);
    }
    /* Above the right edge of the mode dock, clear of native undo/redo and
     * the rotation control below. Coordinates follow the real native tab. */
    x = body_area.right + 2 * (body_area.right - body_area.left) + 12;
    if (x + width > client.right - 12) x = client.right - width - 12;
    if (x < 12 || y < 12) goto done;
    editor_get(root, "fangCreatureEditor", &button);
    if (!editor_get(&button, "fangEditorReady", &ready) ||
        (ready.type & 0x8f) != 2 || !*(unsigned char*)&ready.data) {
        if (!editor_make_button(body_context, &button, width, height, label, editor_preview_toggle)) goto done;
    }
    editor_set(&button, "_x", editor_number(x - body_area.left));
    editor_set(&button, "_y", editor_number(y - body_area.top));
    editor_preview_label(&button, label, width, height);
    editor_set(&button, "disabled", editor_boolean(!is_ready || ui[158]));
    editor_set(&button, "_alpha", editor_number(is_ready && !ui[158] ? 100 : 55));
    editor_set(&button, "_visible", editor_boolean(1));
    SetRect(&preview_area, x, y, x + width, y + height);
done:
    editor_release(&ready);
    editor_release(&button);
}

static void editor_body_update(void) {
    static const char* native_buttons[] = {"contextMenu1", "contextMenu2", "contextMenu3"};
    BYTE* editor = fang_mod_editor_active();
    BYTE* ui;
    BYTE* manager;
    editor_value* root = (editor_value*)(body_context + 24);
    HWND window;
    unsigned int index;
    fang_mod_editor_update_body();
    fang_mod_editor_update_skin();
    if (!editor || editor != body_owner) fang_mod_editor_close_body_ui();
    if (!editor) return;
    ui = *(BYTE**)(editor + 128);
    if (!readable_range(ui, 159) || !ui[157] || !*(void**)(editor + 176)) return;
    fang_mod_editor_update_validation(ui);
    fang_mod_editor_preview_update();
    if (!is_body_requested) {
        manager = editor_ui_manager();
        if (!manager) return;
        /* Imported scenery starts in Body. Ordinary Creature Edit retains
         * native Equip; both views share mode 0 but own different catalogues. */
        body_owner = editor;
        if (fang_mod_editor_is_scene(editor)) editor_enter_body(editor, ui);
        else editor_native_mode(ui, 0);
        trace_client_state("mod_editor_initial_mode", *(unsigned int*)(editor + 856));
        body_mode = UINT32_MAX;
        *(unsigned int*)(body_context + 60) = UINT32_MAX;
        snprintf(body_name, sizeof(body_name), "FangModBodyTab_%u", ++body_generation);
        is_body_requested = 1;
        ((editor_load_fn)(*(void***)manager)[5])(manager, "ContextMenu.swf", body_name,
            1000.0, editor_body_loaded, (void*)(uintptr_t)body_generation);
        trace_client_state("mod_editor_body_tab_requested", body_generation);
    }
    fang_mod_update_palette();
    if (!body_context[56]) return;
    editor_scene_present(editor, ui);
    editor_body_select_tabs(editor, ui);
    if (!editor_body_place(ui)) {
        SetRectEmpty(&body_area);
        SetRectEmpty(&preview_area);
        SetRectEmpty(&reset_area);
        editor_set(root, "_visible", editor_boolean(0));
        return;
    }
    for (index = 0; index < sizeof(native_buttons) / sizeof(native_buttons[0]); ++index) {
        editor_value native = {0};
        if (editor_get(root, native_buttons[index], &native))
            editor_set(&native, "_visible", editor_boolean(0));
        editor_release(&native);
    }
    if (!fang_mod_editor_tab_update(editor_ui_base, ui,
        fang_mod_editor_is_body(editor), !ui[158])) return;
    window = GetActiveWindow();
    if (!window) return;
    if (!editor_body_attach_input(window)) return;
    fang_mod_editor_preview_controls(editor_ui_base, ui, window);
    editor_set(root, "_x", editor_number(body_area.left));
    editor_set(root, "_y", editor_number(body_area.top));
    editor_set(root, "_visible", editor_boolean(1));
    editor_set(root, "_alpha", editor_number(100));
    editor_preview_draw(root, editor, ui);
    editor_reset_draw(root, editor, ui);
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
            "IMPORT EDITOR", editor_scene_open)) goto done;
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

#if FANG_SCENARIO
/* Fixed debug action for a unique owned Arsenal creature by hero noun. Use
 * the existing Creature Edit callback, not the ordinary ship-room request.
 * Contexts are resolved anew from the same native vectors used above; no
 * caller-supplied pointer, actor ID or callback name is accepted. */
uint32_t fang_mod_editor_open_owned(uint32_t hero_noun, int is_import) {
    static const unsigned int vectors[] = {1840, 2440};
    BYTE* controller;
    BYTE* selected = NULL;
    unsigned int index;
    if (!is_editor_ui_enabled || !hero_noun) return 5;
    if (is_import && !fang_mod_scene_available()) return 5;
    controller = *(BYTE**)(editor_ui_base + 0xdbeaa0);
    if (!readable_range(controller, 0xd10) || !controller[920] ||
        *(void**)controller != editor_ui_base + 0xbce048) return 5;
    for (index = 0; index < sizeof(vectors) / sizeof(vectors[0]); ++index) {
        BYTE* begin = *(BYTE**)(controller + vectors[index]);
        BYTE* end = *(BYTE**)(controller + vectors[index] + 4);
        uintptr_t size = (uintptr_t)end - (uintptr_t)begin;
        if ((uintptr_t)end < (uintptr_t)begin || size > 128 * 72 || size % 72 ||
            (size && !readable_range(begin, size))) return 5;
        while (begin != end) {
            if (fang_mod_editor_owned_noun(begin) == hero_noun) {
                if (selected && selected != begin) return 5;
                selected = begin;
            }
            begin += 72;
        }
    }
    if (!selected) return 5;
    if (is_import) fang_mod_editor_open_scene(selected);
    else fang_mod_editor_open(selected);
    return 1; /* Callback returned only; host observes the actual editor. */
}
#endif

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

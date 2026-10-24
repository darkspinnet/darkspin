#include "hook.h"

#include <stdint.h>

typedef BYTE* (FANG_THISCALL* preview_get_fn)(BYTE*);
typedef void (FANG_THISCALL* preview_ref_fn)(BYTE*);
typedef unsigned char (FANG_THISCALL* preview_child_fn)(BYTE*, BYTE*);
typedef void (FANG_THISCALL* preview_flag_fn)(BYTE*, unsigned int, unsigned char);

static BYTE* control_ui;
static BYTE* control_parent;
static BYTE* control_windows[3];
static RECT control_areas[3];
static unsigned int control_press;
static unsigned int control_action;
static HWND control_capture;

static int control_window(BYTE* window) {
    return readable_range(window, 4) && readable_range(*(void**)window, 268);
}

void fang_mod_editor_preview_controls_leave(void) {
    unsigned int index;
    if (control_press && GetCapture() == control_capture) ReleaseCapture();
    for (index = 0; index < 3; ++index) {
        if (control_windows[index]) {
            if (((preview_get_fn)(*(void***)control_windows[index])[4])(control_windows[index]) == control_parent)
                ((preview_child_fn)(*(void***)control_parent)[55])(control_parent, control_windows[index]);
            ((preview_ref_fn)(*(void***)control_windows[index])[1])(control_windows[index]);
            control_windows[index] = NULL;
        }
        SetRectEmpty(control_areas + index);
    }
    if (control_parent) ((preview_ref_fn)(*(void***)control_parent)[1])(control_parent);
    control_parent = NULL;
    control_ui = NULL;
    control_press = 0;
    control_action = 0;
    control_capture = NULL;
}

static int control_create(BYTE* base, BYTE* ui) {
    static const wchar_t* captions[] = {L"IDLE", L"WALK", L"EMOTE"};
    static const wchar_t* tips[] = {L"Play the native editor idle animation",
        L"Play the native walking loop", L"Play the next emote: dances and battle roar"};
    BYTE* donor;
    BYTE* drawable;
    BYTE* allocator;
    BYTE* parent;
    unsigned int depth;
    unsigned int index;
    donor = ((BYTE* (FANG_THISCALL*)(BYTE*, uint32_t))(*(void***)ui)[18])(ui, 0x058a3350);
    if (!control_window(donor)) return 0;
    drawable = ((preview_get_fn)(*(void***)donor)[42])(donor);
    if (!drawable) return 0;
    parent = donor;
    for (depth = 0; depth < 32; ++depth) {
        BYTE* next = ((preview_get_fn)(*(void***)parent)[4])(parent);
        if (!next) break;
        if (!control_window(next) || next == parent) return 0;
        parent = next;
    }
    if (depth == 32) return 0;
    control_parent = parent;
    ((preview_ref_fn)(*(void***)parent)[0])(parent);
    allocator = ((BYTE* (__cdecl*)(void))(base + 0x7de7d0))();
    for (index = 0; index < 3; ++index) {
        BYTE* allocation;
        BYTE* instance;
        BYTE* window;
        BYTE* tooltip;
        allocation = ((BYTE* (__cdecl*)(unsigned int, unsigned int, const char*, BYTE*))
            (base + 0x7de7e0))(2148, 4, "Darkspin/PreviewAction", allocator);
        if (!allocation) goto failed;
        instance = ((BYTE* (FANG_THISCALL*)(BYTE*))(base + 0x7ea860))(allocation);
        if (!instance) goto failed;
        window = instance + 4;
        ((preview_ref_fn)(*(void***)window)[0])(window);
        control_windows[index] = window;
        ((void (FANG_THISCALL*)(BYTE*, uint32_t))(*(void***)window)[20])(window, 0xfad0b010u + index);
        ((void (FANG_THISCALL*)(BYTE*, BYTE*))(*(void***)window)[44])(window, drawable);
        ((void (FANG_THISCALL*)(BYTE*, const wchar_t*))(*(void***)window)[32])(window, captions[index]);
        if (!((preview_child_fn)(*(void***)parent)[54])(parent, window)) goto failed;
        allocation = ((BYTE* (__cdecl*)(unsigned int, unsigned int, const char*, BYTE*))
            (base + 0x7de7e0))(104, 4, "Darkspin/PreviewActionTooltip", allocator);
        if (!allocation) goto failed;
        tooltip = ((BYTE* (FANG_THISCALL*)(BYTE*))(base + 0x51ce20))(allocation);
        ((preview_ref_fn)(*(void***)tooltip)[0])(tooltip);
        ((void (FANG_THISCALL*)(BYTE*, const wchar_t*, int, unsigned char))
            (base + 0x51d030))(tooltip, tips[index], -1, 0);
        ((void (FANG_THISCALL*)(BYTE*, BYTE*))(*(void***)window)[65])(window, tooltip);
        ((preview_ref_fn)(*(void***)tooltip)[1])(tooltip);
    }
    control_ui = ui;
    trace_client_state("mod_editor_preview_action_controls", 3);
    return 1;
failed:
    fang_mod_editor_preview_controls_leave();
    trace_client_state("mod_editor_preview_action_controls_failed", index);
    return 0;
}

void fang_mod_editor_preview_controls(BYTE* base, BYTE* ui, HWND window) {
    BYTE* editor = fang_mod_editor_active();
    RECT client;
    unsigned int index;
    int is_visible = fang_mod_editor_is_scene(editor) &&
        *(unsigned int*)(editor + 856) == 2 && fang_mod_editor_preview_ready(editor);
    if (control_ui && control_ui != ui) fang_mod_editor_preview_controls_leave();
    if (!GetClientRect(window, &client) || client.right < 300 || client.bottom < 300) is_visible = 0;
    if (is_visible && !control_ui && !control_create(base, ui)) return;
    for (index = 0; index < 3; ++index) {
        BYTE* target = control_windows[index];
        BYTE* button;
        if (!target) continue;
        SetRectEmpty(control_areas + index);
        ((preview_flag_fn)(*(void***)target)[31])(target, 1, is_visible);
        if (!is_visible) { control_action = 0; continue; }
        {
            float origin[2];
            float area[4];
            const float* current;
            LONG left = client.right - 282 + index * 90;
            ((float* (FANG_THISCALL*)(BYTE*, float*, float, float))
                (*(void***)control_parent)[49])(control_parent, origin, (float)left, 190.f);
            area[0] = origin[0]; area[1] = origin[1];
            area[2] = origin[0] + 86; area[3] = origin[1] + 32;
            current = ((const float* (FANG_THISCALL*)(BYTE*))(*(void***)target)[14])(target);
            if (!readable_range(current, 16) || current[0] != area[0] || current[1] != area[1] ||
                current[2] != area[2] || current[3] != area[3])
                ((void (FANG_THISCALL*)(BYTE*, const float*))(*(void***)target)[27])(target, area);
            SetRect(control_areas + index, left, 190, left + 86, 222);
        }
        ((preview_flag_fn)(*(void***)target)[31])(target, 2, 1);
        button = ((BYTE* (FANG_THISCALL*)(BYTE*, uint32_t))(*(void***)target)[3])(target, 0x8ed27e7a);
        if (readable_range(button, 4) && readable_range(*(void**)button, 44))
            ((void (FANG_THISCALL*)(BYTE*, unsigned int, int))
                (*(void***)button)[10])(button, 4, index == control_action);
    }
}

int fang_mod_editor_preview_input(HWND window, UINT message, WPARAM wparam, LPARAM lparam) {
    POINT point = {(short)LOWORD(lparam), (short)HIWORD(lparam)};
    BYTE* editor = fang_mod_editor_active();
    unsigned int index;
    (void)wparam;
    if (message == WM_CAPTURECHANGED || message == WM_KILLFOCUS) control_press = 0;
    if (!control_ui || !fang_mod_editor_is_scene(editor) ||
        *(unsigned int*)(editor + 856) != 2) return 0;
    if (message == WM_LBUTTONDOWN) {
        for (index = 0; index < 3; ++index) {
            if (!PtInRect(control_areas + index, point)) continue;
            control_press = index + 1;
            control_capture = window;
            SetCapture(window);
            return 1;
        }
    }
    if (message == WM_LBUTTONUP && control_press) {
        index = control_press - 1;
        control_press = 0;
        if (GetCapture() == window) ReleaseCapture();
        if (PtInRect(control_areas + index, point) && fang_mod_editor_preview_action(index))
            control_action = index;
        return 1;
    }
    return 0;
}

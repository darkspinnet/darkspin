#include "mod/mod.h"
#include "display/display.h"

#include <shobjidl.h>
#include <string.h>
#include <wchar.h>

/* Build 103: borderless changes only the HWND frame. Resolution selection,
 * renderer resets, native fullscreen and Alt+Enter remain owned by the client. */
typedef void* (__cdecl* display_getter_fn)(void);
typedef unsigned char (FANG_THISCALL* fullscreen_read_fn)(void*);
typedef void (FANG_THISCALL* app_update_fn)(void*, unsigned int);

static display_getter_fn display_getter;
static app_update_fn original_app_update;
static PVOID volatile display_window;
static struct {
    int is_requested;
    int is_applied;
    int is_dirty;
    LONG_PTR style;
    LONG_PTR ex_style;
    wchar_t config_path[32768];
} borderless;
static struct {
    int is_requested;
    int is_applied;
    int is_dirty;
} overlay;

// Reads: returns the requested borderless mode.
int fang_borderless_requested(void) {
    return borderless.is_requested;
}

// Modifies: updates the requested borderless mode, marks it dirty, and logs changes.
void fang_request_borderless(int is_enabled) {
    if (borderless.is_requested != (is_enabled != 0)) {
        borderless.is_requested = is_enabled != 0;
        borderless.is_dirty = 1;
        trace_client_state("display_borderless_requested", borderless.is_requested);
    }
}

// Reads: returns the requested taskbar overlay mode.
int fang_taskbar_overlay_requested(void) {
    return overlay.is_requested;
}

// Modifies: updates the requested taskbar overlay, marks it dirty, and logs changes.
void fang_request_taskbar_overlay(int is_enabled) {
    if (overlay.is_requested != (is_enabled != 0)) {
        overlay.is_requested = is_enabled != 0;
        overlay.is_dirty = 1;
        trace_client_state("display_taskbar_overlay_requested", overlay.is_requested);
    }
}

// Modifies: atomically stores the game window used for display changes.
void fang_set_display_window(HWND window) {
    InterlockedExchangePointer(&display_window, window);
}

// Modifies: writes the native display's fullscreen status to the caller's output.
static int read_fullscreen(int* is_fullscreen) {
    void* display = display_getter();
    void** methods;
    if (!readable_range(display, sizeof(void*))) {
        return 0;
    }
    methods = *(void***)display;
    if (!readable_range(methods, 0x58) || methods[0x54 / 4] == NULL) {
        return 0;
    }
    *is_fullscreen = ((fullscreen_read_fn)methods[0x54 / 4])(display) != 0;
    return 1;
}

// Modifies: changes the shell fullscreen hint for the game window and logs the result.
static int set_taskbar_overlay(HWND window, int is_enabled) {
    ITaskbarList2* taskbar = NULL;
    HRESULT initialization = CoInitializeEx(NULL, COINIT_APARTMENTTHREADED);
    HRESULT result;
    if (FAILED(initialization) && initialization != RPC_E_CHANGED_MODE) {
        trace_client_state("display_taskbar_initialize_error", initialization);
        return 0;
    }
    result = CoCreateInstance(&CLSID_TaskbarList, NULL, CLSCTX_INPROC_SERVER,
        &IID_ITaskbarList2, (void**)&taskbar);
    if (SUCCEEDED(result)) {
        result = taskbar->lpVtbl->HrInit(taskbar);
        if (SUCCEEDED(result)) {
            /* This is a shell hint, not a display-mode change. Windows lowers
             * the taskbar while the marked window is active; clearing the hint
             * restores the shell's native detection. Never make the game topmost. */
            result = taskbar->lpVtbl->MarkFullscreenWindow(taskbar, window, is_enabled);
        }
        taskbar->lpVtbl->Release(taskbar);
    }
    if (SUCCEEDED(initialization)) {
        CoUninitialize();
    }
    if (FAILED(result)) {
        trace_client_state("display_taskbar_overlay_error", result);
        return 0;
    }
    trace_client_state("display_taskbar_overlay", is_enabled);
    return 1;
}

// Modifies: applies a pending taskbar overlay request and updates applied state.
static void update_taskbar_overlay(void) {
    HWND window = (HWND)InterlockedCompareExchangePointer(&display_window, NULL, NULL);
    if (window == NULL || !IsWindow(window) ||
        overlay.is_requested == overlay.is_applied) {
        return;
    }
    if (set_taskbar_overlay(window, overlay.is_requested)) {
        overlay.is_applied = overlay.is_requested;
    }
}

// Modifies: changes a window style field and logs Windows errors.
static int set_window_style(HWND window, int index, LONG_PTR style) {
    SetLastError(0);
    if (SetWindowLongPtrW(window, index, style) == 0 && GetLastError() != 0) {
        trace_client_state("display_window_style_error", GetLastError());
        return 0;
    }
    return 1;
}

// Modifies: saves or restores decorations, places borderless at (0, 0), and refreshes the frame.
static int change_window_frame(HWND window, int is_borderless) {
    const LONG_PTR decoration = WS_CAPTION | WS_THICKFRAME | WS_MINIMIZEBOX |
        WS_MAXIMIZEBOX | WS_SYSMENU;
    const LONG_PTR ex_decoration = WS_EX_DLGMODALFRAME | WS_EX_WINDOWEDGE |
        WS_EX_CLIENTEDGE | WS_EX_STATICEDGE;
    const UINT frame_flags = SWP_FRAMECHANGED | SWP_NOSIZE |
        SWP_NOZORDER | SWP_NOACTIVATE;
    const UINT apply_flags = frame_flags | (is_borderless ? 0 : SWP_NOMOVE);
    LONG_PTR previous_style = GetWindowLongPtrW(window, GWL_STYLE);
    LONG_PTR previous_ex_style = GetWindowLongPtrW(window, GWL_EXSTYLE);
    LONG_PTR style = previous_style & ~decoration;
    LONG_PTR ex_style = previous_ex_style & ~ex_decoration;
    if (is_borderless) {
        if (!borderless.is_applied) {
            borderless.style = previous_style & decoration;
            borderless.ex_style = previous_ex_style & ex_decoration;
        } else {
            /* A native window reset may restore frame bits while enabled. */
            borderless.style |= previous_style & decoration;
            borderless.ex_style |= previous_ex_style & ex_decoration;
        }
    } else {
        style |= borderless.style;
        ex_style |= borderless.ex_style;
    }
    if (style == previous_style && ex_style == previous_ex_style &&
        (borderless.is_applied || !is_borderless)) {
        return 1;
    }
    /* Place the undecorated window at the desktop origin after changing its
     * frame, retaining the native dimensions, maximize state and z-order. */
    if (set_window_style(window, GWL_STYLE, style) &&
        set_window_style(window, GWL_EXSTYLE, ex_style) &&
        SetWindowPos(window, NULL, 0, 0, 0, 0, apply_flags)) {
        trace_client_state("display_borderless", is_borderless);
        return 1;
    }
    trace_client_state("display_frame_apply_error", GetLastError());
    int is_style_restored = set_window_style(window, GWL_STYLE, previous_style);
    int is_ex_style_restored = set_window_style(window, GWL_EXSTYLE, previous_ex_style);
    BOOL is_frame_restored = SetWindowPos(window, NULL, 0, 0, 0, 0,
        frame_flags | SWP_NOMOVE);
    if (!is_style_restored || !is_ex_style_restored || !is_frame_restored) {
        trace_client_state("display_frame_rollback_error", GetLastError());
    }
    return 0;
}

// Modifies: writes dirty display preferences to disk, clears saved flags, and logs results.
static void save_display_preferences(void) {
    if (borderless.config_path[0] == L'\0') {
        return;
    }
    if (borderless.is_dirty) {
        if (WritePrivateProfileStringW(L"display", L"is_borderless_mode",
            borderless.is_requested ? L"1" : L"0", borderless.config_path)) {
            borderless.is_dirty = 0;
            trace_client_state("display_borderless_saved", borderless.is_requested);
        } else {
            trace_client_state("display_preferences_write_error", GetLastError());
        }
    }
    if (overlay.is_dirty) {
        if (WritePrivateProfileStringW(L"display", L"is_taskbar_overlay",
            overlay.is_requested ? L"1" : L"0", borderless.config_path)) {
            overlay.is_dirty = 0;
            trace_client_state("display_taskbar_overlay_saved", overlay.is_requested);
        } else {
            trace_client_state("display_overlay_preferences_write_error", GetLastError());
        }
    }
}

// Modifies: reconciles borderless requests with native fullscreen transitions and updates the window frame.
static void update_borderless(void* app) {
    HWND window = (HWND)InterlockedCompareExchangePointer(&display_window, NULL, NULL);
    int is_fullscreen;
    int is_toggle_pending;
    if (!readable_range(app, 0x15C) || window == NULL || !IsWindow(window) ||
        !IsWindowVisible(window) || IsIconic(window) ||
        *(unsigned int*)((BYTE*)app + 0x154) != 0 ||
        !read_fullscreen(&is_fullscreen)) {
        return;
    }
    is_toggle_pending = *((BYTE*)app + 0x159) != 0;
    /* Restore decorations before the native fullscreen transition takes over.
     * Do not intercept or suppress the client's device reset. */
    if (!is_fullscreen && is_toggle_pending) {
        fang_request_borderless(0);
    }
    if (is_fullscreen) {
        if (is_toggle_pending) {
            return;
        }
        fang_request_borderless(0);
        borderless.is_applied = 0;
        return;
    }
    if (*((BYTE*)app + 0x158) != 0) {
        return;
    }
    if (borderless.is_requested || borderless.is_applied) {
        if (!change_window_frame(window, borderless.is_requested)) {
            return;
        }
        borderless.is_applied = borderless.is_requested;
    }
}

// Modifies: runs native app updates and applies mod, display, preference, and options UI changes.
static void FANG_THISCALL hooked_app_update(void* app, unsigned int elapsed) {
    update_borderless(app);
    original_app_update(app, elapsed);
    fang_update_mod();
    update_taskbar_overlay();
    save_display_preferences();
    fang_update_display_ui();
}

// Modifies: loads saved display modes into Fang state and logs the loaded settings.
static void load_display_preferences(void) {
    static const wchar_t suffix[] = L"\\DarksporeData\\Preferences\\DarkspinDisplay.ini";
    DWORD capacity = sizeof(borderless.config_path) / sizeof(wchar_t);
    DWORD length = GetEnvironmentVariableW(L"APPDATA", borderless.config_path, capacity);
    if (length == 0 || length >= capacity - sizeof(suffix) / sizeof(wchar_t)) {
        borderless.config_path[0] = L'\0';
        trace_client_state("display_preferences_path_missing", 1);
        return;
    }
    memcpy(borderless.config_path + length, suffix, sizeof(suffix));
    /* The retired experimental mode used is_borderless plus window dimensions.
     * Its desktop-size behavior is not this option. Start the new mode off and
     * only restore a selection explicitly saved by the new graphics control. */
    borderless.is_requested = GetPrivateProfileIntW(
        L"display", L"is_borderless_mode", 0, borderless.config_path) == 1;
    overlay.is_requested = GetPrivateProfileIntW(
        L"display", L"is_taskbar_overlay", 0, borderless.config_path) == 1;
    trace_client_state("display_borderless_loaded", borderless.is_requested);
    trace_client_state("display_taskbar_overlay_loaded", overlay.is_requested);
}

// Modifies: loads display preferences and patches native update and options UI callbacks.
int fang_install_display_preferences(HMODULE executable) {
    static const BYTE signature[] = {
        0x83, 0xEC, 0x30, 0xB8, 0x64, 0x00, 0x00, 0x00, 0x55, 0x8B, 0xE9,
    };
    BYTE* base = (BYTE*)executable;
    void** slot = (void**)(base + 0xC0601C);
    if (sizeof(void*) != 4 || !readable_range(slot, sizeof(void*)) ||
        !readable_range(base + 0x3EA210, sizeof(signature)) ||
        memcmp(base + 0x3EA210, signature, sizeof(signature)) != 0 ||
        *slot != base + 0x3EA210) {
        return 0;
    }
    display_getter = (display_getter_fn)(base + 0x3AEB90);
    original_app_update = (app_update_fn)(base + 0x3EA210);
    load_display_preferences();
    if (!patch_pointer(slot, original_app_update, hooked_app_update)) {
        return 0;
    }
    if (!fang_install_display_ui(base)) {
        goto restore_update;
    }
    return 1;
restore_update:
    if (!patch_pointer(slot, hooked_app_update, original_app_update)) {
        trace_client_state("display_update_rollback_failed", 1);
    }
    return 0;
}

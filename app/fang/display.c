#include "resolution.h"

#include <stdint.h>
#include <string.h>
#include <wchar.h>

/* Darkspore 5.3.0.103: Alt+Enter and Graphics Fullscreen select a borderless
 * window, with native windowed rendering and the normal preference saver.
 * Window changes, including committed resolution selections, run on the
 * client thread before App::Update prepares the next frame. */
enum {
    option_screen_size = 0x046170A1,
    option_fullscreen = 0x046170A2,
    display_getter_rva = 0x3AEB90,
    settings_getter_rva = 0x3AEBD0,
    shortcut_rva = 0xA8D980,
    shortcut_call_rva = 0x518F54,
    app_update_rva = 0x3EA210,
    app_update_slot_rva = 0xC0601C,
    settings_vtable_rva = 0xC0D498,
    option_write_rva = 0x4562D0,
    option_read_rva = 0x4542D0,
    resolution_read_rva = 0x454650,
    app_busy_offset = 0x154,
    app_reset_offset = 0x158,
    app_fullscreen_offset = 0x159,
    app_vsync_offset = 0x15B,
};

typedef void* (__cdecl* manager_getter_fn)(void);
typedef unsigned char (__cdecl* shortcut_fn)(unsigned int key, unsigned int modifiers);
typedef void (FANG_THISCALL* app_update_fn)(void* app, unsigned int elapsed);
typedef unsigned char (FANG_THISCALL* fullscreen_read_fn)(void* display);
typedef unsigned int (FANG_THISCALL* option_read_fn)(void* settings, unsigned int option);
typedef void (FANG_THISCALL* option_write_fn)(void* settings,
    unsigned int option, unsigned int selection);
typedef unsigned char (FANG_THISCALL* preferences_save_fn)(void* settings);

static BYTE* client_base;
static shortcut_fn original_shortcut;
static app_update_fn original_app_update;
static option_write_fn original_option_write;
static manager_getter_fn display_getter;
static manager_getter_fn settings_getter;

static PVOID volatile display_window;

/* Only window discovery runs on the branding thread. All layout and preference
 * state belongs to the native client thread. Borderless has its own setting:
 * both supported modes use OptionFullScreen=0 in native preferences. */
static struct {
    int is_pending;
    int is_borderless;
    int is_borderless_requested;
    int is_waiting_for_windowed;
    int is_preference_dirty;
    int is_resolution_pending;
    int is_window_resolution_loaded;
    int is_initialized;
    struct fang_resolution window_resolution;
    LONG_PTR window_style;
    LONG_PTR window_ex_style;
    WINDOWPLACEMENT window_placement;
    wchar_t config_path[32768];
} display_preference;

/* Only these UI call sites use this view of the display manager: Flash graphics
 * options, legacy refresh/toggle, and Flash's final Apply comparison.
 * Each only calls IsFullscreen (slot 0x54) and discards the object.
 * Device/window bookkeeping must continue seeing the real windowed state. */
static const unsigned int fullscreen_ui_calls[] = {0x03D53E, 0x377257, 0x377327, 0x03B1BD};

static unsigned char FANG_THISCALL read_borderless(void* display) {
    (void)display;
    return (unsigned char)display_preference.is_borderless_requested;
}

static void* __cdecl fullscreen_ui_display(void) {
    static void* methods[0x58 / 4] = {[0x54 / 4] = (void*)read_borderless};
    static void** display = methods;
    return &display;
}

static unsigned int FANG_THISCALL hooked_option_read(void* settings, unsigned int option) {
    option_read_fn read = (option_read_fn)(client_base + option_read_rva);
    if (option == option_fullscreen && display_preference.is_initialized) {
        return (unsigned int)display_preference.is_borderless_requested;
    }
    return read(settings, option);
}

void fang_set_display_window(HWND window) {
    InterlockedExchangePointer(&display_window, window);
}

static int read_fullscreen(int* is_fullscreen) {
    void* display = display_getter();
    void** methods;
    fullscreen_read_fn read;
    if (!readable_range(display, sizeof(void*))) {
        return 0;
    }
    methods = *(void***)display;
    if (!readable_range(methods, 0x58) || methods[0x54 / 4] == NULL) {
        return 0;
    }
    read = (fullscreen_read_fn)methods[0x54 / 4];
    *is_fullscreen = read(display) != 0;
    return 1;
}

static int save_display_setting(const wchar_t* key, int selection) {
    wchar_t number[16];
    if (swprintf(number, sizeof(number) / sizeof(wchar_t), L"%d", selection) < 0) {
        trace_client_state("display_preferences_format_failed", 1);
        return 0;
    }
    if (!WritePrivateProfileStringW(L"display", key, number, display_preference.config_path)) {
        trace_client_state("display_preferences_write_error", GetLastError());
        return 0;
    }
    return 1;
}

static void save_display_preference(void) {
    void* settings = settings_getter();
    void** methods;
    option_read_fn read;
    option_write_fn write;
    preferences_save_fn save;
    unsigned char is_saved;
    if (!readable_range(settings, sizeof(void*)) ||
        *(void***)settings != (void**)(client_base + settings_vtable_rva)) {
        trace_client_state("display_preferences_manager_missing", 1);
        return;
    }
    methods = *(void***)settings;
    /* Persist the actual device mode without treating this internal write as
     * an unchecked Graphics checkbox. Borderless is stored in the local INI. */
    read = (option_read_fn)(client_base + option_read_rva);
    write = original_option_write;
    save = (preferences_save_fn)methods[0x48 / 4];
    write(settings, option_fullscreen, 0);
    if (read(settings, option_fullscreen) != 0) {
        trace_client_state("display_preferences_option_rejected", 1);
        return;
    }
    is_saved = save(settings);
    trace_client_state("display_preferences_saved", is_saved != 0);
    if (!is_saved || display_preference.config_path[0] == L'\0') {
        return;
    }
    if (!save_display_setting(L"window_width", display_preference.window_resolution.width) ||
        !save_display_setting(L"window_height", display_preference.window_resolution.height) ||
        !save_display_setting(L"window_refresh", display_preference.window_resolution.refresh)) {
        return;
    }
    if (!save_display_setting(L"is_borderless", display_preference.is_borderless)) {
        return;
    }
}

static int set_window_style(HWND window, int index, LONG_PTR style) {
    SetLastError(0);
    if (SetWindowLongPtrW(window, index, style) == 0 && GetLastError() != 0) {
        trace_client_state("display_window_style_error", GetLastError());
        return 0;
    }
    return 1;
}

static int restore_window(HWND window) {
    int is_style_restored = set_window_style(window, GWL_STYLE, display_preference.window_style);
    int is_ex_style_restored = set_window_style(window, GWL_EXSTYLE, display_preference.window_ex_style);
    BOOL is_placement_restored = SetWindowPlacement(window, &display_preference.window_placement);
    BOOL is_frame_restored = SetWindowPos(window, NULL, 0, 0, 0, 0,
        SWP_FRAMECHANGED | SWP_NOMOVE | SWP_NOSIZE | SWP_NOZORDER | SWP_NOACTIVATE);
    if (!is_style_restored || !is_ex_style_restored ||
        !is_placement_restored || !is_frame_restored) {
        trace_client_state("display_window_restore_failed", 1);
        return 0;
    }
    return 1;
}

static int position_borderless(HWND window, const RECT* monitor) {
    LONG_PTR style;
    LONG_PTR ex_style;
    style = (display_preference.window_style & ~(WS_CAPTION | WS_THICKFRAME |
        WS_MINIMIZEBOX | WS_MAXIMIZEBOX | WS_SYSMENU | WS_MAXIMIZE | WS_MINIMIZE)) | WS_POPUP;
    ex_style = display_preference.window_ex_style & ~(WS_EX_DLGMODALFRAME |
        WS_EX_WINDOWEDGE | WS_EX_CLIENTEDGE | WS_EX_STATICEDGE);
    if (set_window_style(window, GWL_STYLE, style) &&
        set_window_style(window, GWL_EXSTYLE, ex_style) &&
        SetWindowPos(window, NULL, monitor->left, monitor->top,
            monitor->right - monitor->left, monitor->bottom - monitor->top,
            SWP_FRAMECHANGED | SWP_NOZORDER | SWP_NOACTIVATE | SWP_NOSENDCHANGING)) {
        return 1;
    }
    trace_client_state("display_borderless_position_failed", 1);
    return 0;
}

static int remember_window_resolution(HWND window, const struct fang_resolution* resolution) {
    RECT frame = {0, 0, resolution->width, resolution->height};
    RECT* placement = &display_preference.window_placement.rcNormalPosition;
    if (!AdjustWindowRectEx(&frame, (DWORD)(display_preference.window_style & ~WS_MAXIMIZE),
        GetMenu(window) != NULL, (DWORD)display_preference.window_ex_style)) {
        trace_client_state("display_resolution_frame_error", GetLastError());
        return 0;
    }
    placement->right = placement->left + frame.right - frame.left;
    placement->bottom = placement->top + frame.bottom - frame.top;
    display_preference.window_placement.showCmd = SW_SHOWNORMAL;
    display_preference.window_placement.flags &= ~WPF_RESTORETOMAXIMIZED;
    display_preference.window_style &= ~WS_MAXIMIZE;
    display_preference.window_resolution = *resolution;
    return 1;
}

static int change_display_mode(void* app, HWND window) {
    MONITORINFOEXW monitor = {0};
    DEVMODEW desktop = {0};
    struct fang_resolution previous;
    struct fang_resolution target;
    int is_changed;
    monitor.cbSize = sizeof(monitor);
    if (!GetMonitorInfoW(MonitorFromWindow(window, MONITOR_DEFAULTTONEAREST),
        (MONITORINFO*)&monitor) || !fang_read_resolution(client_base, &previous)) {
        trace_client_state("display_mode_capture_failed", 1);
        return 0;
    }
    target = display_preference.window_resolution;
    if (display_preference.is_borderless_requested) {
        display_preference.window_placement.length = sizeof(WINDOWPLACEMENT);
        if (!GetWindowPlacement(window, &display_preference.window_placement)) {
            trace_client_state("display_window_capture_error", GetLastError());
            return 0;
        }
        display_preference.window_style = GetWindowLongPtrW(window, GWL_STYLE);
        display_preference.window_ex_style = GetWindowLongPtrW(window, GWL_EXSTYLE);
        if (display_preference.is_window_resolution_loaded) {
            if (!remember_window_resolution(window, &display_preference.window_resolution)) {
                return 0;
            }
        } else {
            display_preference.window_resolution = previous;
        }
        desktop.dmSize = sizeof(desktop);
        if (!EnumDisplaySettingsW(monitor.szDevice, ENUM_CURRENT_SETTINGS, &desktop)) {
            trace_client_state("display_desktop_read_failed", 1);
            return 0;
        }
        /* EnumDisplaySettings reports physical pixels even under DPI scaling.
         * Keep rcMonitor for positioning in the window's coordinate system. */
        target.width = (int)desktop.dmPelsWidth;
        target.height = (int)desktop.dmPelsHeight;
        target.refresh = (int)desktop.dmDisplayFrequency;
    }
    if (!fang_select_resolution(client_base, &target)) {
        return 0;
    }
    is_changed = fang_render_resolution(client_base, app, &target);
    if (is_changed) {
        is_changed = display_preference.is_borderless_requested ?
            position_borderless(window, &monitor.rcMonitor) : restore_window(window);
    }
    if (is_changed) {
        display_preference.is_window_resolution_loaded = 0;
        display_preference.is_preference_dirty = 1;
        return 1;
    }
    /* Keep the saved setting, render buffer and window in the previous mode
     * if either the native reset or the final window layout fails. */
    if (!fang_select_resolution(client_base, &previous)) {
        trace_client_state("display_option_rollback_failed", 1);
    }
    if (!fang_render_resolution(client_base, app, &previous)) {
        trace_client_state("display_resolution_rollback_failed", 1);
    }
    is_changed = display_preference.is_borderless ?
        position_borderless(window, &monitor.rcMonitor) : restore_window(window);
    if (!is_changed) {
        trace_client_state("display_window_rollback_failed", 1);
    }
    return 0;
}

static void FANG_THISCALL hooked_option_write(void* settings,
    unsigned int option, unsigned int selection) {
    option_read_fn read = (option_read_fn)(client_base + option_read_rva);
    unsigned int previous = option == option_screen_size ? read(settings, option) : selection;
    if (option == option_fullscreen) {
        if (selection > 1) {
            return;
        }
        /* Preference loading must not overwrite the separately saved borderless
         * choice with OptionFullScreen=0. After startup this is Graphics Apply,
         * including the native revert path. Never commit an exclusive property. */
        if (display_preference.is_initialized) {
            display_preference.is_borderless_requested = selection != 0;
            display_preference.is_pending = 1;
            display_preference.is_preference_dirty = 1;
        }
        original_option_write(settings, option, 0);
        return;
    }
    original_option_write(settings, option, selection);
    if (option == option_screen_size && previous != selection && read(settings, option) == selection) {
        /* Graphics Apply (and its revert path) commits the selection here.
         * Resize before the next App::Update, outside the option/UI callback.
         * Startup preference loading also uses this setter. Unchanged selections
         * must not replace the remembered windowed size while in borderless mode. */
        display_preference.is_resolution_pending = 1;
    }
}

static void apply_window_resolution(void* app, HWND window) {
    struct fang_resolution resolution;
    RECT frame = {0};
    RECT client;
    RECT previous_window;
    MONITORINFO monitor = {0};
    LONG_PTR style;
    LONG_PTR ex_style;
    display_preference.is_resolution_pending = 0;
    if (!fang_read_resolution(client_base, &resolution)) {
        return;
    }
    if (display_preference.is_borderless) {
        monitor.cbSize = sizeof(monitor);
        if (!GetMonitorInfoW(MonitorFromWindow(window, MONITOR_DEFAULTTONEAREST), &monitor)) {
            trace_client_state("display_monitor_read_error", GetLastError());
            return;
        }
        if (!fang_render_resolution(client_base, app, &resolution) ||
            !position_borderless(window, &monitor.rcMonitor) ||
            !remember_window_resolution(window, &resolution)) {
            return;
        }
        display_preference.is_preference_dirty = 1;
        return;
    }
    style = GetWindowLongPtrW(window, GWL_STYLE);
    ex_style = GetWindowLongPtrW(window, GWL_EXSTYLE);
    frame.right = resolution.width;
    frame.bottom = resolution.height;
    if (!AdjustWindowRectEx(&frame, (DWORD)(style & ~WS_MAXIMIZE),
        GetMenu(window) != NULL, (DWORD)ex_style)) {
        trace_client_state("display_resolution_frame_error", GetLastError());
        return;
    }
    if (IsZoomed(window)) {
        ShowWindow(window, SW_RESTORE);
    }
    if (!GetWindowRect(window, &previous_window)) {
        trace_client_state("display_window_read_error", GetLastError());
        return;
    }
    if (!fang_render_resolution(client_base, app, &resolution)) {
        return;
    }
    /* Keep WM_SIZE enabled for native window bookkeeping and input scaling.
     * The separate UI resize queued by fang_render_resolution is consumed
     * during the next update, after these final window bounds are in place. */
    if (!SetWindowPos(window, NULL, previous_window.left, previous_window.top,
        frame.right - frame.left, frame.bottom - frame.top, SWP_NOZORDER | SWP_NOACTIVATE)) {
        trace_client_state("display_resolution_resize_error", GetLastError());
        return;
    }
    if (!GetClientRect(window, &client)) {
        trace_client_state("display_resolution_read_error", GetLastError());
        return;
    }
    trace_client_state(client.right == resolution.width && client.bottom == resolution.height ?
        "display_resolution_applied" : "display_resolution_size_mismatch",
        ((unsigned int)client.right << 16) | (unsigned int)client.bottom);
}

static unsigned char __cdecl hooked_shortcut(unsigned int key, unsigned int modifiers) {
    if (key != VK_RETURN || (modifiers & ~0x40u) != 4) {
        return original_shortcut(key, modifiers);
    }
    display_preference.is_borderless_requested = !display_preference.is_borderless_requested;
    display_preference.is_pending = 1;
    display_preference.is_preference_dirty = 1;
    return 1;
}

static void apply_display_preference(void* app) {
    HWND window;
    int is_fullscreen;
    if ((!display_preference.is_pending && !display_preference.is_resolution_pending) ||
        !readable_range(app, app_vsync_offset + 1) ||
        *(unsigned int*)((BYTE*)app + app_busy_offset) != 0 ||
        *(unsigned int*)((BYTE*)app + app_reset_offset) != 0) {
        return;
    }
    window = (HWND)InterlockedCompareExchangePointer(&display_window, NULL, NULL);
    if (window == NULL || !IsWindow(window) || !IsWindowVisible(window) || IsIconic(window) ||
        !read_fullscreen(&is_fullscreen)) {
        return;
    }
    if (is_fullscreen) {
        if (!display_preference.is_pending) {
            return;
        }
        if (display_preference.is_waiting_for_windowed) {
            trace_client_state("display_preferences_switch_failed", 1);
            display_preference.is_pending = 0;
            display_preference.is_waiting_for_windowed = 0;
            display_preference.is_borderless_requested = display_preference.is_borderless;
            return;
        }
        /* An existing exclusive preference, or Graphics Apply, must first use
         * the native device-reset path. Never change styles on an exclusive device. */
        display_preference.is_waiting_for_windowed = 1;
        display_preference.is_preference_dirty = 1;
        original_shortcut(VK_RETURN, 4);
        return;
    }
    display_preference.is_waiting_for_windowed = 0;
    display_preference.is_initialized = 1;
    display_preference.is_pending = 0;
    if (display_preference.is_resolution_pending) {
        apply_window_resolution(app, window);
    }
    if (display_preference.is_borderless_requested != display_preference.is_borderless) {
        int is_changed = change_display_mode(app, window);
        if (!is_changed) {
            display_preference.is_borderless_requested = display_preference.is_borderless;
            return;
        }
        display_preference.is_borderless = display_preference.is_borderless_requested;
        trace_client_state("display_borderless", (unsigned int)display_preference.is_borderless);
    }
    if (display_preference.is_preference_dirty) {
        display_preference.is_preference_dirty = 0;
        save_display_preference();
    }
}

static void FANG_THISCALL hooked_app_update(void* app, unsigned int elapsed) {
    /* Legacy Graphics Apply also queues the App exclusive toggle before it
     * commits OptionFullScreen. The setter owns that request now. Suppress the
     * redundant device toggle, retaining only recovery from an old exclusive
     * startup mode. Unaccompanied native toggles select borderless too. */
    if (readable_range(app, app_fullscreen_offset + 1) &&
        *((BYTE*)app + app_fullscreen_offset) &&
        !display_preference.is_waiting_for_windowed) {
        *((BYTE*)app + app_fullscreen_offset) = 0;
        if (!display_preference.is_pending) {
            display_preference.is_borderless_requested = !display_preference.is_borderless_requested;
            display_preference.is_pending = 1;
            display_preference.is_preference_dirty = 1;
        }
    }
    /* Reset the renderer and queue the matching UI size before the native
     * update, as the native mode-switch path does. Resetting afterward leaves
     * the prepared scene/UI frame using the previous render target dimensions. */
    apply_display_preference(app);
    original_app_update(app, elapsed);
}

static void load_display_preference(void) {
    static const wchar_t suffix[] = L"\\DarksporeData\\Preferences\\DarkspinDisplay.ini";
    DWORD capacity = sizeof(display_preference.config_path) / sizeof(wchar_t);
    DWORD length = GetEnvironmentVariableW(L"APPDATA", display_preference.config_path, capacity);
    if (length == 0 || length >= capacity - sizeof(suffix) / sizeof(wchar_t)) {
        display_preference.config_path[0] = L'\0';
        trace_client_state("display_preferences_path_missing", 1);
    } else {
        memcpy(display_preference.config_path + length, suffix, sizeof(suffix));
        display_preference.is_borderless_requested = GetPrivateProfileIntW(
            L"display", L"is_borderless", 0, display_preference.config_path) == 1;
        if (display_preference.is_borderless_requested) {
            display_preference.window_resolution.width = GetPrivateProfileIntW(
                L"display", L"window_width", 0, display_preference.config_path);
            display_preference.window_resolution.height = GetPrivateProfileIntW(
                L"display", L"window_height", 0, display_preference.config_path);
            display_preference.window_resolution.refresh = GetPrivateProfileIntW(
                L"display", L"window_refresh", 0, display_preference.config_path);
            display_preference.is_window_resolution_loaded =
                display_preference.window_resolution.width > 0 &&
                display_preference.window_resolution.width <= 32767 &&
                display_preference.window_resolution.height > 0 &&
                display_preference.window_resolution.height <= 32767;
        }
    }
    display_preference.is_pending = 1;
    display_preference.is_resolution_pending = 1;
}

int fang_install_display_preferences(HMODULE executable) {
    static const BYTE shortcut_signature[] = {
        0x8B, 0x4C, 0x24, 0x08, 0x8B, 0x44, 0x24, 0x04,
        0x83, 0xEC, 0x40, 0x83, 0xE1, 0xBF, 0x83, 0xF8, 0x0D,
    };
    static const BYTE update_signature[] = {
        0x83, 0xEC, 0x30, 0xB8, 0x64, 0x00, 0x00, 0x00, 0x55, 0x8B, 0xE9,
    };
    BYTE* base = (BYTE*)executable;
    IMAGE_DOS_HEADER* dos = (IMAGE_DOS_HEADER*)base;
    IMAGE_NT_HEADERS* nt;
    void** update_slot;
    void** setting_slots;
    int32_t shortcut_relative;
    size_t ui_hook_count = 0;
    if (sizeof(void*) != 4 || !readable_range(dos, sizeof(*dos)) ||
        dos->e_magic != IMAGE_DOS_SIGNATURE) {
        return 0;
    }
    nt = (IMAGE_NT_HEADERS*)(base + dos->e_lfanew);
    if (!readable_range(nt, sizeof(*nt)) || nt->Signature != IMAGE_NT_SIGNATURE ||
        nt->FileHeader.Machine != IMAGE_FILE_MACHINE_I386 ||
        nt->OptionalHeader.SizeOfImage < 0x105DB9C) {
        return 0;
    }
    if (memcmp(base + shortcut_rva, shortcut_signature, sizeof(shortcut_signature)) != 0 ||
        memcmp(base + app_update_rva, update_signature, sizeof(update_signature)) != 0 ||
        base[shortcut_call_rva] != 0xE8) {
        return 0;
    }
    memcpy(&shortcut_relative, base + shortcut_call_rva + 1, sizeof(shortcut_relative));
    update_slot = (void**)(base + app_update_slot_rva);
    setting_slots = (void**)(base + settings_vtable_rva);
    if (base + shortcut_call_rva + 5 + shortcut_relative != base + shortcut_rva ||
        *update_slot != base + app_update_rva ||
        setting_slots[0x30 / 4] != base + option_write_rva ||
        setting_slots[0x34 / 4] != base + option_read_rva ||
        setting_slots[0x54 / 4] != base + 0x454620 ||
        setting_slots[0x58 / 4] != base + resolution_read_rva ||
        *(void**)(base + 0xC0F558 + 0x18) != base + 0x48E8C0 ||
        *(void**)(base + 0xC0F558 + 0x1C) != base + 0x48DC90 ||
        *(void**)(base + 0xC5E798 + 0x0C) != base + 0x7E4020) {
        return 0;
    }
    client_base = base;
    original_shortcut = (shortcut_fn)(base + shortcut_rva);
    original_app_update = (app_update_fn)(base + app_update_rva);
    original_option_write = (option_write_fn)(base + option_write_rva);
    display_getter = (manager_getter_fn)(base + display_getter_rva);
    settings_getter = (manager_getter_fn)(base + settings_getter_rva);
    for (size_t index = 0; index < sizeof(fullscreen_ui_calls) / sizeof(fullscreen_ui_calls[0]); index++) {
        BYTE* call = base + fullscreen_ui_calls[index];
        int32_t relative;
        static const BYTE query[] = {0x8B, 0x10, 0x8B, 0xC8, 0x8B, 0x42, 0x54, 0xFF, 0xD0};
        memcpy(&relative, call + 1, sizeof(relative));
        if (*call != 0xE8 || call + 5 + relative != base + display_getter_rva ||
            (index < 3 && memcmp(call + 5, query, sizeof(query)) != 0)) {
            return 0;
        }
    }
    {
        static const BYTE apply_query[] = {0x8B, 0x13, 0x8B, 0xF8, 0x8B, 0x42, 0x54,
            0x4F, 0xF7, 0xDF, 0x1B, 0xFF, 0x8B, 0xCB, 0x47, 0xFF, 0xD0};
        if (memcmp(base + 0x03B1D7, apply_query, sizeof(apply_query)) != 0) {
            return 0;
        }
    }
    load_display_preference();
    if (!patch_pointer(update_slot, (void*)original_app_update, (void*)hooked_app_update)) {
        return 0;
    }
    if (!patch_pointer(setting_slots + 0x30 / 4, (void*)original_option_write,
        (void*)hooked_option_write)) {
        if (!patch_pointer(update_slot, (void*)hooked_app_update, (void*)original_app_update)) {
            trace_client_state("display_preferences_hook_restore_failed", 1);
        }
        return 0;
    }
    if (!patch_pointer(setting_slots + 0x34 / 4, base + option_read_rva,
        (void*)hooked_option_read)) {
        goto restore_write;
    }
    for (; ui_hook_count < sizeof(fullscreen_ui_calls) / sizeof(fullscreen_ui_calls[0]); ui_hook_count++) {
        if (!patch_call(base + fullscreen_ui_calls[ui_hook_count], (void*)display_getter,
            (void*)fullscreen_ui_display)) {
            goto restore_ui;
        }
    }
    if (!patch_call(base + shortcut_call_rva, (void*)original_shortcut, (void*)hooked_shortcut)) {
        goto restore_ui;
    }
    return 1;

restore_ui:
    while (ui_hook_count > 0) {
        ui_hook_count--;
        if (!patch_call(base + fullscreen_ui_calls[ui_hook_count], (void*)fullscreen_ui_display,
            (void*)display_getter)) {
            trace_client_state("display_checkbox_hook_restore_failed", 1);
        }
    }
    if (!patch_pointer(setting_slots + 0x34 / 4, (void*)hooked_option_read, base + option_read_rva)) {
        trace_client_state("display_option_read_hook_restore_failed", 1);
    }
restore_write:
    if (!patch_pointer(setting_slots + 0x30 / 4, (void*)hooked_option_write, (void*)original_option_write)) {
        trace_client_state("display_resolution_hook_restore_failed", 1);
    }
    if (!patch_pointer(update_slot, (void*)hooked_app_update, (void*)original_app_update)) {
        trace_client_state("display_preferences_hook_restore_failed", 1);
    }
    return 0;
}

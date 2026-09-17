#include "resolution.h"

#include <stdint.h>
#include <string.h>
#include <wchar.h>

/* Darkspore 5.3.0.103: Alt+Enter normally queues an unsaved exclusive-mode
 * switch. The approved compatibility hook instead toggles a borderless
 * window, with native windowed rendering and the normal preference saver.
 * Window changes, including committed resolution selections, run on the
 * client thread after App::Update. */
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
    struct fang_resolution window_resolution;
    LONG_PTR window_style;
    LONG_PTR window_ex_style;
    WINDOWPLACEMENT window_placement;
    wchar_t config_path[32768];
} display_preference;

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
    read = (option_read_fn)methods[0x34 / 4];
    write = (option_write_fn)methods[0x30 / 4];
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
    original_option_write(settings, option, selection);
    if (option == option_screen_size && previous != selection && read(settings, option) == selection) {
        /* Graphics Apply (and its revert path) commits the selection here.
         * Resize after App::Update, outside the option/UI callback. Startup
         * preference loading also uses this setter. Unchanged selections must
         * not replace the remembered windowed size while in borderless mode. */
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
    /* WM_SIZE reaches the native handler (0xB31600), which updates its window
     * rectangle and emits resize event 0x01EE1003. Keep normal window messages
     * enabled so native consumers follow the new client area. */
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

static void FANG_THISCALL hooked_app_update(void* app, unsigned int elapsed) {
    HWND window;
    int is_fullscreen;
    original_app_update(app, elapsed);
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
        *(void**)(base + 0xC0F558 + 0x1C) != base + 0x48DC90) {
        return 0;
    }
    client_base = base;
    original_shortcut = (shortcut_fn)(base + shortcut_rva);
    original_app_update = (app_update_fn)(base + app_update_rva);
    original_option_write = (option_write_fn)(base + option_write_rva);
    display_getter = (manager_getter_fn)(base + display_getter_rva);
    settings_getter = (manager_getter_fn)(base + settings_getter_rva);
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
    if (!patch_call(base + shortcut_call_rva, (void*)original_shortcut,
        (void*)hooked_shortcut)) {
        if (!patch_pointer(setting_slots + 0x30 / 4, (void*)hooked_option_write,
            (void*)original_option_write)) {
            trace_client_state("display_resolution_hook_restore_failed", 1);
        }
        if (!patch_pointer(update_slot, (void*)hooked_app_update, (void*)original_app_update)) {
            trace_client_state("display_preferences_hook_restore_failed", 1);
        }
        return 0;
    }
    return 1;
}

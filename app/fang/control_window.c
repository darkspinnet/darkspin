#include "control.h"
#if FANG_SCENARIO
#include <windows.h>
#include <string.h>

/* Approved development-only finite input. The bridge posts a wake-up; the
 * existing game WndProc processes move/down/up on its actual owner thread. */
static SRWLOCK window_lock = SRWLOCK_INIT;
static HWND control_window;
static UINT control_message;
static uint32_t control_request;
static int32_t control_x, control_y;
static uint64_t control_deadline;
static int is_executing;
static int control_kind; /* 0 click, 1 responsiveness, 2 scene setter, 3 scene ready, 4 key, 5 map state */
static fang_control_window_receipt control_receipt;
static fang_control_map_state control_map_state;
static fang_control_deployment_state control_deployment_state;
static fang_control_arena_state control_arena_state;
static fang_control_beam_state control_beam_state;
static fang_control_screen_state control_screen_state;
static fang_control_editor_tree control_editor_tree;
static int is_editor_canceled;
static uint64_t control_screen_owner_generation;
static int is_screen_canceled;
static uint32_t control_scene;
static char control_text[256];
static uint32_t control_text_length;
static int is_text_canceled;

typedef struct control_window_scan {
    HWND window;
    unsigned int count;
} control_window_scan;

static BOOL CALLBACK observe_control_window(HWND window, LPARAM parameter) {
    control_window_scan* scan = (control_window_scan*)parameter;
    DWORD process_id = 0;
    char class_name[128];
    GetWindowThreadProcessId(window, &process_id);
    if (process_id != GetCurrentProcessId() || !IsWindowVisible(window) ||
        GetWindow(window, GW_OWNER) != NULL) return TRUE;
    /* GetWindowText on another thread in this process can synchronously run
     * WM_GETTEXT. Inspect class metadata instead; never call the game here. */
    if (GetClassNameA(window, class_name, sizeof(class_name)) <= 0 ||
        strcmp(class_name, "#32770") == 0) return TRUE;
    scan->window = window;
    scan->count++;
    return scan->count < 2;
}

/* Read-only installed Win32 APIs. No game pointers, native callbacks, window
 * messages, cursor changes, activation, or retained window handles. */
int fang_control_window_inspect(fang_control_window_snapshot* snapshot) {
    control_window_scan scan = {0};
    DWORD process_id = 0;
    RECT rect;
    if (!snapshot) return 0;
    memset(snapshot, 0, sizeof(*snapshot));
    if (!EnumWindows(observe_control_window, (LPARAM)&scan) || scan.count != 1 ||
        !IsWindow(scan.window) || !GetClientRect(scan.window, &rect)) return 0;
    snapshot->thread_id = GetWindowThreadProcessId(scan.window, &process_id);
    if (process_id != GetCurrentProcessId() || !snapshot->thread_id ||
        rect.left != 0 || rect.top != 0 || rect.right <= 0 || rect.bottom <= 0 ||
        rect.right > 32767 || rect.bottom > 32767) return 0;
    snapshot->window_id = (uint32_t)(uintptr_t)scan.window;
    snapshot->process_id = process_id;
    snapshot->width = (uint32_t)rect.right;
    snapshot->height = (uint32_t)rect.bottom;
    snapshot->is_enabled = IsWindowEnabled(scan.window) != 0;
    snapshot->is_minimized = IsIconic(scan.window) != 0;
    snapshot->is_available = 1;
    return 1;
}

void fang_control_window_attach(HWND window) {
    DWORD process_id = 0;
    if (!window || !GetWindowThreadProcessId(window, &process_id) || process_id != GetCurrentProcessId()) return;
    AcquireSRWLockExclusive(&window_lock);
    if (!control_message) control_message = RegisterWindowMessageA("Darkspin.Scenario.WindowCommand.v1");
    if (control_window != window && !is_executing) {
        control_window = window;
        if (control_request && control_receipt.status == 0) control_receipt.status = 2;
    }
    ReleaseSRWLockExclusive(&window_lock);
}

static int control_click_window(HWND window, int32_t x, int32_t y) {
    DWORD process_id = 0;
    RECT rect;
    return window && IsWindow(window) && GetWindowThreadProcessId(window, &process_id) != 0 &&
        process_id == GetCurrentProcessId() && IsWindowVisible(window) && IsWindowEnabled(window) &&
        !IsIconic(window) && GetWindow(window, GW_OWNER) == NULL && GetClientRect(window, &rect) &&
        rect.left == 0 && rect.top == 0 && x >= 0 && y >= 0 && x < rect.right && y < rect.bottom &&
        rect.right <= 32767 && rect.bottom <= 32767;
}

static int submit_control_window_with_owner(uint32_t request_id, int32_t x, int32_t y, uint32_t timeout_ms, int kind,
    const char* text, uint32_t length, uint64_t expected_owner_generation) {
    int is_posted = 0;
    DWORD process_id = 0;
    fang_control_snapshot boundary;
    if (!request_id || !timeout_ms || timeout_ms > 2000) return 0;
    if (kind == 15) {
        uint32_t index;
        if (!text || !length || length > sizeof(control_text)) return 0;
        for (index = 0; index < length; index++) if ((unsigned char)text[index] < 32 || (unsigned char)text[index] > 126) return 0;
    }
    fang_control_read(&boundary);
    AcquireSRWLockExclusive(&window_lock);
    if (!is_executing && (!control_request || control_receipt.status != 0) && control_message &&
        ((kind != 0 && kind != 16) ? (control_window && IsWindow(control_window) &&
            GetWindowThreadProcessId(control_window, &process_id) != 0 && process_id == GetCurrentProcessId()) :
            control_click_window(control_window, x, y))) {
        control_request = request_id;
        control_x = x;
        control_y = y;
        control_kind = kind;
        control_screen_owner_generation = expected_owner_generation;
        is_screen_canceled = 0;
        is_editor_canceled = 0;
        control_scene = boundary.scene_load_return_count;
        control_deadline = GetTickCount64() + timeout_ms;
        memset(control_text, 0, sizeof(control_text));
        control_text_length = kind == 15 ? length : 0;
        is_text_canceled = 0;
        if (control_text_length) memcpy(control_text, text, control_text_length);
        memset(&control_receipt, 0, sizeof(control_receipt));
        memset(&control_map_state, 0, sizeof(control_map_state));
        memset(&control_deployment_state, 0, sizeof(control_deployment_state));
        memset(&control_arena_state, 0, sizeof(control_arena_state));
        memset(&control_beam_state, 0, sizeof(control_beam_state));
        memset(&control_screen_state, 0, sizeof(control_screen_state));
        control_receipt.request_id = request_id;
        is_posted = PostMessageA(control_window, control_message, request_id, 0) != 0;
        if (!is_posted) {
            control_receipt.status = 2;
            memset(control_text, 0, sizeof(control_text));
            control_text_length = 0;
        }
    }
    ReleaseSRWLockExclusive(&window_lock);
    return is_posted;
}

static int submit_control_window(uint32_t request_id, int32_t x, int32_t y, uint32_t timeout_ms, int kind,
    const char* text, uint32_t length) {
    return submit_control_window_with_owner(request_id, x, y, timeout_ms, kind, text, length, 0);
}

int fang_control_window_submit(uint32_t request_id, int32_t x, int32_t y, uint32_t timeout_ms) {
    return submit_control_window(request_id, x, y, timeout_ms, 0, NULL, 0);
}

int fang_control_window_right_click(uint32_t request_id, int32_t x, int32_t y, uint32_t timeout_ms) {
    return submit_control_window(request_id, x, y, timeout_ms, 16, NULL, 0);
}

int fang_control_window_ready(uint32_t request_id, uint32_t timeout_ms) {
    return submit_control_window(request_id, 0, 0, timeout_ms, 1, NULL, 0);
}

int fang_control_window_screen(uint32_t request_id, uint32_t screen, int is_action, uint64_t expected_owner_generation, uint32_t timeout_ms) {
    if ((screen > 5 && screen != 7) || (is_action && !expected_owner_generation)) return 0;
    return submit_control_window_with_owner(request_id, (int32_t)screen, 0, timeout_ms,
        is_action ? 18 : 17, NULL, 0, expected_owner_generation);
}

int fang_control_window_screen_result(uint32_t request_id, fang_control_screen_state* snapshot) {
    int is_found;
    if (!snapshot) return 0;
    AcquireSRWLockShared(&window_lock);
    is_found = request_id && request_id == control_request &&
        (control_kind == 17 || control_kind == 18) && control_receipt.status != 0;
    if (is_found) *snapshot = control_screen_state;
    ReleaseSRWLockShared(&window_lock);
    return is_found;
}

int fang_control_window_screen_current(uint32_t request_id) {
    int is_current;
    AcquireSRWLockShared(&window_lock);
    is_current = request_id && request_id == control_request &&
        (control_kind == 17 || control_kind == 18) && !is_screen_canceled && control_receipt.status == 0;
    ReleaseSRWLockShared(&window_lock);
    return is_current;
}

int fang_control_window_scene(uint32_t request_id, uint32_t room, uint32_t timeout_ms) {
    if (room < 1 || room > 3) return 0;
    return submit_control_window(request_id, (int32_t)room, 0, timeout_ms, 2, NULL, 0);
}

int fang_control_window_scene_ready(uint32_t request_id, uint32_t room, uint32_t timeout_ms) {
    if (room < 1 || room > 3) return 0;
    return submit_control_window(request_id, (int32_t)room, 0, timeout_ms, 3, NULL, 0);
}

int fang_control_window_press(uint32_t request_id, uint32_t key, uint32_t timeout_ms) {
    if (key != VK_ESCAPE && key != VK_RETURN) return 0;
    return submit_control_window(request_id, (int32_t)key, 0, timeout_ms, 4, NULL, 0);
}

int fang_control_window_text(uint32_t request_id, const char* text, uint32_t length, uint32_t timeout_ms) {
    return submit_control_window(request_id, 0, 0, timeout_ms, 15, text, length);
}

int fang_control_window_map(uint32_t request_id, uint32_t timeout_ms) {
    return submit_control_window(request_id, 0, 0, timeout_ms, 5, NULL, 0);
}

int fang_control_window_editor_cancel(uint32_t request_id, uint32_t timeout_ms) {
    return submit_control_window(request_id, 0, 0, timeout_ms, 12, NULL, 0);
}

int fang_control_window_editor_hierarchy(uint32_t request_id, uint32_t timeout_ms) {
    return submit_control_window(request_id, 0, 0, timeout_ms, 21, NULL, 0);
}

int fang_control_window_editor_open(uint32_t request_id, uint32_t hero_noun, uint32_t timeout_ms) {
    if (!hero_noun) return 0;
    return submit_control_window(request_id, (int32_t)hero_noun, 0, timeout_ms, 22, NULL, 0);
}

int fang_control_window_editor_current(uint32_t request_id) {
    int is_current;
    AcquireSRWLockShared(&window_lock);
    is_current = request_id == control_request && control_kind == 21 && !is_editor_canceled && !control_receipt.status;
    ReleaseSRWLockShared(&window_lock);
    return is_current;
}

int fang_control_window_editor_tree_result(uint32_t request_id, fang_control_editor_tree* snapshot) {
    int is_found;
    if (!snapshot) return 0;
    AcquireSRWLockShared(&window_lock);
    is_found = request_id && request_id == control_request && control_kind == 21 && control_receipt.status != 0;
    if (is_found) *snapshot = control_editor_tree;
    ReleaseSRWLockShared(&window_lock);
    return is_found;
}

/* Fixed allowlist: 6 authored Map Start, 7 deployment observation, 8 authored
 * deployment Continue, 9 Campaign, 10 Previous, 11 Next. No caller-selected
 * GFx name or arguments are accepted. */
int fang_control_window_mission(uint32_t request_id, uint32_t kind, uint32_t timeout_ms) {
    if ((kind < 6 || kind > 11) && kind != 13 && kind != 14 && kind != 19 && kind != 20) return 0;
    return submit_control_window(request_id, 0, 0, timeout_ms, (int)kind, NULL, 0);
}

int fang_control_window_beam_result(uint32_t request_id, fang_control_beam_state* snapshot) {
    int is_found;
    if (!snapshot) return 0;
    AcquireSRWLockShared(&window_lock);
    is_found = request_id && control_request == request_id && (control_kind == 13 || control_kind == 14) && control_receipt.status != 0;
    if (is_found) *snapshot = control_beam_state;
    ReleaseSRWLockShared(&window_lock);
    return is_found;
}

int fang_control_window_map_result(uint32_t request_id, fang_control_map_state* snapshot) {
    int is_found;
    if (!snapshot) return 0;
    AcquireSRWLockShared(&window_lock);
    is_found = request_id && control_request == request_id &&
        (control_kind == 5 || control_kind == 6 || (control_kind >= 9 && control_kind <= 11)) && control_receipt.status != 0;
    if (is_found) *snapshot = control_map_state;
    ReleaseSRWLockShared(&window_lock);
    return is_found;
}

int fang_control_window_arena_result(uint32_t request_id, fang_control_arena_state* snapshot) {
    int is_found;
    if (!snapshot) return 0;
    AcquireSRWLockShared(&window_lock);
    is_found = request_id && control_request == request_id && (control_kind == 19 || control_kind == 20) && control_receipt.status != 0;
    if (is_found) *snapshot = control_arena_state;
    ReleaseSRWLockShared(&window_lock);
    return is_found;
}

int fang_control_window_deployment_result(uint32_t request_id, fang_control_deployment_state* snapshot) {
    int is_found;
    if (!snapshot) return 0;
    AcquireSRWLockShared(&window_lock);
    is_found = request_id && control_request == request_id && (control_kind == 7 || control_kind == 8) && control_receipt.status != 0;
    if (is_found) *snapshot = control_deployment_state;
    ReleaseSRWLockShared(&window_lock);
    return is_found;
}

static uint32_t process_control_key(HWND window, WNDPROC original, uint32_t key) {
    LPARAM scan;
    if (!original || (key != VK_ESCAPE && key != VK_RETURN) ||
        (GetAsyncKeyState((int)key) & 0x8000) || (GetAsyncKeyState(VK_SHIFT) & 0x8000) ||
        (GetAsyncKeyState(VK_CONTROL) & 0x8000) || (GetAsyncKeyState(VK_MENU) & 0x8000) ||
        (GetAsyncKeyState(VK_LWIN) & 0x8000) || (GetAsyncKeyState(VK_RWIN) & 0x8000)) return 2;
    scan = (LPARAM)(MapVirtualKeyA(key, MAPVK_VK_TO_VSC) << 16) | 1;
    CallWindowProcA(original, window, WM_KEYDOWN, (WPARAM)key, scan);
    CallWindowProcA(original, window, WM_KEYUP, (WPARAM)key, scan | (LPARAM)0xC0000000u);
    return 1;
}

int fang_control_window_result(uint32_t request_id, fang_control_window_receipt* receipt) {
    int is_found;
    if (!receipt) return 0;
    AcquireSRWLockShared(&window_lock);
    is_found = request_id && control_request == request_id;
    if (is_found) *receipt = control_receipt;
    ReleaseSRWLockShared(&window_lock);
    return is_found;
}

void fang_control_window_cancel(uint32_t request_id) {
    AcquireSRWLockExclusive(&window_lock);
    if (control_request == request_id && (control_kind == 17 || control_kind == 18) && control_receipt.status == 0)
        is_screen_canceled = 1;
    if (control_request == request_id && control_kind == 21) is_editor_canceled = 1;
    if (control_request == request_id && control_kind == 15 && control_receipt.status == 0) {
        is_text_canceled = 1;
        if (is_executing) {
            /* A character may currently be inside the native WndProc. The
             * count records returned calls only; its outcome is unknown. */
            control_receipt.status = 6;
            control_receipt.completed_tick_ms = GetTickCount64();
        } else {
            memset(control_text, 0, sizeof(control_text));
            control_text_length = 0;
        }
    }
    if (control_request == request_id && !is_executing && control_receipt.status == 0) {
        control_receipt.status = 3;
        control_receipt.completed_tick_ms = GetTickCount64();
    }
    ReleaseSRWLockExclusive(&window_lock);
}

int fang_control_window_message(HWND window, UINT message, WPARAM wparam, LPARAM lparam, WNDPROC original) {
    DWORD process_id = 0;
    LPARAM point;
    uint32_t request_id;
    int is_right_click;
    fang_control_snapshot boundary;
    (void)lparam;
    AcquireSRWLockExclusive(&window_lock);
    if (!control_message || message != control_message) {
        ReleaseSRWLockExclusive(&window_lock);
        return 0;
    }
    request_id = (uint32_t)wparam;
    if (!request_id || request_id != control_request || control_receipt.status || is_executing) {
        ReleaseSRWLockExclusive(&window_lock);
        return 1;
    }
    control_receipt.thread_id = GetCurrentThreadId();
    is_right_click = control_kind == 16;
    fang_control_read(&boundary);
    if (GetTickCount64() >= control_deadline) control_receipt.status = 4;
    else if (window != control_window || !IsWindow(window) ||
        GetWindowThreadProcessId(window, &process_id) != GetCurrentThreadId() || process_id != GetCurrentProcessId() ||
        (((control_kind >= 2 && control_kind <= 4) || control_kind == 6 || control_kind >= 8) &&
            (!IsWindowVisible(window) || !IsWindowEnabled(window) || IsIconic(window))) ||
        ((control_kind == 0 || is_right_click) && (!original || !control_click_window(window, control_x, control_y) ||
            boundary.scene_load_return_count != control_scene ||
            (GetAsyncKeyState(is_right_click ? VK_RBUTTON : VK_LBUTTON) & 0x8000) != 0))) control_receipt.status = 2;
    if (control_receipt.status) {
        if (control_kind == 15) {
            SecureZeroMemory(control_text, sizeof(control_text));
            control_text_length = 0;
        }
        control_receipt.completed_tick_ms = GetTickCount64();
        ReleaseSRWLockExclusive(&window_lock);
        return 1;
    }
    /* Preserve the current owned-window subclass chain, including Mod's Body
     * tab receiver installed above hooked_game_wndproc. Synthetic messages
     * re-enter with a different message ID and cannot resubmit this command. */
    {
        WNDPROC installed = (WNDPROC)GetWindowLongPtrW(window, GWLP_WNDPROC);
        if (installed) original = installed;
    }
    if (control_kind == 22) {
        uint32_t status;
        uint32_t hero_noun = (uint32_t)control_x;
        is_executing = 1;
        ReleaseSRWLockExclusive(&window_lock);
        status = fang_control_scene_ready(1);
        if (status == 1) status = fang_mod_editor_open_owned(hero_noun);
        AcquireSRWLockExclusive(&window_lock);
        control_receipt.status = status;
        control_receipt.completed_tick_ms = GetTickCount64();
        is_executing = 0;
        ReleaseSRWLockExclusive(&window_lock);
        return 1;
    }
    if (control_kind == 21) {
        uint32_t scene = control_scene;
        uint64_t deadline = control_deadline;
        uint32_t status;
        is_executing = 1;
        ReleaseSRWLockExclusive(&window_lock);
        status = fang_control_editor_hierarchy(request_id, scene, deadline, &control_editor_tree);
        AcquireSRWLockExclusive(&window_lock);
        control_receipt.status = status;
        control_receipt.completed_tick_ms = GetTickCount64();
        is_executing = 0;
        ReleaseSRWLockExclusive(&window_lock);
        return 1;
    }
    if (control_kind == 17 || control_kind == 18) {
        uint32_t screen = (uint32_t)control_x;
        uint32_t scene = control_scene;
        uint64_t deadline = control_deadline;
        uint64_t expected_owner_generation = control_screen_owner_generation;
        int is_action = control_kind == 18;
        uint32_t status;
        fang_control_screen_state snapshot;
        is_executing = 1;
        ReleaseSRWLockExclusive(&window_lock);
        status = fang_control_screen(request_id, screen, is_action, expected_owner_generation, scene, deadline, &snapshot);
        AcquireSRWLockExclusive(&window_lock);
        control_screen_state = snapshot;
        control_receipt.status = status;
        control_receipt.completed_tick_ms = GetTickCount64();
        is_executing = 0;
        ReleaseSRWLockExclusive(&window_lock);
        return 1;
    }
    if (control_kind == 1) {
        /* Responsiveness only: the real window owner consumed this probe.
         * Do not call the game WndProc or synthesize any input. */
        control_receipt.status = 1;
        control_receipt.completed_tick_ms = GetTickCount64();
        ReleaseSRWLockExclusive(&window_lock);
        return 1;
    }
    if (control_kind == 2 || control_kind == 3) {
        uint32_t room = (uint32_t)control_x;
        uint32_t status;
        int is_probe = control_kind == 3;
        is_executing = 1;
        ReleaseSRWLockExclusive(&window_lock);
        status = is_probe ? fang_control_scene_ready(room) : fang_control_scene_request(room);
        AcquireSRWLockExclusive(&window_lock);
        control_receipt.status = status;
        control_receipt.completed_tick_ms = GetTickCount64();
        is_executing = 0;
        ReleaseSRWLockExclusive(&window_lock);
        return 1;
    }
    if (control_kind == 4) {
        uint32_t key = (uint32_t)control_x;
        uint32_t status;
        is_executing = 1;
        ReleaseSRWLockExclusive(&window_lock);
        status = process_control_key(window, original, key);
        AcquireSRWLockExclusive(&window_lock);
        control_receipt.status = status;
        control_receipt.completed_tick_ms = GetTickCount64();
        is_executing = 0;
        ReleaseSRWLockExclusive(&window_lock);
        return 1;
    }
    if (control_kind == 15) {
        char text[256];
        uint32_t length = control_text_length;
        uint32_t scene = control_scene;
        uint64_t deadline = control_deadline;
        uint32_t count = 0;
        uint32_t status = 1;
        memcpy(text, control_text, sizeof(text));
        is_executing = 1;
        ReleaseSRWLockExclusive(&window_lock);
        while (count < length) {
            fang_control_snapshot boundary;
            int is_canceled;
            AcquireSRWLockShared(&window_lock);
            is_canceled = is_text_canceled || control_request != request_id;
            ReleaseSRWLockShared(&window_lock);
            fang_control_read(&boundary);
            if (is_canceled) status = count ? 6 : 3;
            else if (GetTickCount64() >= deadline) status = count ? 6 : 4;
            else if (!original || boundary.scene_load_return_count != scene ||
                GetWindowThreadProcessId(window, &process_id) != GetCurrentThreadId() || process_id != GetCurrentProcessId() ||
                !IsWindowVisible(window) || !IsWindowEnabled(window) || IsIconic(window) ||
                (GetAsyncKeyState(VK_SHIFT) & 0x8000) || (GetAsyncKeyState(VK_CONTROL) & 0x8000) ||
                (GetAsyncKeyState(VK_MENU) & 0x8000) || (GetAsyncKeyState(VK_LWIN) & 0x8000) ||
                (GetAsyncKeyState(VK_RWIN) & 0x8000)) status = count ? 6 : 5;
            if (status != 1) break;
            /* Processing receipt only. Native textbox/chat focus and delivery
             * remain ordinary game decisions; WM_CHAR never submits Enter. */
            CallWindowProcA(original, window, WM_CHAR, (WPARAM)(unsigned char)text[count], 1);
            count++;
            AcquireSRWLockExclusive(&window_lock);
            control_receipt.processed_character_count = count;
            ReleaseSRWLockExclusive(&window_lock);
        }
        SecureZeroMemory(text, sizeof(text));
        AcquireSRWLockExclusive(&window_lock);
        SecureZeroMemory(control_text, sizeof(control_text));
        control_text_length = 0;
        control_receipt.status = status;
        control_receipt.processed_character_count = count;
        control_receipt.completed_tick_ms = GetTickCount64();
        is_executing = 0;
        ReleaseSRWLockExclusive(&window_lock);
        return 1;
    }
    if (control_kind == 5 || control_kind == 6 || (control_kind >= 9 && control_kind <= 11)) {
        fang_control_map_state snapshot;
        uint32_t status;
        int kind = control_kind;
        memset(&snapshot, 0, sizeof(snapshot));
        is_executing = 1;
        ReleaseSRWLockExclusive(&window_lock);
        if (kind >= 9) status = fang_control_map_select((uint32_t)kind, &snapshot);
        else status = kind == 6 ? fang_control_map_start(&snapshot) : (fang_control_map_read(&snapshot) ? 1 : 5);
        AcquireSRWLockExclusive(&window_lock);
        control_map_state = snapshot;
        control_receipt.status = status;
        control_receipt.completed_tick_ms = GetTickCount64();
        is_executing = 0;
        ReleaseSRWLockExclusive(&window_lock);
        return 1;
    }
    if (control_kind == 7 || control_kind == 8) {
        fang_control_deployment_state snapshot;
        uint32_t status;
        int is_action = control_kind == 8;
        memset(&snapshot, 0, sizeof(snapshot));
        is_executing = 1;
        ReleaseSRWLockExclusive(&window_lock);
        status = is_action ? fang_control_deployment_continue(&snapshot) : (fang_control_deployment_read(&snapshot) ? 1 : 5);
        AcquireSRWLockExclusive(&window_lock);
        control_deployment_state = snapshot;
        control_receipt.status = status;
        control_receipt.completed_tick_ms = GetTickCount64();
        is_executing = 0;
        ReleaseSRWLockExclusive(&window_lock);
        return 1;
    }
    if (control_kind == 19 || control_kind == 20) {
        fang_control_arena_state snapshot;
        uint32_t status;
        int is_action = control_kind == 20;
        memset(&snapshot, 0, sizeof(snapshot));
        is_executing = 1;
        ReleaseSRWLockExclusive(&window_lock);
        status = is_action ? fang_control_arena_accept(&snapshot) : (fang_control_arena_read(&snapshot) ? 1 : 5);
        AcquireSRWLockExclusive(&window_lock);
        control_arena_state = snapshot;
        control_receipt.status = status;
        control_receipt.completed_tick_ms = GetTickCount64();
        is_executing = 0;
        ReleaseSRWLockExclusive(&window_lock);
        return 1;
    }
    if (control_kind == 12) {
        uint32_t status;
        is_executing = 1;
        ReleaseSRWLockExclusive(&window_lock);
        status = fang_control_editor_cancel(window, original);
        AcquireSRWLockExclusive(&window_lock);
        control_receipt.status = status;
        control_receipt.completed_tick_ms = GetTickCount64();
        is_executing = 0;
        ReleaseSRWLockExclusive(&window_lock);
        return 1;
    }
    if (control_kind == 13 || control_kind == 14) {
        fang_control_beam_state snapshot;
        uint32_t status;
        uint32_t scene = control_scene;
        uint64_t deadline = control_deadline;
        int is_action = control_kind == 13;
        is_executing = 1;
        ReleaseSRWLockExclusive(&window_lock);
        status = fang_control_beam_out(scene, deadline, is_action, &snapshot);
        AcquireSRWLockExclusive(&window_lock);
        control_beam_state = snapshot;
        control_receipt.status = status;
        control_receipt.completed_tick_ms = GetTickCount64();
        is_executing = 0;
        ReleaseSRWLockExclusive(&window_lock);
        return 1;
    }
    point = MAKELPARAM((WORD)control_x, (WORD)control_y);
    is_executing = 1;
    ReleaseSRWLockExclusive(&window_lock);
    /* Always pair release within this finite callback. No desktop cursor or
     * focus API, direct menu/gameplay method, or packaged resource is changed. */
    CallWindowProcA(original, window, WM_MOUSEMOVE, 0, point);
    CallWindowProcA(original, window, is_right_click ? WM_RBUTTONDOWN : WM_LBUTTONDOWN,
        is_right_click ? MK_RBUTTON : MK_LBUTTON, point);
    CallWindowProcA(original, window, is_right_click ? WM_RBUTTONUP : WM_LBUTTONUP, 0, point);
    AcquireSRWLockExclusive(&window_lock);
    control_receipt.status = 1;
    control_receipt.completed_tick_ms = GetTickCount64();
    is_executing = 0;
    ReleaseSRWLockExclusive(&window_lock);
    return 1;
}
#endif

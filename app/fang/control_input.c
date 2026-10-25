#include "hook.h"
#include "control_input.h"
#if FANG_SCENARIO
#include <string.h>
#include <math.h>
#if !defined(__GNUC__)
#include <intrin.h>
#endif

/* Build103 provenance: 44FC65 -> camera ray7B2C60 -> Graphics.GetCursor
 * 88DC90 (lea eax,[ecx+92]; ret), before ordinary4502C0 mouse polling.
 * Only that ray scope receives a private copy of width/height/x/y. Original
 * native answers execute first; no global key array or desktop cursor changes.
 * The normal ray pick and movement/attack/interact decision remain untouched. */
typedef unsigned char (FANG_THISCALL* input_ray_fn)(void*, void*, void*);
typedef unsigned char (__cdecl* input_ground_fn)(float*, float*, uint32_t*, unsigned char*);
typedef int32_t* (FANG_THISCALL* input_cursor_fn)(void*);
typedef int (WINAPI* input_held_fn)(int);
typedef void* (FANG_THISCALL* input_root_fn)(void*);
typedef void* (FANG_THISCALL* input_modal_fn)(void*, int);
typedef BYTE* (__cdecl* input_player_fn)(void);
#if defined(__GNUC__)
#define INPUT_FASTCALL __attribute__((fastcall))
#define INPUT_CALLER() __builtin_return_address(0)
#else
#define INPUT_FASTCALL __fastcall
#define INPUT_CALLER() _ReturnAddress()
#endif

static SRWLOCK input_lock = SRWLOCK_INIT;
static volatile LONG is_input_installed;
static BYTE* input_image;
static input_ray_fn original_input_ray;
static input_ground_fn original_input_ground;
static input_cursor_fn original_input_cursor;
static input_held_fn original_input_held;
static uint32_t input_request, input_scene, input_handle;
static DWORD input_thread;
static HWND input_window;
static uint64_t input_deadline;
static uint64_t input_release_at;
static uint32_t input_hold_ms;
static int is_release_poll_ready;
static int is_held_move_observed;
/* 0 armed, 1 press consumed, 2 held until release_at (tap: next poll),
 * 3 release poll passed, 4 settled. Cancellation removes all synthetic
 * answers immediately; an unwitnessed release remains outcome_unknown. */
static uint32_t input_phase, input_failure;
static int is_input_armed, is_cursor_scope, is_cursor_observed, is_ray_observed;
static int32_t input_x, input_y, input_cursor[4];
static fang_control_gameplay_click input_snapshot;
static fang_control_gameplay_click ground_snapshot;
static uint32_t ground_request;
static int is_aim_only;

static int is_input_identity_current(void) {
    fang_control_snapshot boundary;
    const BYTE* root;
    const BYTE* player;
    DWORD process_id = 0;
    if (input_thread != GetCurrentThreadId()) return 0;
    if (!input_window || !IsWindow(input_window) ||
        GetWindowThreadProcessId(input_window, &process_id) != input_thread || process_id != GetCurrentProcessId() ||
        !IsWindowVisible(input_window) || !IsWindowEnabled(input_window) || IsIconic(input_window)) return 0;
    fang_control_read(&boundary);
    if (boundary.scene_load_return_count != input_scene) return 0;
    root = *(const BYTE**)(input_image + 0x1038CB0);
    if (!readable_range(root, 48) || !readable_range(*(void**)(root + 4), 33500) ||
        (!*(uint32_t*)(root + 40) && !readable_range(*(void**)(input_image + 0x1038CB4), 40))) return 0;
    player = ((input_player_fn)(input_image + 0xE57B0))();
    return readable_range(player, 4668) && *(const uint32_t*)(player + 4664) == input_handle;
}

static int is_native_gameplay_modal_clear(void) {
    BYTE* manager = *(BYTE**)(input_image + 0x105EDCC);
    void* root;
    void* modal;
    if (!readable_range(manager, 4) || !readable_range(*(void**)manager, 76)) return 0;
    /* Exact non-foreground half of native50B1A0. Keep Options/chat/modal
     * precedence; only the owned debug operation substitutes device edges. */
    modal = ((input_modal_fn)(*(void***)manager)[18])(manager, 0);
    root = ((input_root_fn)(*(void***)manager)[1])(manager);
    return root && modal == root;
}

static int is_physical_input_clear(void) {
    return !GetCapture() && !(GetAsyncKeyState(VK_LBUTTON) & 0x8000) &&
        !(GetAsyncKeyState(VK_RBUTTON) & 0x8000) && !(GetAsyncKeyState(VK_SHIFT) & 0x8000) &&
        !(GetAsyncKeyState(VK_CONTROL) & 0x8000) && !(GetAsyncKeyState(VK_MENU) & 0x8000) &&
        !(GetAsyncKeyState(VK_LWIN) & 0x8000) && !(GetAsyncKeyState(VK_RWIN) & 0x8000);
}

static int32_t* INPUT_FASTCALL scoped_input_cursor(void* owner, void* ignored) {
    int32_t* cursor = original_input_cursor(owner);
    int32_t* result = cursor;
    DWORD last_error = GetLastError();
    (void)ignored;
    AcquireSRWLockExclusive(&input_lock);
    if (is_input_armed && is_cursor_scope && input_thread == GetCurrentThreadId() &&
        readable_range(cursor, sizeof(input_cursor))) {
        if (cursor[0] > 0 && cursor[1] > 0 && input_x >= 0 && input_y >= 0 && input_x < cursor[0] && input_y < cursor[1]) {
            memcpy(input_cursor, cursor, sizeof(input_cursor));
            input_cursor[2] = input_x;
            input_cursor[3] = input_y;
            result = input_cursor;
            is_cursor_observed = 1;
        } else {
            is_input_armed = 0;
            input_failure = 5;
        }
    }
    ReleaseSRWLockExclusive(&input_lock);
    SetLastError(last_error);
    return result;
}

static unsigned char INPUT_FASTCALL scoped_input_ray(void* camera, void* ignored, void* near_point, void* far_point) {
    unsigned char result;
    DWORD last_error = GetLastError();
    (void)ignored;
    AcquireSRWLockExclusive(&input_lock);
    if (is_input_armed && input_thread == GetCurrentThreadId() && !is_input_identity_current()) {
        is_input_armed = 0;
        input_failure = 5;
    }
    is_cursor_scope = is_input_armed && input_thread == GetCurrentThreadId() &&
        GetTickCount64() < input_deadline && input_phase < 3;
    is_cursor_observed = 0;
    is_ray_observed = 0;
    ReleaseSRWLockExclusive(&input_lock);
    SetLastError(last_error);
    result = original_input_ray(camera, near_point, far_point);
    last_error = GetLastError();
    AcquireSRWLockExclusive(&input_lock);
    if (is_cursor_scope && is_input_armed && result && is_cursor_observed) is_ray_observed = 1;
    is_cursor_scope = 0;
    ReleaseSRWLockExclusive(&input_lock);
    SetLastError(last_error);
    return result;
}

/* Build103 caller450459 passes two independent XYZ outputs to44FC30:
 * raw ray/terrain point, then native adjusted movement point. The query's
 * return is not movement acceptance. Outputs are copied only on success,
 * after our exact ray was observed, and published only by a consumed press. */
static unsigned char __cdecl scoped_input_ground(float* ray_point, float* ground_point,
    uint32_t* picked_handle, unsigned char* pick_flag) {
    uint32_t request = 0;
    unsigned char result;
    DWORD last_error = GetLastError();
    AcquireSRWLockExclusive(&input_lock);
    if (is_input_armed && input_thread == GetCurrentThreadId() && input_phase == 0) {
        request = input_request;
        ground_request = 0;
        memset(&ground_snapshot, 0, sizeof(ground_snapshot));
    }
    ReleaseSRWLockExclusive(&input_lock);
    SetLastError(last_error);
    result = original_input_ground(ray_point, ground_point, picked_handle, pick_flag);
    last_error = GetLastError();
    AcquireSRWLockExclusive(&input_lock);
    if (request && is_input_armed && input_request == request && input_thread == GetCurrentThreadId() &&
        input_phase == 0 && is_ray_observed && GetTickCount64() < input_deadline && is_input_identity_current()) {
        ground_request = request;
        ground_snapshot.is_native_ground_query_success = result != 0;
        if (result && readable_range(ray_point, 3 * sizeof(float)) &&
            isfinite(ray_point[0]) && isfinite(ray_point[1]) && isfinite(ray_point[2])) {
            ground_snapshot.ray_x = ray_point[0]; ground_snapshot.ray_y = ray_point[1]; ground_snapshot.ray_z = ray_point[2];
            ground_snapshot.fields |= 2;
        }
        if (result && readable_range(ground_point, 3 * sizeof(float)) &&
            isfinite(ground_point[0]) && isfinite(ground_point[1]) && isfinite(ground_point[2])) {
            ground_snapshot.ground_x = ground_point[0]; ground_snapshot.ground_y = ground_point[1]; ground_snapshot.ground_z = ground_point[2];
            ground_snapshot.fields |= 4;
        }
    }
    ReleaseSRWLockExclusive(&input_lock);
    SetLastError(last_error);
    return result;
}

int fang_control_input_press(void* caller, int key, int is_pressed) {
    int result = is_pressed;
    if (!InterlockedCompareExchange(&is_input_installed, 0, 0) ||
        caller != input_image + 0x50323 || key != 1000) return result;
    AcquireSRWLockExclusive(&input_lock);
    if (!is_aim_only && is_input_armed && input_thread == GetCurrentThreadId() && input_phase == 0 && is_ray_observed) {
        if (is_pressed || GetTickCount64() >= input_deadline || !is_input_identity_current() ||
            !is_physical_input_clear() || !is_native_gameplay_modal_clear()) {
            is_input_armed = 0;
            input_failure = 5;
        } else {
            result = 1;
            input_phase = 1;
            input_release_at = GetTickCount64() + input_hold_ms;
            input_snapshot.is_press_poll_observed = 1;
            if (ground_request == input_request) {
                input_snapshot.is_native_ground_query_success = ground_snapshot.is_native_ground_query_success;
                input_snapshot.fields |= ground_snapshot.fields & 6;
                input_snapshot.ray_x = ground_snapshot.ray_x; input_snapshot.ray_y = ground_snapshot.ray_y; input_snapshot.ray_z = ground_snapshot.ray_z;
                input_snapshot.ground_x = ground_snapshot.ground_x; input_snapshot.ground_y = ground_snapshot.ground_y; input_snapshot.ground_z = ground_snapshot.ground_z;
            }
            trace_client_state("scenario_gameplay_press_poll", input_request);
        }
    }
    ReleaseSRWLockExclusive(&input_lock);
    return result;
}

static int WINAPI scoped_input_held(int key) {
    void* caller = INPUT_CALLER();
    int result = original_input_held(key);
    DWORD last_error = GetLastError();
    AcquireSRWLockExclusive(&input_lock);
    if (is_input_armed && input_thread == GetCurrentThreadId()) {
        if (!is_input_identity_current() || !is_physical_input_clear() || !is_native_gameplay_modal_clear()) {
            is_input_armed = 0;
            input_failure = 5;
        }
    }
    if (is_input_armed && input_thread == GetCurrentThreadId()) {
        if (caller == input_image + 0x5037B && key == 1000) {
            if (input_phase == 1 && GetTickCount64() < input_deadline) result = 1;
            else if (input_phase == 2) {
                if (GetTickCount64() < input_release_at && GetTickCount64() < input_deadline) result = 1;
                else { result = 0; is_release_poll_ready = 1; }
            }
        } else if (caller == input_image + 0x4D686 && key == 1000 &&
            (input_phase == 1 || input_phase == 2)) {
            /* Build10344D600 checks the held button again before promoting
             * the initial movement press to continuous movement after 500ms.
             * Answering only45037B delays release but never exercises that
             * ordinary held-movement branch. Keep this scoped to the same
             * finite request and let the original producer update its state. */
            result = GetTickCount64() < input_release_at && GetTickCount64() < input_deadline;
        } else if (caller == input_image + 0x503AF && key == 1002) {
            if (input_phase == 1) {
                /* The ordinary press branch has now run. Sample its retained
                 * target before native44D2E0 clears it on the release poll. */
                input_snapshot.retained_handle = *(uint32_t*)(input_image + 0xDBEF10);
                input_snapshot.fields |= 1;
                input_phase = 2;
            }
            else if (input_phase == 2 && is_release_poll_ready) input_phase = 3;
        }
    }
    ReleaseSRWLockExclusive(&input_lock);
    SetLastError(last_error);
    return result;
}

int fang_control_input_install(HMODULE executable) {
    static const BYTE ray_call[] = {0xE8,0xF6,0x2F,0x36,0x00};
    static const BYTE ground_call[] = {0xE8,0xD2,0xF7,0xFF,0xFF};
    static const BYTE ground_entry[] = {0x8B,0x44,0x24,0x0C,0x83,0xEC,0x54,0x55,0x8B,0x6C,0x24,0x60};
    static const BYTE cursor_entry[] = {0x8D,0x41,0x5C,0xC3};
    static const BYTE held_entry[] = {0x8B,0x44,0x24,0x04,0x50,0xE8,0x96,0x9E,0x5F,0x00};
    static const BYTE ray_entry[] = {0x56,0x8B,0xF1,0xE8,0x88,0xBF,0xFF,0xFF,0x8B,0x10,0x8B,0xC8,0x8B,0x42,0x1C,0xFF,0xD0};
    static const BYTE ray_return[] = {0xC2,0x08,0x00};
    static const BYTE press_call[] = {0x8B,0x16,0x8B,0x42,0x3C,0x68,0xE8,0x03,0x00,0x00,0x8B,0xCE,0xFF,0xD0};
    static const BYTE held_left_call[] = {0x8B,0x16,0x8B,0x42,0x40,0x68,0xE8,0x03,0x00,0x00,0x8B,0xCE,0xC6,0x44,0x24,0x0E,0x00,0xFF,0xD0};
    static const BYTE held_right_call[] = {0x8B,0x16,0x8B,0x42,0x40,0x68,0xEA,0x03,0x00,0x00,0x8B,0xCE,0xC6,0x44,0x24,0x0F,0x00,0xFF,0xD0};
    static const BYTE held_move_call[] = {0x8B,0x10,0x8B,0xC8,0x8B,0x42,0x40,0x68,0xE8,0x03,0x00,0x00,0xFF,0xD0};
    BYTE* base = (BYTE*)executable;
    if (!base || InterlockedCompareExchange(&is_input_installed, 0, 0) ||
        memcmp(base + 0x4FC65, ray_call, sizeof(ray_call)) ||
        memcmp(base + 0x50459, ground_call, sizeof(ground_call)) ||
        memcmp(base + 0x4FC30, ground_entry, sizeof(ground_entry)) ||
        memcmp(base + 0x48DC90, cursor_entry, sizeof(cursor_entry)) ||
        memcmp(base + 0x10B3A0, held_entry, sizeof(held_entry)) ||
        memcmp(base + 0x3B2C60, ray_entry, sizeof(ray_entry)) ||
        memcmp(base + 0x3B2C9B, ray_return, sizeof(ray_return)) ||
        memcmp(base + 0x50315, press_call, sizeof(press_call)) ||
        memcmp(base + 0x50368, held_left_call, sizeof(held_left_call)) ||
        memcmp(base + 0x5039C, held_right_call, sizeof(held_right_call)) ||
        memcmp(base + 0x4D678, held_move_call, sizeof(held_move_call)) ||
        *(void**)(base + 0xC0F574) != base + 0x48DC90 ||
        *(void**)(base + 0xBDE478) != base + 0x10B3A0) return 0;
    input_image = base;
    original_input_ray = (input_ray_fn)(base + 0x3B2C60);
    original_input_ground = (input_ground_fn)(base + 0x4FC30);
    original_input_cursor = (input_cursor_fn)(base + 0x48DC90);
    original_input_held = (input_held_fn)(base + 0x10B3A0);
    if (!patch_pointer((void**)(base + 0xC0F574), (void*)original_input_cursor, (void*)scoped_input_cursor)) return 0;
    if (!patch_pointer((void**)(base + 0xBDE478), (void*)original_input_held, (void*)scoped_input_held)) goto rollback_cursor;
    if (!patch_call(base + 0x4FC65, (void*)original_input_ray, (void*)scoped_input_ray)) goto rollback_held;
    if (!patch_call(base + 0x50459, (void*)original_input_ground, (void*)scoped_input_ground)) goto rollback_ray;
    InterlockedExchange(&is_input_installed, 1);
    return 1;
rollback_ray:
    if (!patch_call(base + 0x4FC65, (void*)scoped_input_ray, (void*)original_input_ray))
        trace_client_state("scenario_gameplay_ray_rollback_failed", 1);
rollback_held:
    if (!patch_pointer((void**)(base + 0xBDE478), (void*)scoped_input_held, (void*)original_input_held))
        trace_client_state("scenario_gameplay_held_rollback_failed", 1);
rollback_cursor:
    if (!patch_pointer((void**)(base + 0xC0F574), (void*)scoped_input_cursor, (void*)original_input_cursor))
        trace_client_state("scenario_gameplay_cursor_rollback_failed", 1);
    return 0;
}

int fang_control_input_begin(uint32_t request_id, uint32_t scene, uint32_t handle, int32_t x, int32_t y, uint64_t deadline, uint32_t hold_ms) {
    fang_control_window_snapshot window;
    if (!InterlockedCompareExchange(&is_input_installed, 0, 0) || hold_ms > 1200 || GetTickCount64() >= deadline ||
        !is_physical_input_clear() || !is_native_gameplay_modal_clear() || !fang_control_window_inspect(&window) ||
        window.thread_id != GetCurrentThreadId() || !window.is_enabled || window.is_minimized) return 0;
    AcquireSRWLockExclusive(&input_lock);
    input_request = request_id; input_scene = scene; input_handle = handle;
    input_thread = GetCurrentThreadId(); input_deadline = deadline;
    input_hold_ms = hold_ms; input_release_at = 0; is_release_poll_ready = 0; is_held_move_observed = 0;
    is_aim_only = 0;
    input_window = (HWND)(uintptr_t)window.window_id;
    input_x = x; input_y = y;
    input_phase = 0; input_failure = 0; is_cursor_scope = 0; is_ray_observed = 0;
    memset(&input_snapshot, 0, sizeof(input_snapshot));
    ground_request = 0;
    memset(&ground_snapshot, 0, sizeof(ground_snapshot));
    input_snapshot.is_projected = 1;
    is_input_armed = 1;
    ReleaseSRWLockExclusive(&input_lock);
    return 1;
}

uint32_t fang_control_input_frame(uint32_t request_id, uint32_t scene, uint32_t handle, int is_world_active, fang_control_gameplay_click* snapshot) {
    uint32_t status = 0;
    AcquireSRWLockExclusive(&input_lock);
    if (request_id != input_request) { status = 5; goto done; }
    if (!is_held_move_observed && input_hold_ms && input_phase == 2 &&
        input_thread == GetCurrentThreadId() && *(BYTE*)(input_image + 0xDBEF3C)) {
        is_held_move_observed = 1;
        trace_client_state("scenario_move_hold_active", input_request);
    }
    if (!is_world_active || scene != input_scene || handle != input_handle || input_thread != GetCurrentThreadId()) {
        input_failure = 5; is_input_armed = 0;
    } else if (input_phase == 3) {
        input_snapshot.is_release_poll_observed = 1;
        input_phase = 4;
        is_input_armed = 0;
        trace_client_state("scenario_gameplay_release_poll", input_request);
        status = 1;
    } else if (GetTickCount64() >= input_deadline) {
        input_failure = 4; is_input_armed = 0;
    }
    if (!status && input_failure) status = input_snapshot.is_press_poll_observed ? 6 : input_failure;
    *snapshot = input_snapshot;
done:
    ReleaseSRWLockExclusive(&input_lock);
    return status;
}

void fang_control_input_cancel(uint32_t request_id) {
    AcquireSRWLockExclusive(&input_lock);
    if (input_request == request_id) {
        is_input_armed = 0;
        is_cursor_scope = 0;
        input_failure = 3;
    }
    ReleaseSRWLockExclusive(&input_lock);
}

int fang_control_input_aim_begin(uint32_t request_id, uint32_t scene, uint32_t handle, int32_t x, int32_t y, uint64_t deadline) {
    if (!fang_control_input_begin(request_id, scene, handle, x, y, deadline, 0)) return 0;
    AcquireSRWLockExclusive(&input_lock);
    is_aim_only = 1;
    ReleaseSRWLockExclusive(&input_lock);
    return 1;
}

int fang_control_input_aim_read(uint32_t request_id, fang_control_gameplay_click* snapshot) {
    int is_available = 0;
    if (!snapshot) return 0;
    AcquireSRWLockExclusive(&input_lock);
    if (is_aim_only && is_input_armed && input_request == request_id && ground_request == request_id &&
        is_ray_observed && GetTickCount64() < input_deadline && is_input_identity_current() &&
        is_physical_input_clear() && is_native_gameplay_modal_clear()) {
        *snapshot = ground_snapshot;
        is_available = 1;
    }
    ReleaseSRWLockExclusive(&input_lock);
    return is_available;
}
#endif

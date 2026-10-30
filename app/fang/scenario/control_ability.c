#include "hook.h"
#include "scenario/control_input.h"
#if FANG_SCENARIO
#include <string.h>

typedef int (__cdecl* ability_cast_fn)(uint32_t, const float*, const float*, uint32_t, uint32_t, uint32_t, uint32_t);
typedef BYTE* (__cdecl* ability_player_fn)(void);
typedef uint32_t (FANG_THISCALL* ability_identity_fn)(void*, uint32_t, int);

static SRWLOCK ability_lock = SRWLOCK_INIT;
static volatile LONG is_ability_installed;
static BYTE* ability_image;
static ability_cast_fn original_ability_cast;
static uint32_t ability_request, ability_scene, ability_handle, ability_phase, ability_failure;
static uint32_t ability_expected_id;
/* 0 armed, 1 edge emitted/caster unobserved, 2 original caster observed,
 * 3 terminal. Terminal requests never collect a later ordinary cast. */
static DWORD ability_thread;
static HWND ability_window;
static uint64_t ability_deadline;
static fang_control_active_cast ability_snapshot;

static int is_ability_owner_current(void) {
    fang_control_snapshot boundary;
    BYTE* root;
    BYTE* player;
    DWORD process_id = 0;
    if (GetCurrentThreadId() != ability_thread || !ability_window || !IsWindow(ability_window) ||
        GetWindowThreadProcessId(ability_window, &process_id) != ability_thread || process_id != GetCurrentProcessId() ||
        !IsWindowVisible(ability_window) || !IsWindowEnabled(ability_window) || IsIconic(ability_window)) return 0;
    fang_control_read(&boundary);
    if (boundary.scene_load_return_count != ability_scene || boundary.frame_thread_id != ability_thread ||
        !boundary.frame_tick_ms || GetTickCount64() - boundary.frame_tick_ms > 250) return 0;
    root = *(BYTE**)(ability_image + 0x1038CB0);
    if (!readable_range(root, 48) || !readable_range(*(void**)(root + 4), 33500) ||
        (!*(uint32_t*)(root + 40) && !readable_range(*(void**)(ability_image + 0x1038CB4), 40))) return 0;
    player = ((ability_player_fn)(ability_image + 0xE57B0))();
    return readable_range(player, 5000) && *(uint32_t*)(player + 4664) == ability_handle;
}

static int is_ability_physical_clear(void) {
    int key;
    if (GetCapture()) return 0;
    for (key = 1; key < 256; key++) if (GetAsyncKeyState(key) & 0x8000) return 0;
    return 1;
}

/* Build10344CF88 calls4DFBE0 with seven cdecl stack arguments and observes
 * EAX. This observer always executes the original caster with unchanged
 * arguments, including native target-validity. It creates no cast itself. */
static int __cdecl observe_ability_cast(uint32_t index, const float* start_point, const float* target_point,
    uint32_t target_handle, uint32_t flag, uint32_t mode, uint32_t is_target_valid) {
    int result = original_ability_cast(index, start_point, target_point, target_handle, flag, mode, is_target_valid);
    DWORD last_error = GetLastError();
    BYTE* player;
    AcquireSRWLockExclusive(&ability_lock);
    if (ability_request && ability_phase == 1 && !ability_failure && index == 2 &&
        GetCurrentThreadId() == ability_thread && GetTickCount64() < ability_deadline && is_ability_owner_current()) {
        ability_snapshot.is_native_request_attempted = 1;
        ability_snapshot.native_result_code = result;
        ability_snapshot.target_handle = target_handle;
        ability_snapshot.fields |= 5;
        if (is_ability_owner_current()) {
            player = ((ability_player_fn)(ability_image + 0xE57B0))();
            ability_snapshot.ability_id = ((ability_identity_fn)(ability_image + 0x5C25C0))(player, index, -1);
            ability_snapshot.fields |= 2;
            if (ability_snapshot.ability_id != ability_expected_id) ability_failure = 5;
        }
        ability_phase = 2;
        trace_client_state("scenario_active_cast_result", (unsigned int)result);
    }
    ReleaseSRWLockExclusive(&ability_lock);
    SetLastError(last_error);
    return result;
}

unsigned char fang_control_ability_edge(void* caller, uint32_t action, unsigned char is_pressed) {
    unsigned char result = is_pressed;
    fang_control_gameplay_click ground;
    BYTE* player;
    if (!InterlockedCompareExchange(&is_ability_installed, 0, 0) ||
        caller != ability_image + 0x4D537 || action != 0x10000011) return result;
    AcquireSRWLockExclusive(&ability_lock);
    if (!ability_request || ability_phase || ability_failure || GetCurrentThreadId() != ability_thread) goto done;
    if (is_pressed || GetTickCount64() >= ability_deadline || !is_ability_owner_current() || !is_ability_physical_clear() ||
        !fang_control_input_aim_read(ability_request, &ground) || !ground.is_native_ground_query_success ||
        (ground.fields & 6) != 6) {
        ability_failure = 5;
        goto done;
    }
    player = ((ability_player_fn)(ability_image + 0xE57B0))();
    if (((ability_identity_fn)(ability_image + 0x5C25C0))(player, 2, -1) != ability_expected_id) {
        ability_failure = 5;
        goto done;
    }
    ability_snapshot.ground = ground;
    ability_snapshot.is_invocation_attempted = 1;
    ability_phase = 1;
    result = 1;
    trace_client_state("scenario_active_cast_edge", ability_request);
done:
    ReleaseSRWLockExclusive(&ability_lock);
    return result;
}

int fang_control_ability_install(HMODULE executable) {
    static const BYTE edge[] = {0x8B,0x4C,0x24,0x10,0x8B,0x11,0x8B,0x42,0x2C,0x68,0x11,0x00,0x00,0x10,0xFF,0xD0};
    static const BYTE caster_call[] = {0xE8,0x53,0x2C,0x09,0x00};
    static const BYTE caster_entry[] = {0x6A,0xFF,0x68,0x88,0xC1,0xE9,0x00,0x64,0xA1,0x00,0x00,0x00,0x00};
    static const BYTE identity_entry[] = {0x64,0xA1,0x00,0x00,0x00,0x00,0x6A,0xFF,0x68,0xA0,0xF8,0xE9,0x00};
    BYTE* base = (BYTE*)executable;
    if (!base || InterlockedCompareExchange(&is_ability_installed, 0, 0) ||
        memcmp(base + 0x4D527, edge, sizeof(edge)) || memcmp(base + 0x4CF88, caster_call, sizeof(caster_call)) ||
        memcmp(base + 0xDFBE0, caster_entry, sizeof(caster_entry)) ||
        memcmp(base + 0x5C25C0, identity_entry, sizeof(identity_entry))) return 0;
    ability_image = base;
    original_ability_cast = (ability_cast_fn)(base + 0xDFBE0);
    if (!patch_call(base + 0x4CF88, (void*)original_ability_cast, (void*)observe_ability_cast)) return 0;
    InterlockedExchange(&is_ability_installed, 1);
    return 1;
}

uint32_t fang_control_ability_begin(uint32_t request_id, uint32_t scene, uint32_t handle, int32_t x, int32_t y,
    uint32_t ability_id, uint64_t deadline, fang_control_active_cast* snapshot) {
    fang_control_window_snapshot window;
    BYTE* player;
    uint32_t status = 5;
    if (!snapshot || !request_id || !handle ||
        (ability_id != 0x43C6B5F7 && ability_id != 0xF0D49F1D) ||
        !InterlockedCompareExchange(&is_ability_installed, 0, 0) ||
        !fang_control_window_inspect(&window) || window.thread_id != GetCurrentThreadId()) return 5;
    AcquireSRWLockExclusive(&ability_lock);
    ability_request = request_id; ability_scene = scene; ability_handle = handle; ability_thread = GetCurrentThreadId();
    ability_expected_id = ability_id;
    ability_window = (HWND)(uintptr_t)window.window_id; ability_deadline = deadline; ability_phase = 0; ability_failure = 0;
    memset(&ability_snapshot, 0, sizeof(ability_snapshot));
    ability_snapshot.action_index = 2;
    if (GetTickCount64() >= deadline || !is_ability_owner_current() || !is_ability_physical_clear()) goto done;
    /* Build 103 routes SpecialTwo to slot 2 for both Ride the Lightning and
     * Missile Barrage. Resolve the installed hero's slot before emitting an
     * edge; a stale or mismatched request must never cast another ability. */
    player = ((ability_player_fn)(ability_image + 0xE57B0))();
    ability_snapshot.ability_id = ((ability_identity_fn)(ability_image + 0x5C25C0))(player, 2, -1);
    ability_snapshot.fields |= 2;
    if (ability_snapshot.ability_id != ability_expected_id) goto done;
    if (!fang_control_input_aim_begin(request_id, scene, handle, x, y, deadline)) goto done;
    status = 0;
done:
    if (status) ability_failure = status;
    *snapshot = ability_snapshot;
    ReleaseSRWLockExclusive(&ability_lock);
    return status;
}

uint32_t fang_control_ability_frame(uint32_t request_id, fang_control_active_cast* snapshot) {
    uint32_t status = 0;
    AcquireSRWLockExclusive(&ability_lock);
    if (!snapshot || request_id != ability_request) { status = 5; goto done; }
    if (!is_ability_owner_current()) ability_failure = 5;
    if ((ability_phase == 1 || ability_phase == 2) && !ability_failure) {
        ability_snapshot.is_query_processed = 1;
        /* An edge without the observed caster call can leave native targeting
         * UI pending. It is not evidence of refusal and must never be replayed.
         * Only an actual negative original-caster return proves refusal. */
        if (!ability_snapshot.is_native_request_attempted) status = 6;
        else if (ability_snapshot.native_result_code < 0) status = 7;
        else if (ability_snapshot.native_result_code >= 1 && ability_snapshot.native_result_code <= 3) status = 1;
        else status = 6;
    } else if (GetTickCount64() >= ability_deadline && !ability_failure) ability_failure = 4;
    if (!status && ability_failure) status = ability_snapshot.is_invocation_attempted ? 6 : ability_failure;
    if (status) {
        ability_phase = 3;
        fang_control_input_cancel(request_id);
    }
    *snapshot = ability_snapshot;
done:
    ReleaseSRWLockExclusive(&ability_lock);
    return status;
}

void fang_control_ability_cancel(uint32_t request_id) {
    AcquireSRWLockExclusive(&ability_lock);
    if (ability_request == request_id) {
        ability_failure = 3;
        ability_phase = 3;
        fang_control_input_cancel(request_id);
    }
    ReleaseSRWLockExclusive(&ability_lock);
}
#endif

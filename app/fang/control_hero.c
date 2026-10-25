#include "hook.h"
#include "control.h"
#if FANG_SCENARIO
#include <string.h>
#if !defined(__GNUC__)
#include <intrin.h>
#endif

typedef unsigned char (FANG_THISCALL* hero_edge_fn)(void*, uint32_t);
typedef int (FANG_THISCALL* hero_eligibility_fn)(void*, uint32_t);
typedef BYTE* (__cdecl* hero_player_fn)(void);
typedef void* (FANG_THISCALL* hero_root_fn)(void*);
typedef void* (FANG_THISCALL* hero_modal_fn)(void*, int);
#if defined(__GNUC__)
#define HERO_FASTCALL __attribute__((fastcall))
#define HERO_CALLER() __builtin_return_address(0)
#else
#define HERO_FASTCALL __fastcall
#define HERO_CALLER() _ReturnAddress()
#endif

static SRWLOCK hero_lock = SRWLOCK_INIT;
static BYTE* hero_image;
static hero_edge_fn original_hero_edge;
static volatile LONG is_hero_installed;
static uint32_t hero_request, hero_scene, hero_handle, hero_phase, hero_failure;
static DWORD hero_thread;
static HWND hero_window;
static uint64_t hero_deadline;
static fang_control_hero_selection hero_snapshot;

static int is_hero_owner_current(int is_actor_required) {
    fang_control_snapshot boundary;
    DWORD process_id = 0;
    BYTE* root;
    BYTE* player;
    if (hero_thread != GetCurrentThreadId() || !hero_window || !IsWindow(hero_window) ||
        GetWindowThreadProcessId(hero_window, &process_id) != hero_thread || process_id != GetCurrentProcessId() ||
        !IsWindowVisible(hero_window) || !IsWindowEnabled(hero_window) || IsIconic(hero_window)) return 0;
    fang_control_read(&boundary);
    if (boundary.scene_load_return_count != hero_scene || boundary.frame_thread_id != hero_thread ||
        !boundary.frame_tick_ms || GetTickCount64() - boundary.frame_tick_ms > 250) return 0;
    root = *(BYTE**)(hero_image + 0x1038CB0);
    if (!readable_range(root, 48) || !readable_range(*(void**)(root + 4), 33500) ||
        (!*(uint32_t*)(root + 40) && !readable_range(*(void**)(hero_image + 0x1038CB4), 40))) return 0;
    if (!is_actor_required) return 1;
    player = ((hero_player_fn)(hero_image + 0xE57B0))();
    return readable_range(player, 5000) && *(uint32_t*)(player + 4664) == hero_handle;
}

static int is_hero_input_clear(void) {
    BYTE* manager = *(BYTE**)(hero_image + 0x105EDCC);
    void* root;
    void* modal;
    int key;
    if (!readable_range(manager, 4) || !readable_range(*(void**)manager, 76) || GetCapture()) return 0;
    /* Rebound slot keys remain user-owned: do not combine a synthetic edge
     * with any currently held physical keyboard or mouse button. */
    for (key = 1; key < 256; key++) if (GetAsyncKeyState(key) & 0x8000) return 0;
    modal = ((hero_modal_fn)(*(void***)manager)[18])(manager, 0);
    root = ((hero_root_fn)(*(void***)manager)[1])(manager);
    return root && modal == root;
}

static unsigned char HERO_FASTCALL scoped_hero_edge(void* input, void* ignored, uint32_t action) {
    void* caller = HERO_CALLER();
    unsigned char result = original_hero_edge(input, action);
    DWORD last_error = GetLastError();
    BYTE* player;
    uint32_t slot;
    (void)ignored;
    AcquireSRWLockExclusive(&hero_lock);
    if (!hero_request || hero_phase || hero_failure || GetCurrentThreadId() != hero_thread ||
        action < 0x10000017 || action > 0x10000019) goto done;
    slot = action - 0x10000017;
    if (caller != hero_image + (slot == 0 ? 0x51DD0 : slot == 1 ? 0x51DE6 : 0x51DFC)) goto done;
    if (result || GetTickCount64() >= hero_deadline || !is_hero_owner_current(1) || !is_hero_input_clear()) {
        hero_failure = 5;
        goto done;
    }
    if (slot != hero_snapshot.requested_slot) goto done;
    player = ((hero_player_fn)(hero_image + 0xE57B0))();
    hero_snapshot.eligibility_code = (uint32_t)((hero_eligibility_fn)(hero_image + 0x5C2B30))(player, slot);
    hero_snapshot.fields |= 4;
    if (hero_snapshot.eligibility_code || *(uint32_t*)(player + 12) == slot) {
        hero_failure = 5;
        goto done;
    }
    result = 1;
    hero_phase = 1;
    hero_snapshot.is_invocation_attempted = 1;
    trace_client_state("scenario_hero_slot_edge", slot);
done:
    ReleaseSRWLockExclusive(&hero_lock);
    result = fang_control_ability_edge(caller, action, result);
    SetLastError(last_error);
    return result;
}

int fang_control_hero_install(HMODULE executable) {
    static const BYTE edge_entry[] = {0x56,0x8B,0xF1,0x0F,0xB6,0x86,0xDC,0x01,0x00,0x00};
    static const BYTE eligibility_entry[] = {0x83,0xEC,0x08,0x53,0x8B,0x5C,0x24,0x10,0x56,0x8B,0xF1,0x3B,0x9E,0x78,0x13,0x00,0x00};
    static const BYTE selected_slot[] = {0x3B,0x5E,0x0C,0x75,0x28};
    static const BYTE slot0[] = {0x8B,0x16,0x8B,0x42,0x2C,0x68,0x17,0x00,0x00,0x10,0x8B,0xCE,0xFF,0xD0};
    static const BYTE slot1[] = {0x8B,0x16,0x8B,0x42,0x2C,0x68,0x18,0x00,0x00,0x10,0x8B,0xCE,0xFF,0xD0};
    static const BYTE slot2[] = {0x8B,0x16,0x8B,0x42,0x2C,0x68,0x19,0x00,0x00,0x10,0x8B,0xCE,0xFF,0xD0};
    static const BYTE action_call[] = {0xE8,0x09,0x3B,0x09,0x00};
    static const BYTE action_entry[] = {0x51,0xE8,0xEA,0xFE,0xFF,0xFF,0x8B,0x44,0x24,0x08,0x6A,0x04,
        0x8D,0x4C,0x24,0x04,0x51,0x6A,0x05,0x89,0x44,0x24,0x0C,0xE8,0x14,0xA2,0xFF,0xFF,0x83,0xC4,0x10,0xC3};
    BYTE* base = (BYTE*)executable;
    if (!base || InterlockedCompareExchange(&is_hero_installed, 0, 0) ||
        memcmp(base + 0x10BBD0, edge_entry, sizeof(edge_entry)) ||
        memcmp(base + 0x5C2B30, eligibility_entry, sizeof(eligibility_entry)) ||
        memcmp(base + 0x5C2B55, selected_slot, sizeof(selected_slot)) ||
        memcmp(base + 0x51DC2, slot0, sizeof(slot0)) || memcmp(base + 0x51DD8, slot1, sizeof(slot1)) ||
        memcmp(base + 0x51DEE, slot2, sizeof(slot2)) || memcmp(base + 0x51E02, action_call, sizeof(action_call)) ||
        memcmp(base + 0xE5910, action_entry, sizeof(action_entry)) ||
        base[0x10BC17] != 0xC2 || base[0x10BC18] != 4 || base[0x10BC19] != 0 ||
        *(void**)(base + 0xBDE464) != base + 0x10BBD0) return 0;
    hero_image = base;
    original_hero_edge = (hero_edge_fn)(base + 0x10BBD0);
    if (!patch_pointer((void**)(base + 0xBDE464), (void*)original_hero_edge, (void*)scoped_hero_edge)) return 0;
    InterlockedExchange(&is_hero_installed, 1);
    return 1;
}

uint32_t fang_control_hero_begin(uint32_t request_id, uint32_t scene, uint32_t handle, uint32_t slot,
    uint64_t deadline, fang_control_hero_selection* snapshot) {
    fang_control_window_snapshot window;
    BYTE* player;
    uint32_t status = 5;
    if (!snapshot || !InterlockedCompareExchange(&is_hero_installed, 0, 0) || slot > 2 ||
        !request_id || !handle || GetTickCount64() >= deadline || !fang_control_window_inspect(&window) ||
        window.thread_id != GetCurrentThreadId()) return 5;
    AcquireSRWLockExclusive(&hero_lock);
    hero_request = request_id; hero_scene = scene; hero_handle = handle; hero_thread = GetCurrentThreadId();
    hero_window = (HWND)(uintptr_t)window.window_id; hero_deadline = deadline; hero_phase = 0; hero_failure = 0;
    memset(&hero_snapshot, 0, sizeof(hero_snapshot));
    hero_snapshot.requested_slot = slot;
    hero_snapshot.previous_controlled_handle = handle;
    hero_snapshot.fields = 1;
    if (!is_hero_owner_current(1) || !is_hero_input_clear()) goto done;
    player = ((hero_player_fn)(hero_image + 0xE57B0))();
    /* Native9C2B30 compares requestedslot against player+12. This scalar is
     * native selected state, obtained through4E57B0, never a server index. */
    hero_snapshot.previous_slot = *(uint32_t*)(player + 12);
    hero_snapshot.eligibility_code = (uint32_t)((hero_eligibility_fn)(hero_image + 0x5C2B30))(player, slot);
    hero_snapshot.fields |= 12;
    if (hero_snapshot.previous_slot > 2 || hero_snapshot.previous_slot == slot || hero_snapshot.eligibility_code) goto done;
    status = 0;
done:
    if (status) hero_failure = status;
    *snapshot = hero_snapshot;
    ReleaseSRWLockExclusive(&hero_lock);
    return status;
}

uint32_t fang_control_hero_frame(uint32_t request_id, fang_control_hero_selection* snapshot) {
    uint32_t status = 0;
    BYTE* player;
    AcquireSRWLockExclusive(&hero_lock);
    if (hero_request != request_id) { status = 5; goto done; }
    if (!is_hero_owner_current(hero_phase == 0)) hero_failure = 5;
    if (hero_phase && !hero_failure) {
        /* The next owner frame follows451D50's synchronous normal4E5910
         * branch. This confirms input dispatch, not command/server success. */
        hero_snapshot.is_query_processed = 1;
        player = ((hero_player_fn)(hero_image + 0xE57B0))();
        if (readable_range(player, 4668)) {
            hero_snapshot.observed_controlled_handle = *(uint32_t*)(player + 4664);
            hero_snapshot.observed_slot = *(uint32_t*)(player + 12);
            if (hero_snapshot.observed_controlled_handle) hero_snapshot.fields |= 2;
            if (hero_snapshot.observed_slot <= 2) hero_snapshot.fields |= 16;
        }
        status = 1;
    } else if (GetTickCount64() >= hero_deadline && !hero_failure) hero_failure = 4;
    if (!status && hero_failure) status = hero_snapshot.is_invocation_attempted ? 6 : hero_failure;
    *snapshot = hero_snapshot;
done:
    ReleaseSRWLockExclusive(&hero_lock);
    return status;
}

void fang_control_hero_cancel(uint32_t request_id) {
    AcquireSRWLockExclusive(&hero_lock);
    if (hero_request == request_id) hero_failure = 3;
    ReleaseSRWLockExclusive(&hero_lock);
}
#endif

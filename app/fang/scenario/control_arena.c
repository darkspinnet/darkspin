#include "hook.h"
#include "scenario/control.h"
#if FANG_SCENARIO
#include <string.h>

/* Scenario-only native ArenaLobby input; no shipped resource changes. */
typedef struct arena_value {
    void* object_interface;
    uint32_t type;
    union { double number; void* object; unsigned char is_true; } data;
} arena_value;
typedef char arena_value_size[sizeof(arena_value) == 16 ? 1 : -1];
typedef int (FANG_THISCALL* arena_enter_fn)(BYTE*, const void*);
typedef int (FANG_THISCALL* arena_leave_fn)(BYTE*);
typedef unsigned char (FANG_THISCALL* arena_member_fn)(void*, void*, const char*, arena_value*, int);
typedef arena_value* (FANG_THISCALL* arena_copy_fn)(arena_value*, const arena_value*);
typedef void (FANG_THISCALL* arena_release_fn)(arena_value*);
typedef unsigned char (FANG_THISCALL* arena_invoke_fn)(arena_value*, const char*, arena_value*, const arena_value*, unsigned int);

static CRITICAL_SECTION arena_lock;
static volatile LONG is_arena_lock_initialized;
static int is_arena_installed;
static BYTE* arena_owner;
static DWORD arena_thread;
static uint64_t arena_generation;
static arena_enter_fn native_arena_enter;
static arena_leave_fn native_arena_leave;
static arena_member_fn arena_member;
static arena_copy_fn arena_copy;
static arena_release_fn arena_release;
static arena_invoke_fn arena_invoke;

#if defined(__GNUC__)
#define ARENA_FASTCALL __attribute__((fastcall))
#else
#define ARENA_FASTCALL __fastcall
#endif

// Modifies: records Fang ArenaLobby ownership and generation, then runs native entry with its effects.
static int ARENA_FASTCALL observe_arena_enter(BYTE* owner, void* ignored, const void* req) {
    int result;
    DWORD last_error = GetLastError();
    (void)ignored;
    EnterCriticalSection(&arena_lock);
    arena_owner = owner;
    arena_thread = GetCurrentThreadId();
    ++arena_generation;
    SetLastError(last_error);
    result = native_arena_enter(owner, req);
    last_error = GetLastError();
    LeaveCriticalSection(&arena_lock);
    SetLastError(last_error);
    return result;
}

// Modifies: invalidates matching Fang ArenaLobby ownership, then runs native exit with its effects.
static int ARENA_FASTCALL observe_arena_leave(BYTE* owner, void* ignored) {
    int result;
    DWORD last_error = GetLastError();
    (void)ignored;
    EnterCriticalSection(&arena_lock);
    if (arena_owner == owner) {
        arena_owner = NULL;
        arena_thread = 0;
        ++arena_generation;
    }
    SetLastError(last_error);
    result = native_arena_leave(owner);
    last_error = GetLastError();
    LeaveCriticalSection(&arena_lock);
    SetLastError(last_error);
    return result;
}

// Modifies: initializes Fang arena tracking, binds GFx helpers, and patches native lifecycle calls.
int fang_control_arena_install(HMODULE executable) {
    static const BYTE enter_call[] = {0xE8,0x28,0xFF,0xFF,0xFF};
    static const BYTE leave_call[] = {0xE8,0x60,0xFE,0xFF,0xFF};
    static const BYTE member_entry[] = {0x83,0xEC,0x10,0x80,0x7C,0x24,0x20,0x00};
    static const BYTE copy_entry[] = {0x56,0x57,0x8B,0x7C,0x24,0x0C};
    static const BYTE release_entry[] = {0x56,0x8B,0xF1,0x8B,0x46,0x04};
    static const BYTE invoke_entry[] = {0x51,0x8B,0x41,0x04,0x24,0x8F,0x3C,0x08};
    BYTE* base = (BYTE*)executable;
    if (!base || memcmp(base + 0x4543, enter_call, sizeof(enter_call)) ||
        memcmp(base + 0x1D8B, leave_call, sizeof(leave_call)) ||
        memcmp(base + 0x14DAE0, member_entry, sizeof(member_entry)) ||
        memcmp(base + 0x17F0, copy_entry, sizeof(copy_entry)) ||
        memcmp(base + 0x17C0, release_entry, sizeof(release_entry)) ||
        memcmp(base + 0x15A0, invoke_entry, sizeof(invoke_entry))) return 0;
    if (InterlockedCompareExchange(&is_arena_lock_initialized, 0, 0)) return 0;
    if (!InitializeCriticalSectionAndSpinCount(&arena_lock, 4000)) return 0;
    InterlockedExchange(&is_arena_lock_initialized, 1);
    native_arena_enter = (arena_enter_fn)(base + 0x4470);
    native_arena_leave = (arena_leave_fn)(base + 0x1BF0);
    arena_member = (arena_member_fn)(base + 0x14DAE0);
    arena_copy = (arena_copy_fn)(base + 0x17F0);
    arena_release = (arena_release_fn)(base + 0x17C0);
    arena_invoke = (arena_invoke_fn)(base + 0x15A0);
    if (!patch_call(base + 0x4543, (void*)native_arena_enter, (void*)observe_arena_enter)) return 0;
    if (!patch_call(base + 0x1D8B, (void*)native_arena_leave, (void*)observe_arena_leave)) {
        if (!patch_call(base + 0x4543, (void*)observe_arena_enter, (void*)native_arena_enter))
            trace_client_state("scenario_arena_enter_rollback_failed", 1);
        return 0;
    }
    EnterCriticalSection(&arena_lock);
    is_arena_installed = 1;
    LeaveCriticalSection(&arena_lock);
    return 1;
}

// Reads/modifies: queries a current native GFx member into the output, acquiring any managed result reference.
static int arena_value_member(BYTE* owner, uint64_t generation, arena_value* parent, const char* name, arena_value* result) {
    uint32_t type = parent->type & 0x8F;
    if (arena_owner != owner || arena_generation != generation || !parent->object_interface || type < 6 || type > 8) return 0;
    return arena_member(parent->object_interface, parent->data.object, name, result, type == 8) != 0;
}

// Reads/modifies: copies a typed native GFx scalar and availability bit out, then releases its temporary reference.
static void arena_scalar(BYTE* owner, uint64_t generation, arena_value* parent, const char* name,
    uint32_t* scalar, uint32_t bit, uint32_t* fields, int is_boolean) {
    arena_value result = {0};
    if (arena_value_member(owner, generation, parent, name, &result)) {
        if (is_boolean && (result.type & 0x8F) == 2) {
            *scalar = result.data.is_true != 0;
            *fields |= bit;
        } else if (!is_boolean && (result.type & 0x8F) == 3 && result.data.number >= 0 &&
            result.data.number <= 4294967295.0 && result.data.number == (double)(uint32_t)result.data.number) {
            *scalar = (uint32_t)result.data.number;
            *fields |= bit;
        }
    }
    arena_release(&result);
}

/* Readiness means the authored Accept control is currently eligible. It does
 * not assert rendering completion or invent a carousel animation flag. */
// Reads: returns whether the snapshot's authored Accept control and squad are eligible.
static int can_accept_arena(const fang_control_arena_state* snapshot) {
    return snapshot->is_available && snapshot->is_loaded && (snapshot->fields & 15) == 15 && snapshot->squad_index >= 1 && snapshot->squad_index <= 3 &&
        snapshot->is_squad_valid && !snapshot->is_accept_disabled && snapshot->is_accept_visible;
}

// Reads/modifies: fills an ArenaLobby snapshot through native GFx queries and balances temporary managed references.
int fang_control_arena_read(fang_control_arena_state* snapshot) {
    arena_value controller = {0}, root = {0}, accept = {0}, squads = {0}, squad = {0};
    BYTE* owner;
    uint64_t generation;
    static const char* squad_keys[] = {"0", "1", "2"};
    int is_available = 0;
    if (!snapshot || !InterlockedCompareExchange(&is_arena_lock_initialized, 0, 0)) return 0;
    memset(snapshot, 0, sizeof(*snapshot));
    EnterCriticalSection(&arena_lock);
    generation = arena_generation;
    owner = arena_owner;
    if (!is_arena_installed) goto done;
    if (!owner) {
        snapshot->is_available = 1;
        is_available = 1;
        goto done;
    }
    if (arena_thread != GetCurrentThreadId() || !readable_range(owner, 0x6C0)) goto done;
    snapshot->is_available = 1;
    is_available = 1;
    if (!owner[48]) goto done;
    arena_copy(&controller, (const arena_value*)(owner + 32));
    if (!controller.object_interface || (controller.type & 0x8F) < 6 || (controller.type & 0x8F) > 8) goto done;
    snapshot->is_loaded = 1;
    arena_scalar(owner, generation, &controller, "mSquadIndex", &snapshot->squad_index, 1, &snapshot->fields, 0);
    if ((snapshot->fields & 1) && snapshot->squad_index >= 1 && snapshot->squad_index <= 3 &&
        arena_value_member(owner, generation, &controller, "mSquadData", &squads) &&
        arena_value_member(owner, generation, &squads, squad_keys[snapshot->squad_index - 1], &squad))
        arena_scalar(owner, generation, &squad, "mValid", &snapshot->is_squad_valid, 2, &snapshot->fields, 1);
    if (arena_value_member(owner, generation, &controller, "mRoot", &root)) {
        if (arena_value_member(owner, generation, &root, "btnAcceptMC", &accept)) {
            arena_scalar(owner, generation, &accept, "disabled", &snapshot->is_accept_disabled, 4, &snapshot->fields, 1);
            arena_scalar(owner, generation, &accept, "_visible", &snapshot->is_accept_visible, 8, &snapshot->fields, 1);
        }
    }
    if (arena_owner != owner || arena_generation != generation || !owner[48]) goto invalid;
    snapshot->is_ready = can_accept_arena(snapshot);
    goto done;
invalid:
    memset(snapshot, 0, sizeof(*snapshot));
    is_available = 0;
done:
    if (arena_release) {
        arena_release(&squad);
        arena_release(&squads); arena_release(&accept); arena_release(&root); arena_release(&controller);
    }
    if (arena_generation != generation) {
        memset(snapshot, 0, sizeof(*snapshot));
        is_available = 0;
    }
    LeaveCriticalSection(&arena_lock);
    return is_available;
}

// Reads/modifies: fills the snapshot and invokes the eligible native OnReadyClicked handler, logging the attempt.
uint32_t fang_control_arena_accept(fang_control_arena_state* snapshot) {
    arena_value controller = {0}, result = {0};
    uint32_t status = 5;
    uint64_t generation;
    BYTE* owner;
    if (!snapshot || !InterlockedCompareExchange(&is_arena_lock_initialized, 0, 0)) return 5;
    EnterCriticalSection(&arena_lock);
    generation = arena_generation;
    owner = arena_owner;
    if (!fang_control_arena_read(snapshot) || !can_accept_arena(snapshot) ||
        arena_generation != generation || arena_owner != owner || !owner ||
        arena_thread != GetCurrentThreadId()) goto done;
    arena_copy(&controller, (const arena_value*)(owner + 32));
    if (arena_generation != generation || arena_owner != owner || !controller.object_interface ||
        (controller.type & 0x8F) < 6 || (controller.type & 0x8F) > 8) goto done;
    trace_client_state("scenario_arena_accept_invoke", 1);
    status = arena_invoke(&controller, "OnReadyClicked", &result, NULL, 0) ? 1 : 6;
done:
    if (arena_release) { arena_release(&result); arena_release(&controller); }
    LeaveCriticalSection(&arena_lock);
    return status;
}
#endif

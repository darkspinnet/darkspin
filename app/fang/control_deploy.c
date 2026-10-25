#include "hook.h"
#include "control.h"
#if FANG_SCENARIO
#include <string.h>

/* PlanetScreen owns two GFx Values, not a virtual native controller. Capture
 * construction/close, and never infer its lifetime from a retained UI pointer. */
typedef struct deploy_value {
    void* object_interface;
    uint32_t type;
    union { double number; void* object; unsigned char is_true; } data;
} deploy_value;
typedef char deploy_value_size[sizeof(deploy_value) == 16 ? 1 : -1];
typedef void (FANG_THISCALL* deploy_enter_fn)(BYTE*, const void*, const void*);
typedef void (FANG_THISCALL* deploy_leave_fn)(BYTE*);
typedef unsigned char (FANG_THISCALL* deploy_member_fn)(void*, void*, const char*, deploy_value*, int);
typedef deploy_value* (FANG_THISCALL* deploy_copy_fn)(deploy_value*, const deploy_value*);
typedef void (FANG_THISCALL* deploy_release_fn)(deploy_value*);
typedef unsigned char (FANG_THISCALL* deploy_invoke_fn)(deploy_value*, const char*, deploy_value*, const deploy_value*, unsigned int);

static CRITICAL_SECTION deploy_lock;
static volatile LONG is_deploy_lock_initialized;
static int is_deploy_installed;
static BYTE* deploy_owner;
static DWORD deploy_thread;
static uint64_t deploy_generation;
static deploy_enter_fn native_deploy_enter;
static deploy_leave_fn native_deploy_leave;
static deploy_member_fn deploy_member;
static deploy_copy_fn deploy_copy;
static deploy_release_fn deploy_release;
static deploy_invoke_fn deploy_invoke;

#if defined(__GNUC__)
#define DEPLOY_FASTCALL __attribute__((fastcall))
#else
#define DEPLOY_FASTCALL __fastcall
#endif

static void DEPLOY_FASTCALL observe_deploy_enter(BYTE* owner, void* ignored, const void* req, const void* detail) {
    DWORD last_error = GetLastError();
    (void)ignored;
    EnterCriticalSection(&deploy_lock);
    deploy_owner = owner;
    deploy_thread = GetCurrentThreadId();
    ++deploy_generation;
    SetLastError(last_error);
    native_deploy_enter(owner, req, detail);
    last_error = GetLastError();
    LeaveCriticalSection(&deploy_lock);
    SetLastError(last_error);
}

static void DEPLOY_FASTCALL observe_deploy_leave(BYTE* owner, void* ignored) {
    DWORD last_error = GetLastError();
    (void)ignored;
    EnterCriticalSection(&deploy_lock);
    if (deploy_owner == owner) {
        deploy_owner = NULL;
        deploy_thread = 0;
        ++deploy_generation;
    }
    SetLastError(last_error);
    native_deploy_leave(owner);
    last_error = GetLastError();
    LeaveCriticalSection(&deploy_lock);
    SetLastError(last_error);
}

int fang_control_deployment_install(HMODULE executable) {
    static const BYTE enter_call[] = {0xE8,0xBC,0xFE,0xFF,0xFF};
    static const BYTE leave_call[] = {0xE8,0xF0,0xFD,0xFF,0xFF};
    static const BYTE member_entry[] = {0x83,0xEC,0x10,0x80,0x7C,0x24,0x20,0x00};
    static const BYTE copy_entry[] = {0x56,0x57,0x8B,0x7C,0x24,0x0C};
    static const BYTE release_entry[] = {0x56,0x8B,0xF1,0x8B,0x46,0x04};
    static const BYTE invoke_entry[] = {0x51,0x8B,0x41,0x04,0x24,0x8F,0x3C,0x08};
    BYTE* base = (BYTE*)executable;
    if (!base || memcmp(base + 0x127DBF, enter_call, sizeof(enter_call)) ||
        memcmp(base + 0x127C5B, leave_call, sizeof(leave_call)) ||
        memcmp(base + 0x14DAE0, member_entry, sizeof(member_entry)) ||
        memcmp(base + 0x17F0, copy_entry, sizeof(copy_entry)) ||
        memcmp(base + 0x17C0, release_entry, sizeof(release_entry)) ||
        memcmp(base + 0x15A0, invoke_entry, sizeof(invoke_entry))) return 0;
    if (InterlockedCompareExchange(&is_deploy_lock_initialized, 0, 0)) return 0;
    if (!InitializeCriticalSectionAndSpinCount(&deploy_lock, 4000)) return 0;
    InterlockedExchange(&is_deploy_lock_initialized, 1);
    native_deploy_enter = (deploy_enter_fn)(base + 0x127C80);
    native_deploy_leave = (deploy_leave_fn)(base + 0x127A50);
    deploy_member = (deploy_member_fn)(base + 0x14DAE0);
    deploy_copy = (deploy_copy_fn)(base + 0x17F0);
    deploy_release = (deploy_release_fn)(base + 0x17C0);
    deploy_invoke = (deploy_invoke_fn)(base + 0x15A0);
    if (!patch_call(base + 0x127DBF, (void*)native_deploy_enter, (void*)observe_deploy_enter)) return 0;
    if (!patch_call(base + 0x127C5B, (void*)native_deploy_leave, (void*)observe_deploy_leave)) {
        if (!patch_call(base + 0x127DBF, (void*)observe_deploy_enter, (void*)native_deploy_enter))
            trace_client_state("scenario_deployment_enter_rollback_failed", 1);
        return 0;
    }
    EnterCriticalSection(&deploy_lock);
    is_deploy_installed = 1;
    LeaveCriticalSection(&deploy_lock);
    return 1;
}

static int deployment_member(BYTE* owner, uint64_t generation, deploy_value* parent, const char* name, deploy_value* result) {
    uint32_t type = parent->type & 0x8F;
    if (deploy_owner != owner || deploy_generation != generation || !parent->object_interface || type < 6 || type > 8) return 0;
    return deploy_member(parent->object_interface, parent->data.object, name, result, type == 8) != 0;
}

static void deployment_scalar(BYTE* owner, uint64_t generation, deploy_value* parent, const char* name,
    uint32_t* scalar, uint32_t bit, uint32_t* fields, int is_boolean) {
    deploy_value result = {0};
    if (deployment_member(owner, generation, parent, name, &result)) {
        if (is_boolean && (result.type & 0x8F) == 2) {
            *scalar = result.data.is_true != 0;
            *fields |= bit;
        } else if (!is_boolean && (result.type & 0x8F) == 3 && result.data.number >= 0 &&
            result.data.number <= 4294967295.0 && result.data.number == (double)(uint32_t)result.data.number) {
            *scalar = (uint32_t)result.data.number;
            *fields |= bit;
        }
    }
    deploy_release(&result);
}

/* Readiness means the authored Accept control is currently eligible. It does
 * not assert rendering completion or invent a carousel animation flag. */
static int can_continue_deployment(const fang_control_deployment_state* snapshot) {
    return snapshot->is_available && snapshot->is_loaded && !snapshot->is_results_screen &&
        (snapshot->fields & 63) == 63 && snapshot->squad_index >= 1 && snapshot->squad_index <= 3 &&
        snapshot->is_squad_valid && !snapshot->is_accept_disabled && snapshot->is_accept_visible;
}

int fang_control_deployment_read(fang_control_deployment_state* snapshot) {
    deploy_value controller = {0}, root = {0}, accept = {0}, squads = {0}, squad = {0};
    deploy_value left = {0}, right = {0};
    BYTE* owner;
    uint64_t generation;
    uint32_t is_left_disabled = 0, is_right_disabled = 0, button_fields = 0;
    static const char* squad_keys[] = {"0", "1", "2"};
    int is_available = 0;
    if (!snapshot || !InterlockedCompareExchange(&is_deploy_lock_initialized, 0, 0)) return 0;
    memset(snapshot, 0, sizeof(*snapshot));
    EnterCriticalSection(&deploy_lock);
    generation = deploy_generation;
    owner = deploy_owner;
    if (!is_deploy_installed) goto done;
    if (!owner) {
        snapshot->is_available = 1;
        is_available = 1;
        goto done;
    }
    if (deploy_thread != GetCurrentThreadId() || !readable_range(owner, 0x188)) goto done;
    snapshot->is_available = 1;
    is_available = 1;
    if (!owner[32]) goto done;
    snapshot->is_results_screen = owner[64] != 0;
    deploy_copy(&controller, (const deploy_value*)(owner + 16));
    if (!controller.object_interface || (controller.type & 0x8F) < 6 || (controller.type & 0x8F) > 8) goto done;
    snapshot->is_loaded = 1;
    deployment_scalar(owner, generation, &controller, "mSquadIndex", &snapshot->squad_index, 1, &snapshot->fields, 0);
    deployment_scalar(owner, generation, &controller, "mMajorDifficulty", &snapshot->major_difficulty, 2, &snapshot->fields, 0);
    deployment_scalar(owner, generation, &controller, "mMinorDifficulty", &snapshot->minor_difficulty, 4, &snapshot->fields, 0);
    if ((snapshot->fields & 1) && snapshot->squad_index >= 1 && snapshot->squad_index <= 3 &&
        deployment_member(owner, generation, &controller, "mSquadData", &squads) &&
        deployment_member(owner, generation, &squads, squad_keys[snapshot->squad_index - 1], &squad))
        deployment_scalar(owner, generation, &squad, "mValid", &snapshot->is_squad_valid, 8, &snapshot->fields, 1);
    if (deployment_member(owner, generation, &controller, "mRoot", &root)) {
        if (deployment_member(owner, generation, &root, "btnAcceptMC", &accept)) {
            deployment_scalar(owner, generation, &accept, "disabled", &snapshot->is_accept_disabled, 16, &snapshot->fields, 1);
            deployment_scalar(owner, generation, &accept, "_visible", &snapshot->is_accept_visible, 32, &snapshot->fields, 1);
        }
        if (deployment_member(owner, generation, &root, "btnScrubberLeft", &left) &&
            deployment_member(owner, generation, &root, "btnScrubberRight", &right)) {
            deployment_scalar(owner, generation, &left, "disabled", &is_left_disabled, 1, &button_fields, 1);
            deployment_scalar(owner, generation, &right, "disabled", &is_right_disabled, 2, &button_fields, 1);
            if (button_fields == 3) {
                snapshot->are_squad_buttons_disabled = is_left_disabled && is_right_disabled;
                snapshot->fields |= 64;
            }
        }
    }
    if (deploy_owner != owner || deploy_generation != generation || !owner[32]) goto invalid;
    snapshot->is_ready = can_continue_deployment(snapshot);
    goto done;
invalid:
    memset(snapshot, 0, sizeof(*snapshot));
    is_available = 0;
done:
    if (deploy_release) {
        deploy_release(&right); deploy_release(&left); deploy_release(&squad);
        deploy_release(&squads); deploy_release(&accept); deploy_release(&root); deploy_release(&controller);
    }
    if (deploy_generation != generation) {
        memset(snapshot, 0, sizeof(*snapshot));
        is_available = 0;
    }
    LeaveCriticalSection(&deploy_lock);
    return is_available;
}

uint32_t fang_control_deployment_continue(fang_control_deployment_state* snapshot) {
    deploy_value controller = {0}, result = {0};
    uint32_t status = 5;
    uint64_t generation;
    BYTE* owner;
    if (!snapshot || !InterlockedCompareExchange(&is_deploy_lock_initialized, 0, 0)) return 5;
    EnterCriticalSection(&deploy_lock);
    generation = deploy_generation;
    owner = deploy_owner;
    if (!fang_control_deployment_read(snapshot) || !can_continue_deployment(snapshot) ||
        deploy_generation != generation || deploy_owner != owner || !owner ||
        deploy_thread != GetCurrentThreadId()) goto done;
    deploy_copy(&controller, (const deploy_value*)(owner + 16));
    if (deploy_generation != generation || deploy_owner != owner || !controller.object_interface ||
        (controller.type & 0x8F) < 6 || (controller.type & 0x8F) > 8) goto done;
    trace_client_state("scenario_mission_continue_invoke", 1);
    status = deploy_invoke(&controller, "AcceptMission", &result, NULL, 0) ? 1 : 6;
done:
    if (deploy_release) { deploy_release(&result); deploy_release(&controller); }
    LeaveCriticalSection(&deploy_lock);
    return status;
}
#endif

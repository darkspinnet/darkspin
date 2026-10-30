#include "hook.h"
#include "scenario/control.h"
#if FANG_SCENARIO
#include <string.h>

typedef struct map_value {
    void* object_interface;
    uint32_t type;
    union { double number; void* object; unsigned char is_true; } data;
} map_value;
typedef char map_value_size[sizeof(map_value) == 16 ? 1 : -1];
typedef int (FANG_THISCALL* map_enter_fn)(BYTE*);
typedef void (FANG_THISCALL* map_leave_fn)(BYTE*);
typedef unsigned char (FANG_THISCALL* map_member_fn)(void*, void*, const char*, map_value*, int);
typedef map_value* (FANG_THISCALL* map_copy_fn)(map_value*, const map_value*);
typedef void (FANG_THISCALL* map_release_fn)(map_value*);
typedef unsigned char (FANG_THISCALL* map_invoke_fn)(map_value*, const char*, map_value*, const map_value*, unsigned int);
typedef struct map_read_context {
    BYTE* owner;
    uint64_t generation;
} map_read_context;

static CRITICAL_SECTION map_lock;
static volatile LONG is_map_lock_initialized;
static int is_map_installed;
static BYTE* map_image;
static BYTE* map_controller;
static DWORD map_thread;
static uint64_t map_generation;
static map_enter_fn native_map_enter;
static map_leave_fn native_map_leave;
static map_member_fn map_member;
static map_copy_fn map_copy;
static map_release_fn map_release;
static map_invoke_fn map_invoke;

#if defined(__GNUC__)
#define MAP_FASTCALL __attribute__((fastcall))
#else
#define MAP_FASTCALL __fastcall
#endif

// Modifies: records Fang map ownership and generation, then runs native map entry with its effects.
static int MAP_FASTCALL observe_map_enter(BYTE* controller, void* ignored) {
    int result;
    DWORD last_error = GetLastError();
    (void)ignored;
    EnterCriticalSection(&map_lock);
    map_controller = controller;
    map_thread = GetCurrentThreadId();
    ++map_generation;
    SetLastError(last_error);
    result = native_map_enter(controller);
    last_error = GetLastError();
    LeaveCriticalSection(&map_lock);
    SetLastError(last_error);
    return result;
}

// Modifies: invalidates matching Fang map ownership, then runs native map exit with its effects.
static void MAP_FASTCALL observe_map_leave(BYTE* controller, void* ignored) {
    DWORD last_error = GetLastError();
    (void)ignored;
    EnterCriticalSection(&map_lock);
    if (map_controller == controller) {
        map_controller = NULL;
        map_thread = 0;
        ++map_generation;
    }
    SetLastError(last_error);
    native_map_leave(controller);
    last_error = GetLastError();
    LeaveCriticalSection(&map_lock);
    SetLastError(last_error);
}

// Modifies: initializes Fang map tracking, binds GFx helpers, and patches native lifecycle calls.
int fang_control_map_install(HMODULE executable) {
    static const BYTE enter_call[] = {0xE8,0x44,0xFE,0xFF,0xFF};
    static const BYTE null_call[] = {0xE8,0x37,0xFE,0xFF,0xFF};
    static const BYTE leave_call[] = {0xE8,0xB0,0xFC,0xFF,0xFF};
    static const BYTE member_entry[] = {0x83,0xEC,0x10,0x80,0x7C,0x24,0x20,0x00};
    static const BYTE copy_entry[] = {0x56,0x57,0x8B,0x7C,0x24,0x0C};
    static const BYTE release_entry[] = {0x56,0x8B,0xF1,0x8B,0x46,0x04};
    static const BYTE invoke_entry[] = {0x51,0x8B,0x41,0x04,0x24,0x8F,0x3C,0x08};
    BYTE* base = (BYTE*)executable;
    if (!base || memcmp(base + 0x1184F7, enter_call, sizeof(enter_call)) ||
        memcmp(base + 0x118504, null_call, sizeof(null_call)) ||
        memcmp(base + 0x1160EB, leave_call, sizeof(leave_call)) ||
        memcmp(base + 0x14DAE0, member_entry, sizeof(member_entry)) ||
        memcmp(base + 0x17F0, copy_entry, sizeof(copy_entry)) ||
        memcmp(base + 0x17C0, release_entry, sizeof(release_entry)) ||
        memcmp(base + 0x15A0, invoke_entry, sizeof(invoke_entry))) return 0;
    if (InterlockedCompareExchange(&is_map_lock_initialized, 0, 0)) return 0;
    if (!InitializeCriticalSectionAndSpinCount(&map_lock, 4000)) return 0;
    InterlockedExchange(&is_map_lock_initialized, 1);
    map_image = base;
    native_map_enter = (map_enter_fn)(base + 0x118340);
    native_map_leave = (map_leave_fn)(base + 0x115DA0);
    map_member = (map_member_fn)(base + 0x14DAE0);
    map_copy = (map_copy_fn)(base + 0x17F0);
    map_release = (map_release_fn)(base + 0x17C0);
    map_invoke = (map_invoke_fn)(base + 0x15A0);
    if (!patch_call(base + 0x1184F7, (void*)native_map_enter, (void*)observe_map_enter)) return 0;
    if (!patch_call(base + 0x118504, (void*)native_map_enter, (void*)observe_map_enter)) goto rollback_enter;
    if (!patch_call(base + 0x1160EB, (void*)native_map_leave, (void*)observe_map_leave)) goto rollback_null;
    EnterCriticalSection(&map_lock);
    is_map_installed = 1;
    LeaveCriticalSection(&map_lock);
    return 1;
rollback_null:
    if (!patch_call(base + 0x118504, (void*)observe_map_enter, (void*)native_map_enter))
        trace_client_state("scenario_map_null_rollback_failed", 1);
rollback_enter:
    if (!patch_call(base + 0x1184F7, (void*)observe_map_enter, (void*)native_map_enter))
        trace_client_state("scenario_map_enter_rollback_failed", 1);
    return 0;
}

// Reads/modifies: queries a current native GFx member into the output, acquiring any managed result reference.
static int read_map_member(const map_read_context* context, map_value* parent, const char* name, map_value* result) {
    uint32_t type = parent->type & 0x8F;
    if (map_generation != context->generation || map_controller != context->owner) return 0;
    if (!parent->object_interface || type < 6 || type > 8) return 0;
    return map_member(parent->object_interface, parent->data.object, name, result, type == 8) != 0;
}

// Reads/modifies: copies an integral native GFx number and availability bit out, then releases its temporary reference.
static void read_map_number(const map_read_context* context, map_value* parent, const char* name, uint32_t* number, uint32_t bit, uint32_t* fields) {
    map_value result = {0};
    if (read_map_member(context, parent, name, &result) && (result.type & 0x8F) == 3 &&
        result.data.number >= 0 && result.data.number <= 4294967295.0 &&
        result.data.number == (double)(uint32_t)result.data.number) {
        *number = (uint32_t)result.data.number;
        *fields |= bit;
    }
    map_release(&result);
}

// Reads/modifies: copies a native GFx boolean and availability bit out, then releases its temporary reference.
static void read_map_boolean(const map_read_context* context, map_value* parent, const char* name, uint32_t* is_true, uint32_t bit, uint32_t* fields) {
    map_value result = {0};
    if (read_map_member(context, parent, name, &result) && (result.type & 0x8F) == 2) {
        *is_true = result.data.is_true != 0;
        *fields |= bit;
    }
    map_release(&result);
}

// Reads/modifies: queries native button visibility/enabled state and balances temporary GFx references.
static int is_map_button_ready(const map_read_context* context, map_value* parent, const char* name) {
    map_value button = {0};
    uint32_t fields = 0, is_disabled = 0, is_visible = 0;
    int is_ready = 0;
    if (read_map_member(context, parent, name, &button)) {
        read_map_boolean(context, &button, "disabled", &is_disabled, 1, &fields);
        read_map_boolean(context, &button, "_visible", &is_visible, 2, &fields);
        is_ready = fields == 3 && !is_disabled && is_visible;
    }
    map_release(&button);
    return is_ready;
}

// Reads/modifies: derives selection readiness in the output snapshot from native GFx state, balancing temporary references.
static void read_map_selection_ready(const map_read_context* context,
    map_value* root, fang_control_map_state* snapshot) {
    map_value switcher = {0};
    uint32_t is_root_visible = 0, is_switcher_visible = 0;
    uint32_t visibility_fields = 0;
    if ((snapshot->fields & 97) != 97 || snapshot->is_transactions_processing ||
        !snapshot->are_party_members_ready) return;
    if ((snapshot->fields & 512) == 0 || snapshot->difficulty_mode != 1) return;
    read_map_boolean(context, root, "_visible", &is_root_visible, 1, &visibility_fields);
    if (visibility_fields != 1 || !is_root_visible) return;
    if (snapshot->state == 1) {
        snapshot->is_campaign_ready = is_map_button_ready(context, root, "btnMM1");
        return;
    }
    if ((snapshot->fields & 127) != 127 || snapshot->state != 5 || snapshot->game_type != 2 ||
        snapshot->difficulty < 1 || snapshot->difficulty > 24 || snapshot->animation_state != 0 ||
        snapshot->animation_difficulty != snapshot->difficulty) return;
    if (!read_map_member(context, root, "lvlSwitcherMC", &switcher)) goto done;
    read_map_boolean(context, &switcher, "_visible", &is_switcher_visible, 2, &visibility_fields);
    if ((visibility_fields & 2) == 0 || !is_switcher_visible) goto done;
    if (snapshot->difficulty > 1)
        snapshot->is_previous_ready = is_map_button_ready(context, &switcher, "btnPrevious");
    if (snapshot->difficulty < 24)
        snapshot->is_next_ready = is_map_button_ready(context, &switcher, "btnNext");
done:
    map_release(&switcher);
}

// Reads/modifies: fills a native map-state snapshot through GFx queries and balances temporary managed references.
int fang_control_map_read(fang_control_map_state* snapshot) {
    map_value controller = {0}, root = {0}, start = {0};
    BYTE* owner;
    int is_available = 0;
    uint64_t generation;
    map_read_context context;
    if (!snapshot || !InterlockedCompareExchange(&is_map_lock_initialized, 0, 0)) return 0;
    memset(snapshot, 0, sizeof(*snapshot));
    EnterCriticalSection(&map_lock);
    if (!is_map_installed) goto done;
    owner = map_controller;
    generation = map_generation;
    context.owner = owner;
    context.generation = generation;
    if (!owner) {
        snapshot->is_available = 1;
        is_available = 1;
        goto done;
    }
    if (map_thread != GetCurrentThreadId() || !readable_range(owner, 72) ||
        *(void**)owner != map_image + 0xBDFC60) goto done;
    snapshot->is_available = 1;
    is_available = 1;
    if (!owner[48]) goto done;
    /* Keep a managed copy throughout property reads and release every owned
     * GFx result on this same native UI thread. No pointer escapes the copy. */
    map_copy(&controller, (const map_value*)(owner + 32));
    if (!controller.object_interface || (controller.type & 0x8F) < 6 || (controller.type & 0x8F) > 8) goto done;
    snapshot->is_loaded = 1;
    read_map_number(&context, &controller, "mState", &snapshot->state, 1, &snapshot->fields);
    read_map_number(&context, &controller, "mCurrentDifficulty", &snapshot->difficulty, 2, &snapshot->fields);
    read_map_number(&context, &controller, "mSelectedGameType", &snapshot->game_type, 4, &snapshot->fields);
    read_map_number(&context, &controller, "mDifficultyAnimState", &snapshot->animation_state, 8, &snapshot->fields);
    read_map_number(&context, &controller, "mCurrentAnimationDifficulty", &snapshot->animation_difficulty, 16, &snapshot->fields);
    read_map_number(&context, &controller, "mCurrentDifficultyMode", &snapshot->difficulty_mode, 512, &snapshot->fields);
    read_map_boolean(&context, &controller, "mbTransactionsProcessing", &snapshot->is_transactions_processing, 32, &snapshot->fields);
    read_map_boolean(&context, &controller, "mbPartyMembersReadyToStartGame", &snapshot->are_party_members_ready, 64, &snapshot->fields);
    read_map_number(&context, &controller, "mPvPGameSize", &snapshot->pvp_game_size, 1024, &snapshot->fields);
    read_map_number(&context, &controller, "mPvPRankType", &snapshot->pvp_rank_type, 2048, &snapshot->fields);
    read_map_number(&context, &controller, "mNumPartyMembers", &snapshot->party_member_count, 4096, &snapshot->fields);
    read_map_boolean(&context, &controller, "mbPvPUnlocked", &snapshot->is_pvp_unlocked, 8192, &snapshot->fields);
    read_map_boolean(&context, &controller, "mbIsPartyLeader", &snapshot->is_party_leader, 16384, &snapshot->fields);
    read_map_boolean(&context, &controller, "mbYouAndPartyMembersCanStartPvP", &snapshot->are_pvp_members_ready, 32768, &snapshot->fields);
    if (read_map_member(&context, &controller, "mRoot", &root)) {
        if (read_map_member(&context, &root, "btnStartPlay", &start)) {
            read_map_boolean(&context, &start, "disabled", &snapshot->is_start_disabled, 128, &snapshot->fields);
            read_map_boolean(&context, &start, "_visible", &snapshot->is_start_visible, 256, &snapshot->fields);
        }
        read_map_selection_ready(&context, &root, snapshot);
    }
    /* A reentrant UI teardown invalidates the whole observation, even though
     * managed temporary references remained owned through cleanup. */
    if (map_generation != generation || map_controller != owner || !owner[48]) {
        memset(snapshot, 0, sizeof(*snapshot));
        is_available = 0;
    }
done:
    if (map_release) {
        map_release(&start);
        map_release(&root);
        map_release(&controller);
    }
    if (is_available && map_generation != generation) {
        memset(snapshot, 0, sizeof(*snapshot));
        is_available = 0;
    }
    LeaveCriticalSection(&map_lock);
    return is_available;
}

/* The authored handler performs the ordinary UI state transition and external
 * StartGame call. An Invoke failure after dispatch is an unknown outcome, never
 * permission to retry. The managed copy survives reentrant native teardown. */
// Reads/modifies: fills the snapshot and invokes eligible native OnPlayClicked, logging the mission-start attempt.
uint32_t fang_control_map_start(fang_control_map_state* snapshot) {
    map_value controller = {0}, result = {0};
    uint32_t status = 5;
    uint64_t generation;
    BYTE* owner;
    if (!snapshot || !InterlockedCompareExchange(&is_map_lock_initialized, 0, 0)) return 5;
    EnterCriticalSection(&map_lock);
    generation = map_generation;
    owner = map_controller;
    if (!fang_control_map_read(snapshot) || !snapshot->is_loaded || (snapshot->fields & 511) != 511 ||
        snapshot->state != 5 || snapshot->game_type != 2 || snapshot->difficulty < 1 || snapshot->difficulty > 72 ||
        snapshot->animation_state != 0 || snapshot->animation_difficulty != snapshot->difficulty ||
        snapshot->is_transactions_processing || !snapshot->are_party_members_ready ||
        snapshot->is_start_disabled || !snapshot->is_start_visible ||
        map_generation != generation || map_controller != owner || !owner ||
        map_thread != GetCurrentThreadId()) goto done;
    map_copy(&controller, (const map_value*)(owner + 32));
    if (map_generation != generation || map_controller != owner || !controller.object_interface ||
        (controller.type & 0x8F) < 6 || (controller.type & 0x8F) > 8) goto done;
    trace_client_state("scenario_mission_start_invoke", 1);
    status = map_invoke(&controller, "OnPlayClicked", &result, NULL, 0) ? 1 : 6;
done:
    if (map_release) {
        map_release(&result);
        map_release(&controller);
    }
    LeaveCriticalSection(&map_lock);
    return status;
}

/* Fixed ordinary UI handlers only. Their no-argument authored bodies retain
 * the game's animation, bounds, party and native external decision flow. */
// Reads/modifies: fills the snapshot and invokes an eligible authored campaign/level-selection handler, logging the attempt.
uint32_t fang_control_map_select(uint32_t kind, fang_control_map_state* snapshot) {
    map_value controller = {0}, result = {0};
    const char* handler;
    const char* trace;
    uint32_t status = 5;
    uint64_t generation;
    BYTE* owner;
    if (!snapshot || !InterlockedCompareExchange(&is_map_lock_initialized, 0, 0)) return 5;
    switch (kind) {
    case 9: handler = "OnCampaignClicked"; trace = "scenario_mission_campaign_invoke"; break;
    case 10: handler = "OnPreviousLevelClicked"; trace = "scenario_mission_previous_invoke"; break;
    case 11: handler = "OnNextLevelClicked"; trace = "scenario_mission_next_invoke"; break;
    default: return 2;
    }
    EnterCriticalSection(&map_lock);
    generation = map_generation;
    owner = map_controller;
    if (!fang_control_map_read(snapshot) ||
        (kind == 9 && !snapshot->is_campaign_ready) ||
        (kind == 10 && !snapshot->is_previous_ready) ||
        (kind == 11 && !snapshot->is_next_ready) ||
        map_generation != generation || map_controller != owner || !owner ||
        map_thread != GetCurrentThreadId()) goto done;
    map_copy(&controller, (const map_value*)(owner + 32));
    if (map_generation != generation || map_controller != owner || !controller.object_interface ||
        (controller.type & 0x8F) < 6 || (controller.type & 0x8F) > 8) goto done;
    trace_client_state(trace, 1);
    status = map_invoke(&controller, handler, &result, NULL, 0) ? 1 : 6;
done:
    if (map_release) { map_release(&result); map_release(&controller); }
    LeaveCriticalSection(&map_lock);
    return status;
}
#endif

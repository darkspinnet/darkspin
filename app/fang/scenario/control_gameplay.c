#include "scenario/camera.h"
#include "hook.h"
#include "scenario/control.h"
#include "scenario/control_input.h"
#if FANG_SCENARIO
#include <string.h>
#if !defined(__GNUC__)
#include <intrin.h>
#endif

typedef int (FANG_THISCALL* gameplay_mouse_fn)(void*, int);
typedef void* (__cdecl* gameplay_lookup_fn)(void);
typedef void* (FANG_THISCALL* gameplay_object_fn)(void*, uint32_t);
typedef void* (FANG_THISCALL* gameplay_method_fn)(void*);
typedef unsigned char (__cdecl* gameplay_visible_fn)(const float*, float*);
typedef float* (__cdecl* gameplay_project_fn)(const float*, float*, float*);
typedef int (__cdecl* gameplay_cancel_fn)(void);
#if defined(__GNUC__)
#define GAMEPLAY_FASTCALL __attribute__((fastcall))
#define GAMEPLAY_CALLER() __builtin_return_address(0)
#else
#define GAMEPLAY_FASTCALL __fastcall
#define GAMEPLAY_CALLER() _ReturnAddress()
#endif

static SRWLOCK gameplay_lock = SRWLOCK_INIT;
static BYTE* gameplay_image;
static volatile LONG is_gameplay_installed;
static gameplay_mouse_fn native_gameplay_mouse;
static gameplay_lookup_fn native_gameplay_player, native_gameplay_registry;
static gameplay_object_fn native_gameplay_object;
static gameplay_visible_fn native_gameplay_visible;
static gameplay_project_fn native_gameplay_project;
static DWORD gameplay_poll_thread;
static uint64_t gameplay_poll_tick;
static uint32_t gameplay_poll_scene;
static uint32_t gameplay_request, gameplay_kind;
static uint32_t gameplay_request_scene;
static uint64_t gameplay_deadline;
static float gameplay_point[3];
static fang_control_window_receipt gameplay_receipt;
static fang_control_gameplay_state gameplay_state;
static fang_control_projection gameplay_projection;
static fang_control_gameplay_click gameplay_click;
static uint32_t gameplay_hold_ms;
static uint32_t gameplay_hero_slot;
static uint32_t gameplay_ability_id;
static fang_control_hero_selection gameplay_hero;
static fang_control_active_cast gameplay_cast;
static int is_gameplay_input_started;

/* This hook only observes the exact ordinary gameplay poll, and preserves the
 * original answer. A recent poll is evidence of that native phase, not proof
 * of controllability, rendering, or server mission completion. */
// Reads/modifies: forwards native input polling, records Fang poll ownership, and may substitute an armed synthetic press.
static int GAMEPLAY_FASTCALL observe_gameplay_mouse(void* input, void* ignored, int key) {
    void* caller = GAMEPLAY_CALLER();
    int is_pressed = native_gameplay_mouse(input, key);
    DWORD last_error = GetLastError();
    fang_control_snapshot snapshot;
    DWORD previous_thread;
    (void)ignored;
    if (key == 1002 && caller == gameplay_image + 0x502FB) {
        fang_control_read(&snapshot);
        AcquireSRWLockExclusive(&gameplay_lock);
        previous_thread = gameplay_poll_thread;
        gameplay_poll_thread = GetCurrentThreadId();
        gameplay_poll_tick = GetTickCount64();
        gameplay_poll_scene = snapshot.scene_load_return_count;
        ReleaseSRWLockExclusive(&gameplay_lock);
        if (previous_thread != GetCurrentThreadId()) trace_client_state("scenario_gameplay_poll_thread", GetCurrentThreadId());
    }
    is_pressed = fang_control_input_press(caller, key, is_pressed);
    SetLastError(last_error);
    return is_pressed;
}

// Modifies: binds native gameplay accessors, patches input polling, and installs/logs scoped input and ability hooks.
int fang_control_gameplay_install(HMODULE executable) {
    int is_hero_ready;
    static const BYTE mouse_entry[] = {0x8B,0x44,0x24,0x04,0x80,0xBC,0x08,0xF8,0xFE,0xFF,0xFF,0x00};
    static const BYTE poll_call[] = {0x8B,0x06,0x8B,0x50,0x3C,0x68,0xEA,0x03,0x00,0x00,0x8B,0xCE,0xFF,0xD2};
    static const BYTE project_entry[] = {0x83,0xEC,0x1C,0xE8,0x08,0x0C,0x0F,0x00};
    static const BYTE visible_entry[] = {0x83,0xEC,0x0C,0xE8,0x68,0x11,0x38,0x00};
    static const BYTE object_entry[] = {0x8B,0x49,0x04,0xE9,0xB8,0x66,0xDE,0xFF};
    static const BYTE registry_tls[] = {0x64,0x8B,0x0D,0x2C,0x00,0x00,0x00,0x8B,0x14,0x81,0x8B,0x82,0x08,0x00,0x00,0x00};
    BYTE* base = (BYTE*)executable;
    if (!base || InterlockedCompareExchange(&is_gameplay_installed, 0, 0) ||
        memcmp(base + 0x10B340, mouse_entry, sizeof(mouse_entry)) ||
        memcmp(base + 0x502ED, poll_call, sizeof(poll_call)) ||
        memcmp(base + 0x2BDFA0, project_entry, sizeof(project_entry)) ||
        memcmp(base + 0x2DA40, visible_entry, sizeof(visible_entry)) ||
        memcmp(base + 0x5D1730, object_entry, sizeof(object_entry)) ||
        memcmp(base + 0x5BC745, registry_tls, sizeof(registry_tls)) ||
        base[0xE57B0] != 0x51 || base[0xE57B1] != 0xA1 ||
        *(void**)(base + 0xBDE474) != base + 0x10B340) return 0;
    gameplay_image = base;
    native_gameplay_mouse = (gameplay_mouse_fn)(base + 0x10B340);
    native_gameplay_player = (gameplay_lookup_fn)(base + 0xE57B0);
    native_gameplay_registry = (gameplay_lookup_fn)(base + 0x5BC740);
    native_gameplay_object = (gameplay_object_fn)(base + 0x5D1730);
    native_gameplay_visible = (gameplay_visible_fn)(base + 0x2DA40);
    native_gameplay_project = (gameplay_project_fn)(base + 0x2BDFA0);
    if (!patch_pointer((void**)(base + 0xBDE474), (void*)native_gameplay_mouse, (void*)observe_gameplay_mouse)) return 0;
    InterlockedExchange(&is_gameplay_installed, 1);
    trace_client_state("scenario_gameplay_input_hook", fang_control_input_install(executable));
    is_hero_ready = fang_control_hero_install(executable);
    trace_client_state("scenario_hero_input_hook", is_hero_ready);
    trace_client_state("scenario_ability_input_hook", is_hero_ready ? fang_control_ability_install(executable) : 0);
    return 1;
}

// Reads: returns whether the supplied float bit pattern is finite.
static int is_gameplay_float_finite(const float* scalar) {
    uint32_t bits;
    memcpy(&bits, scalar, sizeof(bits));
    return (bits & 0x7F800000u) != 0x7F800000u;
}

/* Build 103's sub_4F3DB0 selects the final type-3 graphics part. Bone
 * presentation then dereferences model+28 and rig+388. Observe that same
 * chain on the existing gameplay owner thread; no pointers leave this sample. */
// Reads/modifies: inspects the native graphics/model/rig chain and writes observation/readiness output flags.
static void read_gameplay_rig(const BYTE* object, uint32_t* is_observed, uint32_t* is_ready) {
    const BYTE* graphics;
    const BYTE* manager;
    const BYTE* pool;
    const BYTE* slots;
    const uint32_t* handles;
    uintptr_t begin, end, offset;
    uint32_t count, capacity, stride, index;
    *is_observed = 0;
    *is_ready = 0;
    if (!readable_range(object, 656)) return;
    graphics = *(const BYTE* const*)(object + 652);
    if (!graphics) { *is_observed = 1; return; }
    if (!readable_range(graphics, 112)) return;
    begin = *(const uintptr_t*)(graphics + 104);
    end = *(const uintptr_t*)(graphics + 108);
    if (end < begin || (end - begin) % sizeof(uint32_t) ||
        end - begin > 256 * sizeof(uint32_t)) return;
    count = (uint32_t)((end - begin) / sizeof(uint32_t));
    if (!count) { *is_observed = 1; return; }
    if (!readable_range((const void*)begin, end - begin)) return;
    if (!readable_range(gameplay_image + 0x1038EF0, sizeof(void*))) return;
    manager = *(const BYTE* const*)(gameplay_image + 0x1038EF0);
    if (!manager || (uintptr_t)manager > UINTPTR_MAX - 102436) return;
    pool = (const BYTE*)((uintptr_t)manager + 102436);
    if (!readable_range(pool, 24)) return;
    slots = *(const BYTE* const*)pool;
    capacity = *(const uint32_t*)(pool + 16);
    stride = *(const uint32_t*)(pool + 20);
    if (!slots || !capacity || capacity > 65536 || stride < 16 || stride > 4096) return;
    handles = (const uint32_t*)begin;
    for (index = count; index > 0; index--) {
        const BYTE* part;
        const BYTE* model;
        const BYTE* rig;
        const BYTE* skeleton;
        uint32_t slot = handles[index - 1] & 0xffffu;
        if (slot >= capacity) return;
        offset = (uintptr_t)slot * stride;
        if (offset > UINTPTR_MAX - (uintptr_t)slots) return;
        part = (const BYTE*)((uintptr_t)slots + offset);
        if (!readable_range(part, 16)) return;
        if (*(const uint32_t*)(part + 8) != 3) continue;
        model = *(const BYTE* const*)(part + 12);
        if (!model) { *is_observed = 1; return; }
        if (!readable_range(model, 32)) return;
        rig = *(const BYTE* const*)(model + 28);
        if (!rig) { *is_observed = 1; return; }
        if (!readable_range(rig, 392)) return;
        skeleton = *(const BYTE* const*)(rig + 388);
        if (skeleton && !readable_range(skeleton, sizeof(void*))) return;
        *is_observed = 1;
        *is_ready = skeleton != NULL;
        return;
    }
    *is_observed = 1;
}

// Reads/modifies: validates and copies the native selected hero's retained ranks into Fang gameplay state.
static void read_gameplay_ability_ranks(const BYTE* player) {
    /* Build 103 sub_9C2970 reads player[378 * selected + 237 + index].
     * Verify its installed accessor before observing the nine retained ranks;
     * no native call, rank assignment, or gameplay decision occurs here. */
    static const BYTE rank_entry[] = {
        0x8B,0x44,0x24,0x08,0x83,0xF8,0xFF,0x75,0x06,0x8B,0x41,0x0C,
        0x83,0xF8,0xFF,0x7E,0x14,0x69,0xC0,0x7A,0x01,0x00,0x00,
        0x03,0x44,0x24,0x04,0x8B,0x84,0x81,0xB4,0x03,0x00,0x00,0xC2,0x08,0x00
    };
    const uint32_t* ranks;
    int32_t selected;
    uint32_t index;
    if (!readable_range(gameplay_image + 0x5C2970, sizeof(rank_entry)) ||
        memcmp(gameplay_image + 0x5C2970, rank_entry, sizeof(rank_entry)) ||
        !readable_range(player, 16)) return;
    selected = *(const int32_t*)(player + 12);
    if (selected < 0 || selected > 2) return;
    ranks = (const uint32_t*)(player + 1512u * (uint32_t)selected + 948u);
    if (!readable_range(ranks, sizeof(gameplay_state.ability_ranks))) return;
    for (index = 0; index < 9; index++) {
        if (ranks[index] > 8) return;
    }
    memcpy(gameplay_state.ability_ranks, ranks, sizeof(gameplay_state.ability_ranks));
    if (*(const int32_t*)(player + 12) != selected) {
        memset(gameplay_state.ability_ranks, 0, sizeof(gameplay_state.ability_ranks));
        return;
    }
    gameplay_state.is_ability_rank_available = 1;
}

// Reads/modifies: samples native actor, rig, ranks, camera, and targeting into Fang gameplay state.
static void read_gameplay_state(uint32_t sequence, const fang_control_snapshot* boundary) {
    const BYTE* root;
    const BYTE* player;
    const BYTE* object;
    void* registry;
    uint32_t handle;
    memset(&gameplay_state, 0, sizeof(gameplay_state));
    gameplay_state.is_available = 1;
    gameplay_state.frame_sequence = sequence;
    gameplay_state.owner_thread_id = GetCurrentThreadId();
    if (gameplay_poll_thread != GetCurrentThreadId() || !gameplay_poll_tick ||
        GetTickCount64() - gameplay_poll_tick > 250 || gameplay_poll_scene != boundary->scene_load_return_count) return;
    root = *(const BYTE**)(gameplay_image + 0x1038CB0);
    if (!readable_range(root, 48) || !readable_range(*(void**)(root + 4), 33500) ||
        (!*(uint32_t*)(root + 40) && !readable_range(*(void**)(gameplay_image + 0x1038CB4), 40))) return;
    player = (const BYTE*)native_gameplay_player();
    if (!readable_range(player, 4668)) return;
    handle = *(const uint32_t*)(player + 4664);
    if (!handle) return;
    registry = native_gameplay_registry();
    if (!readable_range(registry, 8) || !readable_range(*(void**)((BYTE*)registry + 4), 24)) return;
    object = (const BYTE*)native_gameplay_object(registry, handle);
    if (!readable_range(object, 36) || *(const uint32_t*)object != handle) return;
    gameplay_state.controlled_handle = handle;
    gameplay_state.fields |= 1;
    if (is_gameplay_float_finite((const float*)(object + 24)) &&
        is_gameplay_float_finite((const float*)(object + 28)) && is_gameplay_float_finite((const float*)(object + 32))) {
        memcpy(&gameplay_state.x, object + 24, 3 * sizeof(float));
        gameplay_state.fields |= 2;
    }
    read_gameplay_rig(object, &gameplay_state.is_rig_observed, &gameplay_state.is_rig_ready);
    read_gameplay_ability_ranks(player);
    /* Double-check identity after copying. Handles retain their generation;
     * none of these pointers or server-ID assumptions leave this callback. */
    if (*(const uint32_t*)object != handle || *(const uint32_t*)(player + 4664) != handle) {
        gameplay_state.fields = 0;
        gameplay_state.is_rig_observed = 0;
        gameplay_state.is_rig_ready = 0;
        gameplay_state.is_ability_rank_available = 0;
        memset(gameplay_state.ability_ranks, 0, sizeof(gameplay_state.ability_ranks));
        return;
    }
    gameplay_state.is_world_active = 1;
    fang_control_camera_read(&gameplay_state.camera);
    /* This is the client's retained input target, not a server object ID. */
    gameplay_state.picked_handle = *(const uint32_t*)(gameplay_image + 0xDBEF10);
    gameplay_state.fields |= 4;
}

// Reads/modifies: invokes native visibility/projection queries and fills Fang's projected client coordinates.
static int read_gameplay_projection(void) {
    BYTE* manager = *(BYTE**)(gameplay_image + 0x105DB28);
    BYTE* camera;
    float ndc[2] = {0};
    fang_control_window_snapshot window;
    memset(&gameplay_projection, 0, sizeof(gameplay_projection));
    if (!gameplay_state.is_world_active || !readable_range(manager, 4) ||
        !readable_range(*(void**)manager, 120) || !fang_control_window_inspect(&window)) return 0;
    camera = ((gameplay_method_fn)(*(void***)manager)[29])(manager);
    if (!readable_range(camera, 436) || !readable_range(*(void**)(camera + 432), 136)) return 0;
    gameplay_projection.is_on_screen = native_gameplay_visible(gameplay_point, ndc) != 0;
    native_gameplay_project(gameplay_point, &gameplay_projection.x, &gameplay_projection.y);
    if (!is_gameplay_float_finite(&gameplay_projection.x) || !is_gameplay_float_finite(&gameplay_projection.y)) return 0;
    gameplay_projection.client_width = window.width;
    gameplay_projection.client_height = window.height;
    gameplay_projection.is_on_screen = gameplay_projection.is_on_screen &&
        gameplay_projection.x >= 0 && gameplay_projection.y >= 0 &&
        gameplay_projection.x < window.width && gameplay_projection.y < window.height;
    gameplay_projection.is_available = 1;
    return 1;
}

// Reads/modifies: advances pending Fang gameplay requests and may invoke native cancellation or arm scoped input actions.
void fang_control_gameplay_frame(uint32_t sequence) {
    fang_control_snapshot boundary;
    if (!InterlockedCompareExchange(&is_gameplay_installed, 0, 0)) return;
    fang_control_read(&boundary);
    AcquireSRWLockExclusive(&gameplay_lock);
    if (!gameplay_request || gameplay_receipt.status) goto done;
    gameplay_receipt.thread_id = GetCurrentThreadId();
    if (gameplay_kind == 6) {
        static const BYTE cancel_entry[] = {0xC6,0x05,0x4C,0x8A,0x43,0x01,0x01,0xC6,0x05,0x4D,0x8A,0x43,0x01,0x00,0xC6,0x05,0x4F,0x8A,0x43,0x01,0x01};
        fang_control_window_snapshot window;
        int key;
        read_gameplay_state(sequence, &boundary);
        if (GetTickCount64() >= gameplay_deadline) { gameplay_receipt.status = 4; goto complete; }
        if (boundary.scene_load_return_count != gameplay_request_scene || !gameplay_state.is_world_active ||
            !fang_control_window_inspect(&window) || window.thread_id != GetCurrentThreadId() ||
            !IsWindowVisible((HWND)(uintptr_t)window.window_id) || !window.is_enabled || window.is_minimized || GetCapture() ||
            memcmp(gameplay_image + 0xDFB20, cancel_entry, sizeof(cancel_entry))) {
            gameplay_receipt.status = 5; goto complete;
        }
        for (key = 1; key < 256; key++) {
            if (GetAsyncKeyState(key) & 0x8000) { gameplay_receipt.status = 5; goto complete; }
        }
        /* Build1034DFB20 ->4DF800 ->4DEF10 uses the client's ordinary
         * cancellation queue and encoder (command10). No server packet or
         * authoritative gameplay state is fabricated by the debugger. */
        trace_client_state("scenario_ability_cancel_invoked", gameplay_request);
        ((gameplay_cancel_fn)(gameplay_image + 0xDFB20))();
        gameplay_receipt.status = 1;
        goto complete;
    }
    if (gameplay_kind == 5) {
        read_gameplay_state(sequence, &boundary);
        if (is_gameplay_input_started) gameplay_receipt.status = fang_control_ability_frame(gameplay_request, &gameplay_cast);
        else if (GetTickCount64() >= gameplay_deadline) gameplay_receipt.status = 4;
        else if (boundary.scene_load_return_count != gameplay_request_scene || !read_gameplay_projection() ||
            !gameplay_projection.is_on_screen) gameplay_receipt.status = 5;
        else {
            gameplay_receipt.status = fang_control_ability_begin(gameplay_request, boundary.scene_load_return_count,
                gameplay_state.controlled_handle, (int32_t)gameplay_projection.x, (int32_t)gameplay_projection.y,
                gameplay_ability_id, gameplay_deadline, &gameplay_cast);
            if (!gameplay_receipt.status) is_gameplay_input_started = 1;
        }
        goto complete;
    }
    if (gameplay_kind == 4) {
        read_gameplay_state(sequence, &boundary);
        if (is_gameplay_input_started) gameplay_receipt.status = fang_control_hero_frame(gameplay_request, &gameplay_hero);
        else if (GetTickCount64() >= gameplay_deadline) gameplay_receipt.status = 4;
        else if (boundary.scene_load_return_count != gameplay_request_scene || !gameplay_state.is_world_active) gameplay_receipt.status = 5;
        else {
            gameplay_receipt.status = fang_control_hero_begin(gameplay_request, boundary.scene_load_return_count,
                gameplay_state.controlled_handle, gameplay_hero_slot, gameplay_deadline, &gameplay_hero);
            if (!gameplay_receipt.status) is_gameplay_input_started = 1;
        }
        goto complete;
    }
    if (gameplay_kind >= 2 && is_gameplay_input_started) {
        read_gameplay_state(sequence, &boundary);
        gameplay_receipt.status = fang_control_input_frame(gameplay_request, boundary.scene_load_return_count,
            gameplay_state.controlled_handle, gameplay_state.is_world_active, &gameplay_click);
    }
    else if (GetTickCount64() >= gameplay_deadline) gameplay_receipt.status = 4;
    else if (boundary.scene_load_return_count != gameplay_request_scene) gameplay_receipt.status = 5;
    else {
        read_gameplay_state(sequence, &boundary);
        if (gameplay_kind == 0) {
            fang_trace_camera_state();
            gameplay_receipt.status = 1;
        }
        else if (!read_gameplay_projection()) gameplay_receipt.status = 5;
        else if (gameplay_kind == 1) gameplay_receipt.status = 1;
        else {
            gameplay_click.is_projected = 1;
            if (!gameplay_projection.is_on_screen || !fang_control_input_begin(gameplay_request,
                boundary.scene_load_return_count, gameplay_state.controlled_handle,
                (int32_t)gameplay_projection.x, (int32_t)gameplay_projection.y, gameplay_deadline, gameplay_hold_ms)) gameplay_receipt.status = 5;
            else is_gameplay_input_started = 1;
        }
    }
complete:
    if (gameplay_receipt.status) gameplay_receipt.completed_tick_ms = GetTickCount64();
done:
    ReleaseSRWLockExclusive(&gameplay_lock);
}

// Modifies: validates and stores a pending Fang gameplay request, clearing its tracked outputs.
int fang_control_gameplay_submit(uint32_t request_id, uint32_t kind, float x, float y, float z, uint32_t timeout_ms, uint32_t hold_ms, uint32_t hero_slot, uint32_t ability_id) {
    int is_accepted = 0;
    fang_control_snapshot boundary;
    if (!InterlockedCompareExchange(&is_gameplay_installed, 0, 0) || !request_id || kind > 6 ||
        (kind == 4 ? hero_slot > 2 : hero_slot != 0) ||
        (kind == 5 ? (ability_id != 0x43C6B5F7 && ability_id != 0xF0D49F1D) : ability_id != 0) ||
        (kind == 3 ? (!hold_ms || hold_ms > 1200) : hold_ms != 0) ||
        !timeout_ms || timeout_ms > 2000 || !is_gameplay_float_finite(&x) ||
        !is_gameplay_float_finite(&y) || !is_gameplay_float_finite(&z)) return 0;
    fang_control_read(&boundary);
    AcquireSRWLockExclusive(&gameplay_lock);
    if (!gameplay_request || gameplay_receipt.status) {
        gameplay_request = request_id;
        gameplay_kind = kind;
        gameplay_hold_ms = hold_ms;
        gameplay_hero_slot = hero_slot;
        gameplay_ability_id = ability_id;
        gameplay_request_scene = boundary.scene_load_return_count;
        gameplay_point[0] = x; gameplay_point[1] = y; gameplay_point[2] = z;
        gameplay_deadline = GetTickCount64() + timeout_ms;
        memset(&gameplay_receipt, 0, sizeof(gameplay_receipt));
        memset(&gameplay_state, 0, sizeof(gameplay_state));
        memset(&gameplay_projection, 0, sizeof(gameplay_projection));
        memset(&gameplay_click, 0, sizeof(gameplay_click));
        memset(&gameplay_hero, 0, sizeof(gameplay_hero));
        gameplay_hero.requested_slot = hero_slot;
        memset(&gameplay_cast, 0, sizeof(gameplay_cast));
        gameplay_cast.action_index = 2;
        is_gameplay_input_started = 0;
        gameplay_receipt.request_id = request_id;
        is_accepted = 1;
    }
    ReleaseSRWLockExclusive(&gameplay_lock);
    return is_accepted;
}

// Reads/modifies: copies the matching Fang receipt and gameplay snapshots into caller outputs.
int fang_control_gameplay_result(uint32_t request_id, fang_control_window_receipt* receipt,
    fang_control_gameplay_state* snapshot, fang_control_projection* projection, fang_control_gameplay_click* click,
    fang_control_hero_selection* selection, fang_control_active_cast* cast) {
    int is_found;
    if (!receipt || !snapshot || !projection || !click || !selection || !cast) return 0;
    AcquireSRWLockShared(&gameplay_lock);
    is_found = request_id && gameplay_request == request_id;
    if (is_found) { *receipt = gameplay_receipt; *snapshot = gameplay_state; *projection = gameplay_projection; *click = gameplay_click; *selection = gameplay_hero; *cast = gameplay_cast; }
    ReleaseSRWLockShared(&gameplay_lock);
    return is_found;
}

// Modifies: cancels matching Fang synthetic operations and records terminal receipts and snapshots.
void fang_control_gameplay_cancel(uint32_t request_id) {
    AcquireSRWLockExclusive(&gameplay_lock);
    if (gameplay_request == request_id && !gameplay_receipt.status) {
        if (gameplay_kind == 5) {
            if (is_gameplay_input_started) {
                fang_control_ability_cancel(request_id);
                fang_control_ability_frame(request_id, &gameplay_cast);
            }
            gameplay_receipt.status = gameplay_cast.is_invocation_attempted ? 6 : 3;
            gameplay_receipt.completed_tick_ms = GetTickCount64();
            ReleaseSRWLockExclusive(&gameplay_lock);
            return;
        }
        if (gameplay_kind == 4) {
            if (is_gameplay_input_started) {
                fang_control_hero_cancel(request_id);
                fang_control_hero_frame(request_id, &gameplay_hero);
            }
            gameplay_receipt.status = gameplay_hero.is_invocation_attempted ? 6 : 3;
            gameplay_receipt.completed_tick_ms = GetTickCount64();
            ReleaseSRWLockExclusive(&gameplay_lock);
            return;
        }
        if (is_gameplay_input_started) {
            fang_control_input_cancel(request_id);
            fang_control_input_frame(request_id, gameplay_request_scene, gameplay_state.controlled_handle,
                gameplay_state.is_world_active, &gameplay_click);
        }
        gameplay_receipt.status = gameplay_click.is_press_poll_observed ? 6 : 3;
        gameplay_receipt.completed_tick_ms = GetTickCount64();
    }
    ReleaseSRWLockExclusive(&gameplay_lock);
}
#endif

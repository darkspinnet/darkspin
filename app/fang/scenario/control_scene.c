#include "hook.h"
#include "scenario/control.h"
#if FANG_SCENARIO
#include <string.h>

/* These are the existing ship navigation UI's request/lifetime boundaries,
 * not the weak Flash callback ABI. No game resource or UI default is changed. */
typedef int (FANG_THISCALL* scene_init_fn)(void*);
typedef void (FANG_THISCALL* scene_close_fn)(void*);
typedef unsigned char (FANG_THISCALL* scene_request_fn)(void*, unsigned char, int, int);
typedef int (FANG_THISCALL* scene_room_fn)(void*);
typedef void* (__cdecl* scene_lookup_fn)(void);
typedef unsigned int (__cdecl* scene_progress_fn)(void);
typedef int (__cdecl* scene_mode_fn)(void);
typedef unsigned char (FANG_THISCALL* scene_playing_fn)(void*);
typedef void* (__cdecl* scene_spline_lookup_fn)(uint32_t);
typedef unsigned char (__cdecl* scene_spline_start_fn)(void*);

static CRITICAL_SECTION scene_lock;
static volatile LONG is_scene_lock_initialized;
static BYTE* scene_image;
static void* navigation;
static DWORD navigation_thread;
static int is_scene_installed;
static scene_init_fn original_scene_init;
static scene_close_fn original_scene_close;
static scene_request_fn original_scene_request;
static scene_room_fn current_scene_room;
static scene_lookup_fn scene_controller, scene_camera, scene_movie;
static scene_progress_fn scene_progress;
static scene_mode_fn scene_mode;
static scene_spline_lookup_fn original_spline_lookup;
static scene_spline_start_fn original_spline_start;
static volatile LONG movie_observed_at;

// Reads: returns whether Fang observed native movie progress recently.
int is_scenario_movie_observed(void) {
    DWORD stamp = (DWORD)InterlockedCompareExchange(&movie_observed_at, 0, 0);
    return stamp != 0 && (DWORD)(GetTickCount() - stamp) < 1500;
}

/* Observe the exact native calls; retain their return values and last error.
 * These hooks never substitute a marker, advance a spline or replay a start. */
// Reads/modifies: forwards the native spline lookup and logs its token/result while preserving LastError.
static void* __cdecl observe_spline_lookup(uint32_t token) {
    void* spline = original_spline_lookup(token);
    DWORD last_error = GetLastError();
    trace_client_state("scenario_cinematic_requested_spline", token);
    trace_client_state("scenario_cinematic_spline_found", spline != NULL);
    SetLastError(last_error);
    return spline;
}

// Modifies: forwards native spline activation and logs the call while preserving its result and LastError.
static unsigned char __cdecl observe_spline_start(void* spline) {
    unsigned char result = original_spline_start(spline);
    DWORD last_error = GetLastError();
    trace_client_state("scenario_cinematic_spline_started", 1);
    SetLastError(last_error);
    return result;
}

// Reads/modifies: copies native movie/decoder state for logging and updates Fang's movie-observation timestamp.
static void observe_movie_progress(void* movie) {
    /* Build 103: 79AAC0 tests manager+56 only. 79B610 advances +104;
     * the secondary draw interface at manager+4 enters 79AC70, sets +812,
     * computes fade alpha from +68/+72/+104 and draws through 79D900.
     * Decoder +336 is its state, +252 target frame, +248 uploaded frame,
     * +180 frame count and +184 current decoded frame (whose +52 is index).
     * Copies are best-effort observations, never a decoder call or a write. */
    unsigned char manager_bytes[816];
    unsigned char decoder_bytes[340];
    unsigned int decoder, frame, frame_index;
    SIZE_T copied = 0;
    if (!ReadProcessMemory(GetCurrentProcess(), movie, manager_bytes, sizeof(manager_bytes), &copied) ||
        copied != sizeof(manager_bytes)) return;
    InterlockedExchange(&movie_observed_at, (LONG)GetTickCount());
    trace_client_state("scenario_movie_draw_seen", manager_bytes[812] != 0);
    trace_client_state("scenario_movie_elapsed_bits", *(unsigned int*)(manager_bytes + 104));
    trace_client_state("scenario_movie_fadein_bits", *(unsigned int*)(manager_bytes + 68));
    trace_client_state("scenario_movie_fadeout_bits", *(unsigned int*)(manager_bytes + 72));
    decoder = *(unsigned int*)(manager_bytes + 56);
    if (!decoder) return;
    if (!ReadProcessMemory(GetCurrentProcess(), (void*)(uintptr_t)decoder, decoder_bytes, sizeof(decoder_bytes), &copied) ||
        copied != sizeof(decoder_bytes)) return;
    trace_client_state("scenario_movie_decoder_state", *(unsigned int*)(decoder_bytes + 336));
    trace_client_state("scenario_movie_target_frame", *(unsigned int*)(decoder_bytes + 252));
    trace_client_state("scenario_movie_uploaded_frame", *(unsigned int*)(decoder_bytes + 248));
    trace_client_state("scenario_movie_total_frames", *(unsigned int*)(decoder_bytes + 180));
    frame = *(unsigned int*)(decoder_bytes + 184);
    if (frame && frame <= UINTPTR_MAX - 52 &&
        ReadProcessMemory(GetCurrentProcess(), (void*)(uintptr_t)(frame + 52), &frame_index, sizeof(frame_index), &copied) &&
        copied == sizeof(frame_index)) trace_client_state("scenario_movie_decoded_frame", frame_index);
}

#if defined(__GNUC__)
#define SCENE_FASTCALL __attribute__((fastcall))
#else
#define SCENE_FASTCALL __fastcall
#endif

// Modifies: runs native navigation initialization and records Fang controller/thread ownership.
static int SCENE_FASTCALL capture_scene_navigation(void* controller, void* ignored) {
    int result = original_scene_init(controller);
    DWORD last_error = GetLastError();
    (void)ignored;
    EnterCriticalSection(&scene_lock);
    navigation = controller;
    navigation_thread = GetCurrentThreadId();
    LeaveCriticalSection(&scene_lock);
    SetLastError(last_error);
    return result;
}

// Modifies: invalidates matching Fang navigation ownership and runs native closure with its effects.
static void SCENE_FASTCALL release_scene_navigation(void* controller, void* ignored) {
    DWORD last_error = GetLastError();
    (void)ignored;
    EnterCriticalSection(&scene_lock);
    if (navigation == controller) {
        navigation = NULL;
        navigation_thread = 0;
    }
    LeaveCriticalSection(&scene_lock);
    SetLastError(last_error);
    original_scene_close(controller);
}

// Modifies: binds native scene helpers, patches navigation/spline calls, and installs/logs editor observation.
int fang_control_scene_install(HMODULE executable) {
    static const BYTE create_call[] = {0xE8,0x41,0xFB,0xFF,0xFF};
    static const BYTE close_call[] = {0xE8,0x90,0xFB,0xFF,0xFF};
    static const BYTE request_entry[] = {0x6A,0xFF,0x68,0x88,0x53,0xEA,0x00};
    BYTE* base = (BYTE*)executable;
    if (!base || memcmp(base + 0x12D5CA, create_call, sizeof(create_call)) != 0 ||
        memcmp(base + 0x12D5EB, close_call, sizeof(close_call)) != 0 ||
        memcmp(base + 0x12CB60, request_entry, sizeof(request_entry)) != 0) return 0;
    if (InterlockedCompareExchange(&is_scene_lock_initialized, 0, 0)) return 0;
    if (!InitializeCriticalSectionAndSpinCount(&scene_lock, 4000)) return 0;
    InterlockedExchange(&is_scene_lock_initialized, 1);
    scene_image = base;
    original_scene_init = (scene_init_fn)(base + 0x12D110);
    original_scene_close = (scene_close_fn)(base + 0x12D180);
    original_scene_request = (scene_request_fn)(base + 0x12CB60);
    current_scene_room = (scene_room_fn)(base + 0x3A29C0);
    scene_controller = (scene_lookup_fn)(base + 0x12C0D0);
    scene_camera = (scene_lookup_fn)(base + 0x619970);
    scene_movie = (scene_lookup_fn)(base + 0x3B6FD0);
    scene_progress = (scene_progress_fn)(base + 0x1187D0);
    scene_mode = (scene_mode_fn)(base + 0xE5030);
    original_spline_lookup = (scene_spline_lookup_fn)(base + 0x619D70);
    original_spline_start = (scene_spline_start_fn)(base + 0x61B810);
    /* Build-103 disassembly: CALL A19D70 at 52CAC0, CALL A1B810 at 52CACD. */
    if (!patch_call(base + 0x12CAC0, (void*)original_spline_lookup, (void*)observe_spline_lookup)) return 0;
    if (!patch_call(base + 0x12CACD, (void*)original_spline_start, (void*)observe_spline_start)) return 0;
    if (!patch_call(base + 0x12D5CA, (void*)original_scene_init, (void*)capture_scene_navigation)) return 0;
    if (!patch_call(base + 0x12D5EB, (void*)original_scene_close, (void*)release_scene_navigation)) return 0;
    EnterCriticalSection(&scene_lock);
    is_scene_installed = 1;
    LeaveCriticalSection(&scene_lock);
    trace_client_state("scenario_editor_hook", (unsigned int)fang_control_editor_install(executable));
    return 1;
}

/* Caller owns scene_lock throughout all use of the returned controller. */
// Reads/modifies: checks native navigation/cinematic gates and fills controller/current-room output slots.
static int eligible_scene_controller(uint32_t room, BYTE** controller_out, int* current_room_out) {
    BYTE* nav;
    BYTE* controller;
    void* movie;
    DWORD thread;
    int is_available;
    int current_room;
    *controller_out = NULL;
    *current_room_out = 0;
    if (room < 1 || room > 3) return 0;
    nav = (BYTE*)navigation;
    thread = navigation_thread;
    is_available = is_scene_installed;
    if (!is_available || !nav || thread != GetCurrentThreadId() || !readable_range(nav, 48) ||
        *(void**)nav != scene_image + 0xBE17DC || !nav[40] || !nav[42] ||
        scene_progress() < 6500 || scene_mode() != 0) return 0;
    controller = (BYTE*)scene_controller();
    if (!readable_range(controller, 20) || controller[4] || controller[13] || scene_camera() != NULL) return 0;
    current_room = current_scene_room(controller);
    if (current_room < 1 || current_room > 3) return 0;
    movie = scene_movie();
    if (movie) {
        scene_playing_fn playing = (scene_playing_fn)(*(void***)movie)[5];
        if (playing(movie)) return 0;
    }
    *controller_out = controller;
    *current_room_out = current_room;
    return 1;
}

/* Called only by the owned HWND owner callback. Return 1 only for the actual
 * native request's accepted AL; 2 for native rejection, 5 for unavailable UI.
 * The host separately waits for a fresh client report and native readiness. */
// Reads/modifies: validates native navigation, invokes an eligible room transition, and logs acceptance/rejection.
static uint32_t validate_and_request_scene(uint32_t room) {
    BYTE* controller;
    int current_room;
    unsigned char is_accepted;
    if (!eligible_scene_controller(room, &controller, &current_room)) return 5;
    /* The Flash handler itself rejects a no-op. Do not synthesize acceptance
     * or call the native request when the requested room is already current. */
    if (current_room == (int)room) return 2;
    is_accepted = original_scene_request(controller, 0, (int)room, 0);
    trace_client_state(is_accepted ? "scenario_scene_requested" : "scenario_scene_rejected", room);
    return is_accepted ? 1 : 2;
}

// Modifies: serializes a validated native room-transition request through the Fang lifetime lock.
uint32_t fang_control_scene_request(uint32_t room) {
    uint32_t status;
    if (!InterlockedCompareExchange(&is_scene_lock_initialized, 0, 0)) return 5;
    /* Foreign-thread teardown waits for all guard reads and the native request.
     * Same-owner reentrant unregister can clear the pointer without deadlock.
     * No navigation/controller pointer is used after the native request. */
    EnterCriticalSection(&scene_lock);
    status = validate_and_request_scene(room);
    LeaveCriticalSection(&scene_lock);
    return status;
}

/* Build 103 only. A9's 449090 -> 52C8A0 selects controller mode 4; A19D70
 * resolves the authored spline token and A1B810 activates it. 52C140 sets the
 * finished pulse consumed by 448F70 before ship voting. Sample these boundaries
 * on the owned HWND thread without advancing, skipping or changing presentation.
 * A missing finished pulse in one sample is not evidence of a stuck gate. */
// Reads/modifies: fills the cinematic snapshot from native state and logs movie progress when playing.
uint32_t fang_control_scene_snapshot(fang_control_cinematic_snapshot* snapshot) {
    BYTE* controller;
    void* movie;
    BYTE* camera;
    void** methods;
    scene_playing_fn playing;
    if (!snapshot) return 5;
    memset(snapshot, 0, sizeof(*snapshot));
    if (!InterlockedCompareExchange(&is_scene_lock_initialized, 0, 0)) return 1;
    EnterCriticalSection(&scene_lock);
    if (!is_scene_installed) {
        LeaveCriticalSection(&scene_lock);
        return 1;
    }
    controller = (BYTE*)scene_controller();
    if (!readable_range(controller, 20)) {
        LeaveCriticalSection(&scene_lock);
        return 1;
    }
    snapshot->mode = *(uint32_t*)controller;
    snapshot->game_mode = (uint32_t)scene_mode();
    snapshot->current_room = (uint32_t)current_scene_room(controller);
    snapshot->is_active = controller[4] != 0;
    snapshot->is_skippable = controller[5] != 0;
    snapshot->is_finished_pulse = controller[12] != 0;
    snapshot->is_transitioning = controller[13] != 0;
    snapshot->next_mode = *(uint32_t*)(controller + 16);
    camera = (BYTE*)scene_camera();
    snapshot->is_camera_present = camera != NULL;
    if (readable_range(camera, 144)) {
        snapshot->fields |= 1;
        snapshot->active_spline_token = *(uint32_t*)(camera + 140);
    }
    movie = scene_movie();
    snapshot->is_movie_present = movie != NULL;
    if (movie) {
        if (!readable_range(movie, sizeof(void*))) {
            LeaveCriticalSection(&scene_lock);
            return 1;
        }
        methods = *(void***)movie;
        if (!readable_range(methods, 6 * sizeof(void*))) {
            LeaveCriticalSection(&scene_lock);
            return 1;
        }
        playing = (scene_playing_fn)methods[5];
        if (!playing || !readable_range((void*)playing, 1)) {
            LeaveCriticalSection(&scene_lock);
            return 1;
        }
        snapshot->is_movie_playing = playing(movie) != 0;
        if (snapshot->is_movie_playing) observe_movie_progress(movie);
    }
    snapshot->is_available = 1;
    LeaveCriticalSection(&scene_lock);
    return 1;
}

// Reads: checks native room or editor readiness without issuing a transition request.
uint32_t fang_control_scene_ready(uint32_t room) {
    BYTE* controller;
    int current_room;
    uint32_t status = 5;
    if (room == 3) return fang_control_editor_ready();
    if (!InterlockedCompareExchange(&is_scene_lock_initialized, 0, 0)) return 5;
    EnterCriticalSection(&scene_lock);
    if (eligible_scene_controller(room, &controller, &current_room) && current_room == (int)room) status = 1;
    /* No request, input, selected-room write, or transition is performed. */
    LeaveCriticalSection(&scene_lock);
    return status;
}
#endif

#include "hook.h"
#include "control.h"
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

#if defined(__GNUC__)
#define SCENE_FASTCALL __attribute__((fastcall))
#else
#define SCENE_FASTCALL __fastcall
#endif

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
    if (!patch_call(base + 0x12D5CA, (void*)original_scene_init, (void*)capture_scene_navigation)) return 0;
    if (!patch_call(base + 0x12D5EB, (void*)original_scene_close, (void*)release_scene_navigation)) return 0;
    EnterCriticalSection(&scene_lock);
    is_scene_installed = 1;
    LeaveCriticalSection(&scene_lock);
    trace_client_state("scenario_editor_hook", (unsigned int)fang_control_editor_install(executable));
    return 1;
}

/* Caller owns scene_lock throughout all use of the returned controller. */
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

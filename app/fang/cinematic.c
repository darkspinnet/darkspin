#include "hook.h"
#include "cinematic.h"
#include <string.h>
#if !defined(__GNUC__)
#include <intrin.h>
#endif

/* Production compatibility uses only the existing native Escape decision.
 * No synthetic window messages, held-key state, camera methods, or movie-stop
 * methods are invoked here. Each original query executes exactly once. */
#if defined(__GNUC__)
typedef int (__attribute__((thiscall))* cinematic_key_fn)(void*, int);
typedef unsigned char (__attribute__((thiscall))* cinematic_playing_fn)(void*);
#define CINEMATIC_FASTCALL __attribute__((fastcall))
#define CINEMATIC_CALLER() __builtin_return_address(0)
#else
typedef int (__thiscall* cinematic_key_fn)(void*, int);
typedef unsigned char (__thiscall* cinematic_playing_fn)(void*);
#define CINEMATIC_FASTCALL __fastcall
#define CINEMATIC_CALLER() _ReturnAddress()
#endif
typedef void* (__cdecl* cinematic_lookup_fn)(void);

static cinematic_key_fn original_key;
static cinematic_lookup_fn movie_lookup;
static cinematic_lookup_fn camera_lookup;
static BYTE* image;
static volatile LONG setting_flags;
static volatile LONG is_installed;

static int cinematic_is_playing(void) {
    void* movie = movie_lookup();
    cinematic_playing_fn playing;
    if (!movie) return 0;
    playing = (cinematic_playing_fn)(*(void***)movie)[5];
    return playing(movie) != 0;
}

static int CINEMATIC_FASTCALL cinematic_key(void* input, void* ignored, int key) {
    void* caller = CINEMATIC_CALLER();
    int is_pressed = original_key(input, key);
    DWORD last_error = GetLastError();
    LONG flags;
    (void)ignored;
    if (key != 27 || is_pressed) return is_pressed;
    flags = InterlockedCompareExchange(&setting_flags, 0, 0);
    if (caller == image + 0x48EC0 && (flags & 1) && cinematic_is_playing()) {
        /* This exact query exists only in native opening state 1. Native
         * playback completion/transition remains with its original caller. */
        is_pressed = 1;
        trace_client_state("cinematic_escape_intro", 1);
    } else if (caller == image + 0x12C1E0 && (flags & 2)) {
        /* The caller reached this query only through its own skippable-byte
         * gate. Require an actual current camera/movie as well. Returning one
         * Escape edge enters its ordinary finite cleanup immediately; nothing
         * is queued or retained that can become a later menu Escape. */
        if (camera_lookup() != NULL || cinematic_is_playing()) {
            is_pressed = 1;
            trace_client_state("cinematic_escape_in_game", 1);
        }
    }
    SetLastError(last_error);
    return is_pressed;
}

int fang_cinematic_install(HMODULE executable, int is_intro_skipped, int is_cinematic_skipped) {
    static const BYTE opening_query[] = {0x8B,0x17,0x8B,0x42,0x34,0x6A,0x1B,0x8B,0xCF,0xFF,0xD0};
    static const BYTE camera_query[] = {0x8B,0x55,0x00,0x8B,0x42,0x34,0x6A,0x1B,0x8B,0xCD,0xFF,0xD0};
    void** slot;
    if (!executable || InterlockedCompareExchange(&is_installed, 0, 0)) return 0;
    image = (BYTE*)executable;
    slot = (void**)(image + 0xBDE46C);
    if (memcmp(image + 0x48EB5, opening_query, sizeof(opening_query)) != 0 ||
        memcmp(image + 0x12C1D4, camera_query, sizeof(camera_query)) != 0 ||
        *slot != image + 0x10B310) return 0;
    original_key = (cinematic_key_fn)(image + 0x10B310);
    movie_lookup = (cinematic_lookup_fn)(image + 0x3B6FD0);
    camera_lookup = (cinematic_lookup_fn)(image + 0x619970);
    InterlockedExchange(&setting_flags, (is_intro_skipped ? 1 : 0) | (is_cinematic_skipped ? 2 : 0));
    if (!patch_pointer(slot, (void*)original_key, (void*)cinematic_key)) return 0;
    InterlockedExchange(&is_installed, 1);
    trace_client_state("cinematic_escape_flags", (unsigned int)InterlockedCompareExchange(&setting_flags, 0, 0));
    return 1;
}

int fang_cinematic_configure(uint32_t mask, uint32_t flags, fang_cinematic_setting* setting) {
    LONG previous, next;
    if (!setting || !mask || (mask & ~3u) || (flags & ~mask) ||
        !InterlockedCompareExchange(&is_installed, 0, 0)) return 0;
    do {
        previous = InterlockedCompareExchange(&setting_flags, 0, 0);
        next = (previous & ~(LONG)mask) | (LONG)flags;
    } while (InterlockedCompareExchange(&setting_flags, next, previous) != previous);
    setting->flags = (uint32_t)next;
    setting->thread_id = GetCurrentThreadId();
    setting->completed_tick_ms = GetTickCount64();
    trace_client_state("cinematic_escape_flags", (unsigned int)next);
    return 1;
}

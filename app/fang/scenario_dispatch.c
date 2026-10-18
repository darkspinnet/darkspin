#include "scenario_dispatch.h"

#if FANG_SCENARIO
#include "hook.h"

// The supported callees each take one stack pointer and return with ret 4.
// Their incoming ECX is not an argument. Keep the original EAX return bits;
// neither return is an authenticated object identity or success assertion.
typedef void* (WINAPI* scenario_object_create_fn)(void* stream);
typedef int (WINAPI* scenario_object_delete_fn)(void* stream);

static scenario_object_create_fn original_scenario_object_create;
static scenario_object_delete_fn original_scenario_object_delete;
// This process-lifetime ordinal wraps; it is not a session/lifecycle epoch.
// Missing enter/return rows remain ambiguous when recorder output is lost.
static volatile LONG dispatch_observation_sequence;
static volatile LONG observed_frame_thread;

static void* WINAPI observe_scenario_object_create(void* stream) {
    DWORD incoming_last_error = GetLastError();
    unsigned int sequence = (unsigned int)InterlockedIncrement(
        &dispatch_observation_sequence);
    void* result;
    DWORD last_error;
    trace_client_state("scenario_object_create_enter", sequence);
    SetLastError(incoming_last_error);
    result = original_scenario_object_create(stream);
    last_error = GetLastError();
    trace_client_state("scenario_object_create_return", sequence);
    SetLastError(last_error);
    return result;
}

static int WINAPI observe_scenario_object_delete(void* stream) {
    DWORD incoming_last_error = GetLastError();
    unsigned int sequence = (unsigned int)InterlockedIncrement(
        &dispatch_observation_sequence);
    int result;
    DWORD last_error;
    trace_client_state("scenario_object_delete_enter", sequence);
    SetLastError(incoming_last_error);
    result = original_scenario_object_delete(stream);
    last_error = GetLastError();
    trace_client_state("scenario_object_delete_return", sequence);
    SetLastError(last_error);
    return result;
}

void observe_scenario_dispatch_frame(void) {
    DWORD last_error = GetLastError();
    DWORD thread = GetCurrentThreadId();
    LONG previous_thread = InterlockedExchange(&observed_frame_thread, (LONG)thread);
    if ((DWORD)previous_thread != thread) {
        // The existing trace metadata records this callback's current thread
        // and frame. A zero previous ID denotes its first observed owner.
        trace_client_state("scenario_frame_thread", (unsigned int)previous_thread);
    }
    SetLastError(last_error);
}

int install_scenario_dispatch_observation(void* executable) {
    DWORD last_error = GetLastError();
    unsigned char* base = (unsigned char*)executable;
    int is_create_hooked = 0;
    int is_delete_hooked = 0;
    if (base != NULL) {
        original_scenario_object_create =
            (scenario_object_create_fn)(base + 0x13A760);
        original_scenario_object_delete =
            (scenario_object_delete_fn)(base + 0x138FD0);
        is_create_hooked = patch_call(base + 0x13AE33,
            (void*)original_scenario_object_create,
            (void*)observe_scenario_object_create);
        is_delete_hooked = patch_call(base + 0x13AE4D,
            (void*)original_scenario_object_delete,
            (void*)observe_scenario_object_delete);
    }
    trace_client_state("scenario_object_create_hook", (unsigned int)is_create_hooked);
    trace_client_state("scenario_object_delete_hook", (unsigned int)is_delete_hooked);
    trace_client_state("scenario_dispatch_hook_count",
        (unsigned int)(is_create_hooked + is_delete_hooked));
    SetLastError(last_error);
    return is_create_hooked && is_delete_hooked;
}
#endif

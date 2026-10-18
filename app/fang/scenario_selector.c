#include "scenario_selector.h"

#if FANG_SCENARIO
#include "hook.h"

// ECX carries the opaque context; the original pops one 32-bit stack key.
// Only AL has proved scalar meaning. Use EAX as an unchanged raw-bit carrier.
typedef unsigned int (FANG_THISCALL* scenario_selector_lookup_fn)(
    void* lookup_context, unsigned int key);

static scenario_selector_lookup_fn original_scenario_selector_lookup;

static unsigned int FANG_THISCALL observe_scenario_selector_lookup(
    void* lookup_context, unsigned int key) {
    unsigned int result = original_scenario_selector_lookup(lookup_context, key);
    DWORD last_error = GetLastError();
    if (key == 0xE8369A08u) {
        trace_client_state("scenario_selector_override_e8369a08", result);
    } else if (key == 0xF342F8ACu) {
        trace_client_state("scenario_selector_override_f342f8ac", result);
    }
    SetLastError(last_error);
    return result;
}

int install_scenario_selector_observation(void* executable) {
    DWORD last_error = GetLastError();
    unsigned char* base = (unsigned char*)executable;
    int is_hooked = 0;
    if (base != NULL) {
        original_scenario_selector_lookup =
            (scenario_selector_lookup_fn)(base + 0x3B6600);
        is_hooked = patch_call(base + 0x5D5DDB,
            (void*)original_scenario_selector_lookup,
            (void*)observe_scenario_selector_lookup);
    }
    trace_client_state("scenario_selector_override_hook", (unsigned int)is_hooked);
    SetLastError(last_error);
    return is_hooked;
}
#endif

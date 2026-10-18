#include "scenario_alternate.h"
#if FANG_SCENARIO
#include "hook.h"
#include <cpuid.h>
#include <string.h>

#define SCENARIO_ALTERNATE_CALL_LIMIT 4096
#define SCENARIO_ALTERNATE_XSAVE 1u
#define SCENARIO_ALTERNATE_LEGACY 2u

void* scenario_alternate_original;
uint32_t scenario_alternate_mode;
uint32_t scenario_alternate_mask_low;
uint32_t scenario_alternate_mask_high;
uint32_t scenario_alternate_state_size;
const uint32_t scenario_alternate_default_mxcsr = 0x1f80;
volatile LONG scenario_alternate_state_loss;

static volatile LONG alternate_call_count;
static volatile LONG alternate_limit_reported;
static volatile LONG alternate_install_attempted;

static uint32_t reserve_alternate_call(void) {
    LONG observed = InterlockedCompareExchange(&alternate_call_count, 0, 0);
    while (observed < SCENARIO_ALTERNATE_CALL_LIMIT) {
        LONG previous = InterlockedCompareExchange(
            &alternate_call_count, observed + 1, observed);
        if (previous == observed) {
            return (uint32_t)(observed + 1);
        }
        observed = previous;
    }
    return 0;
}

void __cdecl scenario_alternate_before(ScenarioAlternateFrame* frame, const void* entry,
    uint32_t raw_marker_set_reference, int32_t entry_ordinal) {
    DWORD last_error = GetLastError();
    ScenarioAlternateSelectionEvent* req = &frame->EventArea.Event;
    frame->IncomingLastError = last_error;
    memset(req, 0, sizeof(*req));
    req->CallOrdinal = reserve_alternate_call();
    req->LossFlags = (uint32_t)InterlockedCompareExchange(
        &scenario_alternate_state_loss, 0, 0);
    if (req->CallOrdinal == 0) {
        if (InterlockedCompareExchange(&alternate_limit_reported, 1, 0) == 0) {
            req->Phase = SCENARIO_ALTERNATE_LIMIT;
            trace_scenario_alternate_selection(req);
        }
        SetLastError(last_error);
        return;
    }
    req->RawMarkerSetReference = raw_marker_set_reference;
    req->EntryOrdinal = entry_ordinal;
    copy_scenario_authored_input(entry, raw_marker_set_reference, &req->Input);
    req->Phase = SCENARIO_ALTERNATE_ENTRY;
    trace_scenario_alternate_selection(req);
    SetLastError(last_error);
}

void __cdecl scenario_alternate_after(ScenarioAlternateFrame* frame) {
    DWORD last_error = GetLastError();
    ScenarioAlternateSelectionEvent* req = &frame->EventArea.Event;
    frame->ReturnedLastError = last_error;
    req->Phase = SCENARIO_ALTERNATE_NORMAL_RETURN;
    req->LossFlags = (uint32_t)InterlockedCompareExchange(
        &scenario_alternate_state_loss, 0, 0);
    // Reuse the owned pre-call copy; never read or interpret returned EAX.
    trace_scenario_alternate_selection(req);
    SetLastError(last_error);
}

static int prepare_alternate_state(void) {
    SYSTEM_INFO system;
    unsigned int eax, ebx, ecx, edx;
    uint32_t mask_low, mask_high;
    uint32_t supported_low, supported_high;
    uint32_t state_size;
    unsigned int maximum_leaf = __get_cpuid_max(0, NULL);
    GetSystemInfo(&system);
    if (system.dwPageSize != 4096 ||
        !IsProcessorFeaturePresent(PF_XMMI_INSTRUCTIONS_AVAILABLE) ||
        maximum_leaf < 1 || !__get_cpuid(1, &eax, &ebx, &ecx, &edx) ||
        (edx & 0x03000000u) != 0x03000000u) {
        return 0;
    }
    if ((ecx & 0x08000000u) == 0) {
        scenario_alternate_mode = SCENARIO_ALTERNATE_LEGACY;
        scenario_alternate_mask_low = 3;
        scenario_alternate_mask_high = 0;
        scenario_alternate_state_size = 512;
        return 1;
    }
    if ((ecx & 0x04000000u) == 0 || maximum_leaf < 0x0d) {
        return 0;
    }
    // XGETBV is executed only after the OSXSAVE bit establishes its support.
    __asm__ volatile("xgetbv" : "=a"(mask_low), "=d"(mask_high) : "c"(0));
    __cpuid_count(0x0d, 0, supported_low, state_size, ecx, supported_high);
    if ((mask_low & 3) != 3 || (mask_low & ~supported_low) != 0 ||
        (mask_high & ~supported_high) != 0 || state_size < 576 ||
        state_size > 8192) {
        return 0;
    }
    scenario_alternate_mode = SCENARIO_ALTERNATE_XSAVE;
    scenario_alternate_mask_low = mask_low;
    scenario_alternate_mask_high = mask_high;
    scenario_alternate_state_size = state_size;
    return 1;
}

int install_scenario_alternate_observation(void* executable) {
    DWORD last_error = GetLastError();
    unsigned char* base = (unsigned char*)executable;
    int is_hooked = 0;
    int is_state_supported = 0;
    if (InterlockedCompareExchange(&alternate_install_attempted, 1, 0) == 0 &&
        base != NULL) {
        is_state_supported = prepare_alternate_state();
        if (is_state_supported) {
            scenario_alternate_original = base + 0x5D49C0;
            is_hooked = patch_call(base + 0x5D5FCC,
                scenario_alternate_original, (void*)scenario_alternate_bridge);
        }
    }
    trace_client_state("scenario_alternate_state_supported", (unsigned int)is_state_supported);
    trace_client_state("scenario_alternate_selection_hook", (unsigned int)is_hooked);
    // Actual prepared mode/mask/size only; unsupported initial state stays zero.
    trace_client_state("scenario_alternate_state_mode", scenario_alternate_mode);
    trace_client_state("scenario_alternate_state_mask_low", scenario_alternate_mask_low);
    trace_client_state("scenario_alternate_state_mask_high", scenario_alternate_mask_high);
    trace_client_state("scenario_alternate_state_size", scenario_alternate_state_size);
    SetLastError(last_error);
    return is_hooked;
}
#endif

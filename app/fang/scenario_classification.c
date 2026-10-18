#include "scenario_classification.h"
#if FANG_SCENARIO
#include "hook.h"
#include <cpuid.h>
#include <string.h>

#define SCENARIO_CLASSIFICATION_CALL_LIMIT 4096
#define SCENARIO_CLASSIFICATION_XSAVE 1u
#define SCENARIO_CLASSIFICATION_LEGACY 2u

void* scenario_classification_original;
uint32_t scenario_classification_mode;
uint32_t scenario_classification_mask_low;
uint32_t scenario_classification_mask_high;
uint32_t scenario_classification_state_size;
const uint32_t scenario_classification_default_mxcsr = 0x1f80;
volatile LONG scenario_classification_state_loss;

static volatile LONG classification_call_count;
static volatile LONG classification_limit_reported;
static volatile LONG classification_install_attempted;

static uint32_t reserve_classification_call(void) {
    LONG observed = InterlockedCompareExchange(&classification_call_count, 0, 0);
    while (observed < SCENARIO_CLASSIFICATION_CALL_LIMIT) {
        LONG previous = InterlockedCompareExchange(
            &classification_call_count, observed + 1, observed);
        if (previous == observed) {
            return (uint32_t)(observed + 1);
        }
        observed = previous;
    }
    return 0;
}

static void copy_classification_marker(const void* entry,
    ScenarioMarkerMaskObservation* req) {
    uintptr_t address = (uintptr_t)entry;
    SIZE_T copied_count = 0;
    uint32_t input_words[9] = {0};
    BOOL is_copied;
    if (address == 0) {
        req->CopyFailure = SCENARIO_MARKER_COPY_NULL_ENTRY;
        return;
    }
    if (address > UINT32_MAX - 39u) {
        req->CopyFailure = SCENARIO_MARKER_COPY_ADDRESS_RANGE;
        return;
    }
    req->IsCopyAttempted = 1;
    is_copied = ReadProcessMemory(GetCurrentProcess(),
        (const void*)(address + 4u), input_words, sizeof(input_words), &copied_count);
    if (!is_copied) {
        req->CopyError = GetLastError();
        req->CopyByteCount = (uint32_t)copied_count;
        req->CopyFailure = SCENARIO_MARKER_COPY_READ_FAILED;
        return;
    }
    req->CopyByteCount = (uint32_t)copied_count;
    if (copied_count != sizeof(input_words)) {
        req->CopyFailure = SCENARIO_MARKER_COPY_SHORT_READ;
        return;
    }
    // Same entry-time copy: marker+4; authored position+1c/+20/+24.
    // The copied words do not establish atomicity or a world-state transform.
    req->MarkerID = input_words[0];
    req->IsMarkerIDAvailable = 1;
    req->PositionBits[0] = input_words[6];
    req->PositionBits[1] = input_words[7];
    req->PositionBits[2] = input_words[8];
    req->IsPositionAvailable = 1;
}

void __cdecl scenario_classification_before(ScenarioClassificationFrame* frame,
    const void* entry) {
    DWORD last_error = GetLastError();
    ScenarioMarkerMaskObservation* req = &frame->EventArea.Event;
    frame->IncomingLastError = last_error;
    memset(req, 0, sizeof(*req));
    req->CallOrdinal = reserve_classification_call();
    req->LossFlags = (uint32_t)InterlockedCompareExchange(
        &scenario_classification_state_loss, 0, 0);
    if (req->CallOrdinal == 0) {
        if (InterlockedCompareExchange(&classification_limit_reported, 1, 0) == 0) {
            req->Phase = SCENARIO_MARKER_MASK_LIMIT;
            trace_scenario_marker_mask(req);
        }
        SetLastError(last_error);
        return;
    }
    copy_classification_marker(entry, req);
    req->Phase = SCENARIO_MARKER_MASK_ENTRY;
    trace_scenario_marker_mask(req);
    SetLastError(last_error);
}

void __cdecl scenario_classification_after(ScenarioClassificationFrame* frame) {
    DWORD last_error = GetLastError();
    ScenarioMarkerMaskObservation* req = &frame->EventArea.Event;
    frame->ReturnedLastError = last_error;
    req->Phase = SCENARIO_MARKER_MASK_NORMAL_RETURN;
    // Saved pushad word7 is full returned EAX, not a native object pointer.
    req->RawMask = frame->ReturnRegisters[7];
    req->LossFlags = (uint32_t)InterlockedCompareExchange(
        &scenario_classification_state_loss, 0, 0);
    trace_scenario_marker_mask(req);
    SetLastError(last_error);
}

static int prepare_classification_state(void) {
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
        scenario_classification_mode = SCENARIO_CLASSIFICATION_LEGACY;
        scenario_classification_mask_low = 3;
        scenario_classification_mask_high = 0;
        scenario_classification_state_size = 512;
        return 1;
    }
    if ((ecx & 0x04000000u) == 0 || maximum_leaf < 0x0d) {
        return 0;
    }
    __asm__ volatile("xgetbv" : "=a"(mask_low), "=d"(mask_high) : "c"(0));
    __cpuid_count(0x0d, 0, supported_low, state_size, ecx, supported_high);
    if ((mask_low & 3) != 3 || (mask_low & ~supported_low) != 0 ||
        (mask_high & ~supported_high) != 0 || state_size < 576 ||
        state_size > 8192) {
        return 0;
    }
    scenario_classification_mode = SCENARIO_CLASSIFICATION_XSAVE;
    scenario_classification_mask_low = mask_low;
    scenario_classification_mask_high = mask_high;
    scenario_classification_state_size = state_size;
    return 1;
}

int install_scenario_classification_observation(void* executable) {
    DWORD last_error = GetLastError();
    unsigned char* base = (unsigned char*)executable;
    int is_hooked = 0;
    int is_state_supported = 0;
    if (InterlockedCompareExchange(&classification_install_attempted, 1, 0) == 0 &&
        base != NULL) {
        is_state_supported = prepare_classification_state();
        if (is_state_supported) {
            scenario_classification_original = base + 0x5D3720;
            is_hooked = patch_call(base + 0x5D5F76,
                scenario_classification_original, (void*)scenario_classification_bridge);
        }
    }
    trace_client_state("scenario_classification_state_supported", (unsigned int)is_state_supported);
    trace_client_state("scenario_classification_hook", (unsigned int)is_hooked);
    trace_client_state("scenario_classification_state_mode", scenario_classification_mode);
    trace_client_state("scenario_classification_state_mask_low", scenario_classification_mask_low);
    trace_client_state("scenario_classification_state_mask_high", scenario_classification_mask_high);
    trace_client_state("scenario_classification_state_size", scenario_classification_state_size);
    SetLastError(last_error);
    return is_hooked;
}
#endif

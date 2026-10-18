#include "scenario_primary.h"

#if FANG_SCENARIO
#include "hook.h"
#include <string.h>

#define SCENARIO_PRIMARY_CALL_LIMIT 4096

// Exact ECX registry + three stack arguments; the supported callee uses ret 12.
typedef uint32_t (FANG_THISCALL* scenario_primary_base_fn)(
    void* registry, const void* entry, uint32_t raw_marker_set_reference,
    int32_t entry_ordinal);

static scenario_primary_base_fn original_scenario_primary_base;
static volatile LONG primary_call_count;
static volatile LONG primary_limit_reported;

static uint32_t reserve_primary_call(void) {
    LONG observed = InterlockedCompareExchange(&primary_call_count, 0, 0);
    while (observed < SCENARIO_PRIMARY_CALL_LIMIT) {
        LONG previous = InterlockedCompareExchange(
            &primary_call_count, observed + 1, observed);
        if (previous == observed) {
            return (uint32_t)(observed + 1);
        }
        observed = previous;
    }
    return 0;
}

static uint32_t FANG_THISCALL observe_scenario_primary_base(
    void* registry, const void* entry, uint32_t raw_marker_set_reference,
    int32_t entry_ordinal) {
    DWORD incoming_last_error = GetLastError();
    ScenarioPrimaryBaseCallEvent req = {0};
    ScenarioAuthoredInputCopy copy;
    uint32_t result;
    DWORD last_error;
    req.CallOrdinal = reserve_primary_call();
    if (req.CallOrdinal == 0) {
        if (InterlockedCompareExchange(&primary_limit_reported, 1, 0) == 0) {
            req.Phase = SCENARIO_PRIMARY_BASE_LIMIT;
            trace_scenario_primary_base(&req);
        }
        SetLastError(incoming_last_error);
        return original_scenario_primary_base(
            registry, entry, raw_marker_set_reference, entry_ordinal);
    }
    req.RawMarkerSetReference = raw_marker_set_reference;
    req.EntryOrdinal = entry_ordinal;
    copy_scenario_authored_input(entry, raw_marker_set_reference, &copy);
    req.MarkerID = copy.MarkerID;
    req.RawNounReference = copy.RawNounReference;
    memcpy(req.PositionBits, copy.PositionBits, sizeof(req.PositionBits));
    memcpy(req.RotationBits, copy.RotationBits, sizeof(req.RotationBits));
    req.ScaleBits = copy.ScaleBits;
    req.CopyByteCount = copy.CopyByteCount;
    req.CopyError = copy.CopyError;
    req.IsCopyAttempted = copy.IsCopyAttempted;
    req.IsInputCopyAvailable = copy.IsInputCopyAvailable;
    req.MarkerSetName = copy.MarkerSetName;
    req.NounName = copy.NounName;
    req.IsNounAssociationRecheckAttempted = copy.IsNounAssociationRecheckAttempted;
    req.IsNounAssociationRecheckAvailable = copy.IsNounAssociationRecheckAvailable;
    req.IsNounAssociationUnchanged = copy.IsNounAssociationUnchanged;
    req.Phase = SCENARIO_PRIMARY_BASE_ENTRY;
    trace_scenario_primary_base(&req);
    SetLastError(incoming_last_error);
    result = original_scenario_primary_base(
        registry, entry, raw_marker_set_reference, entry_ordinal);
    last_error = GetLastError();
    // Original exceptions propagate naturally, leaving an unmatched entry.
    // Completion may be a later promotion; this is not fresh admission.
    req.Phase = SCENARIO_PRIMARY_BASE_NORMAL_RETURN;
    req.IsResultNonzero = result != 0;
    trace_scenario_primary_base(&req);
    SetLastError(last_error);
    return result;
}

int install_scenario_primary_observation(void* executable) {
    DWORD last_error = GetLastError();
    unsigned char* base = (unsigned char*)executable;
    int is_hooked = 0;
    if (base != NULL) {
        original_scenario_primary_base =
            (scenario_primary_base_fn)(base + 0x5D1DF0);
        is_hooked = patch_call(base + 0x5D4FA7,
            (void*)original_scenario_primary_base,
            (void*)observe_scenario_primary_base);
    }
    trace_client_state("scenario_primary_base_hook", (unsigned int)is_hooked);
    SetLastError(last_error);
    return is_hooked;
}
#endif

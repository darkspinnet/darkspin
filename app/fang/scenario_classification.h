#ifndef DARKSPIN_SCENARIO_CLASSIFICATION_H
#define DARKSPIN_SCENARIO_CLASSIFICATION_H
#ifndef FANG_SCENARIO
#define FANG_SCENARIO 0
#endif
#if FANG_SCENARIO
#include <stddef.h>
#include <stdint.h>
#include <windows.h>

enum ScenarioMarkerMaskPhase {
    SCENARIO_MARKER_MASK_ENTRY,
    SCENARIO_MARKER_MASK_NORMAL_RETURN,
    SCENARIO_MARKER_MASK_LIMIT
};

enum ScenarioMarkerCopyFailure {
    SCENARIO_MARKER_COPY_NONE,
    SCENARIO_MARKER_COPY_NULL_ENTRY,
    SCENARIO_MARKER_COPY_ADDRESS_RANGE,
    SCENARIO_MARKER_COPY_READ_FAILED,
    SCENARIO_MARKER_COPY_SHORT_READ
};

// Owned marker-linked authored input sample, not atomic/world/current membership.
// No entry/result/set/name/ordinal pointer, semantic join or completeness proof.
typedef struct ScenarioMarkerMaskObservation {
    enum ScenarioMarkerMaskPhase Phase;
    uint32_t CallOrdinal;
    uint32_t MarkerID;
    uint32_t RawMask;
    uint32_t CopyByteCount;
    DWORD CopyError;
    enum ScenarioMarkerCopyFailure CopyFailure;
    int IsCopyAttempted;
    int IsMarkerIDAvailable;
    uint32_t LossFlags;
    int IsPositionAvailable;
    uint32_t PositionBits[3];
} ScenarioMarkerMaskObservation;

typedef struct ScenarioClassificationFrame {
    unsigned char StateArea[8256];
    unsigned char Reserved0[64];
    union {
        ScenarioMarkerMaskObservation Event;
        unsigned char Bytes[384];
    } EventArea;
    DWORD IncomingLastError;
    DWORD ReturnedLastError;
    uint32_t MaskLow;
    uint32_t MaskHigh;
    uint32_t StateSize;
    uint32_t Mode;
    uintptr_t OriginalStack;
    unsigned char Reserved1[36];
    uint32_t ReturnRegisters[9];
    unsigned char Reserved2[376];
    uint32_t InputRegisters[9];
} ScenarioClassificationFrame;

_Static_assert(sizeof(void*) == 4, "classification bridge requires x86");
_Static_assert(sizeof(ScenarioMarkerMaskObservation) <= 384, "event bound");
_Static_assert(sizeof(ScenarioMarkerMaskObservation) == 56, "observation size");
_Static_assert(offsetof(ScenarioMarkerMaskObservation, IsPositionAvailable) == 40, "position availability");
_Static_assert(offsetof(ScenarioMarkerMaskObservation, PositionBits) == 44, "position words");
_Static_assert(sizeof(ScenarioClassificationFrame) == 9216, "frame bound");
_Static_assert(offsetof(ScenarioClassificationFrame, EventArea) == 8320, "event offset");
_Static_assert(offsetof(ScenarioMarkerMaskObservation, CallOrdinal) == 4, "ordinal offset");
_Static_assert(offsetof(ScenarioClassificationFrame, IncomingLastError) == 8704, "incoming error");
_Static_assert(offsetof(ScenarioClassificationFrame, ReturnedLastError) == 8708, "returned error");
_Static_assert(offsetof(ScenarioClassificationFrame, MaskLow) == 8712, "mask low");
_Static_assert(offsetof(ScenarioClassificationFrame, MaskHigh) == 8716, "mask high");
_Static_assert(offsetof(ScenarioClassificationFrame, StateSize) == 8720, "state size");
_Static_assert(offsetof(ScenarioClassificationFrame, Mode) == 8724, "mode");
_Static_assert(offsetof(ScenarioClassificationFrame, OriginalStack) == 8728, "parent stack");
_Static_assert(offsetof(ScenarioClassificationFrame, ReturnRegisters) == 8768, "returned registers");
_Static_assert(offsetof(ScenarioClassificationFrame, InputRegisters) == 9180, "input registers");

extern void* scenario_classification_original;
extern uint32_t scenario_classification_mode;
extern uint32_t scenario_classification_mask_low;
extern uint32_t scenario_classification_mask_high;
extern uint32_t scenario_classification_state_size;
extern const uint32_t scenario_classification_default_mxcsr;
extern volatile LONG scenario_classification_state_loss;

void __cdecl scenario_classification_bridge(void);
void __cdecl scenario_classification_before(ScenarioClassificationFrame* frame,
    const void* entry);
void __cdecl scenario_classification_after(ScenarioClassificationFrame* frame);
void trace_scenario_marker_mask(const ScenarioMarkerMaskObservation* req);
int install_scenario_classification_observation(void* executable);
#endif
#endif

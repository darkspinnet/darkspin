#ifndef DARKSPIN_SCENARIO_ALTERNATE_H
#define DARKSPIN_SCENARIO_ALTERNATE_H
#ifndef FANG_SCENARIO
#define FANG_SCENARIO 0
#endif
#if FANG_SCENARIO
#include "scenario_input.h"
#include <stddef.h>
#include <windows.h>

enum ScenarioAlternatePhase {
    SCENARIO_ALTERNATE_ENTRY,
    SCENARIO_ALTERNATE_NORMAL_RETURN,
    SCENARIO_ALTERNATE_LIMIT
};

// Pre-call owned inputs only; no native owner, entry or result pointer.
typedef struct ScenarioAlternateSelectionEvent {
    enum ScenarioAlternatePhase Phase;
    uint32_t CallOrdinal;
    uint32_t RawMarkerSetReference;
    int32_t EntryOrdinal;
    ScenarioAuthoredInputCopy Input;
    uint32_t LossFlags;
} ScenarioAlternateSelectionEvent;

typedef struct ScenarioAlternateFrame {
    unsigned char StateArea[8256];
    unsigned char Reserved0[64];
    union {
        ScenarioAlternateSelectionEvent Event;
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
} ScenarioAlternateFrame;

// The naked bridge uses these fixed offsets, not C compiler frame offsets.
_Static_assert(sizeof(void*) == 4, "alternate bridge requires x86");
_Static_assert(sizeof(ScenarioAlternateSelectionEvent) <= 384, "event bound");
_Static_assert(sizeof(ScenarioAlternateFrame) == 9216, "frame bound");
_Static_assert(offsetof(ScenarioAlternateFrame, EventArea) == 8320, "event offset");
_Static_assert(offsetof(ScenarioAlternateSelectionEvent, CallOrdinal) == 4, "ordinal offset");
_Static_assert(offsetof(ScenarioAlternateFrame, IncomingLastError) == 8704, "incoming error");
_Static_assert(offsetof(ScenarioAlternateFrame, ReturnedLastError) == 8708, "returned error");
_Static_assert(offsetof(ScenarioAlternateFrame, MaskLow) == 8712, "mask low");
_Static_assert(offsetof(ScenarioAlternateFrame, MaskHigh) == 8716, "mask high");
_Static_assert(offsetof(ScenarioAlternateFrame, StateSize) == 8720, "state size");
_Static_assert(offsetof(ScenarioAlternateFrame, Mode) == 8724, "mode");
_Static_assert(offsetof(ScenarioAlternateFrame, OriginalStack) == 8728, "parent stack");
_Static_assert(offsetof(ScenarioAlternateFrame, ReturnRegisters) == 8768, "returned registers");
_Static_assert(offsetof(ScenarioAlternateFrame, InputRegisters) == 9180, "input registers");

// Initialized once before patch publication; assembly never invokes native accessors.
extern void* scenario_alternate_original;
extern uint32_t scenario_alternate_mode;
extern uint32_t scenario_alternate_mask_low;
extern uint32_t scenario_alternate_mask_high;
extern uint32_t scenario_alternate_state_size;
extern const uint32_t scenario_alternate_default_mxcsr;
extern volatile LONG scenario_alternate_state_loss;

void __cdecl scenario_alternate_bridge(void);
void __cdecl scenario_alternate_before(ScenarioAlternateFrame* frame, const void* entry,
    uint32_t raw_marker_set_reference, int32_t entry_ordinal);
void __cdecl scenario_alternate_after(ScenarioAlternateFrame* frame);
void trace_scenario_alternate_selection(const ScenarioAlternateSelectionEvent* req);
int install_scenario_alternate_observation(void* executable);
#endif
#endif

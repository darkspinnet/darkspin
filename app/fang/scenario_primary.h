#ifndef DARKSPIN_SCENARIO_PRIMARY_H
#define DARKSPIN_SCENARIO_PRIMARY_H

#ifndef FANG_SCENARIO
#define FANG_SCENARIO 0
#endif

#if FANG_SCENARIO
#include <stdint.h>

typedef enum ScenarioPrimaryBaseCallPhase {
    SCENARIO_PRIMARY_BASE_ENTRY,
    SCENARIO_PRIMARY_BASE_NORMAL_RETURN,
    SCENARIO_PRIMARY_BASE_LIMIT
} ScenarioPrimaryBaseCallPhase;

#include "scenario_input.h"

typedef ScenarioResourceNameFailure ScenarioPrimaryNameFailure;
typedef ScenarioResourceNameCopy ScenarioPrimaryResourceNameCopy;
#define SCENARIO_PRIMARY_NAME_NONE SCENARIO_INPUT_NAME_NONE
#define SCENARIO_PRIMARY_NAME_HOLDER_UNREADABLE SCENARIO_INPUT_NAME_HOLDER_UNREADABLE
#define SCENARIO_PRIMARY_NAME_ADDRESS_ZERO SCENARIO_INPUT_NAME_ADDRESS_ZERO
#define SCENARIO_PRIMARY_NAME_BYTES_UNREADABLE SCENARIO_INPUT_NAME_BYTES_UNREADABLE
#define SCENARIO_PRIMARY_NAME_RECHECK_UNREADABLE SCENARIO_INPUT_NAME_RECHECK_UNREADABLE
#define SCENARIO_PRIMARY_NAME_HOLDER_CHANGED SCENARIO_INPUT_NAME_HOLDER_CHANGED
#define SCENARIO_PRIMARY_NAME_UNTERMINATED SCENARIO_INPUT_NAME_UNTERMINATED
#define SCENARIO_PRIMARY_NAME_EMPTY SCENARIO_INPUT_NAME_EMPTY
#define SCENARIO_PRIMARY_NAME_NON_ASCII SCENARIO_INPUT_NAME_NON_ASCII
#define SCENARIO_PRIMARY_NAME_UNEXPECTED SCENARIO_INPUT_NAME_UNEXPECTED
#define SCENARIO_PRIMARY_NAME_ENTRY_UNAVAILABLE SCENARIO_INPUT_NAME_ENTRY_UNAVAILABLE
#define SCENARIO_PRIMARY_NAME_ENTRY_RECHECK_UNREADABLE SCENARIO_INPUT_NAME_ENTRY_RECHECK_UNREADABLE
#define SCENARIO_PRIMARY_NAME_ENTRY_NOUN_CHANGED SCENARIO_INPUT_NAME_ENTRY_NOUN_CHANGED

// Owned pre-call scalars only. Raw references are opaque holder addresses,
// never semantic IDs. No registry, entry or returned-record pointer is retained.
typedef struct ScenarioPrimaryBaseCallEvent {
    ScenarioPrimaryBaseCallPhase Phase;
    uint32_t CallOrdinal;
    uint32_t RawMarkerSetReference;
    int32_t EntryOrdinal;
    uint32_t MarkerID;
    uint32_t RawNounReference;
    uint32_t PositionBits[3];
    uint32_t RotationBits[3];
    uint32_t ScaleBits;
    uint32_t CopyByteCount;
    uint32_t CopyError;
    int IsCopyAttempted;
    int IsInputCopyAvailable;
    int IsResultNonzero;
    ScenarioPrimaryResourceNameCopy MarkerSetName;
    ScenarioPrimaryResourceNameCopy NounName;
    int IsNounAssociationRecheckAttempted;
    int IsNounAssociationRecheckAvailable;
    int IsNounAssociationUnchanged;
} ScenarioPrimaryBaseCallEvent;

void trace_scenario_primary_base(const ScenarioPrimaryBaseCallEvent* req);
int install_scenario_primary_observation(void* executable);
#endif

#endif

#ifndef DARKSPIN_SCENARIO_INPUT_H
#define DARKSPIN_SCENARIO_INPUT_H
#ifndef FANG_SCENARIO
#define FANG_SCENARIO 0
#endif
#if FANG_SCENARIO
#include <stdint.h>
typedef enum ScenarioResourceNameFailure {
    SCENARIO_INPUT_NAME_NONE,
    SCENARIO_INPUT_NAME_HOLDER_UNREADABLE,
    SCENARIO_INPUT_NAME_ADDRESS_ZERO,
    SCENARIO_INPUT_NAME_BYTES_UNREADABLE,
    SCENARIO_INPUT_NAME_RECHECK_UNREADABLE,
    SCENARIO_INPUT_NAME_HOLDER_CHANGED,
    SCENARIO_INPUT_NAME_UNTERMINATED,
    SCENARIO_INPUT_NAME_EMPTY,
    SCENARIO_INPUT_NAME_NON_ASCII,
    SCENARIO_INPUT_NAME_UNEXPECTED,
    SCENARIO_INPUT_NAME_ENTRY_UNAVAILABLE,
    SCENARIO_INPUT_NAME_ENTRY_RECHECK_UNREADABLE,
    SCENARIO_INPUT_NAME_ENTRY_NOUN_CHANGED
} ScenarioResourceNameFailure;

typedef struct ScenarioResourceNameCopy {
    char Name[64];
    ScenarioResourceNameFailure Failure;
    int IsCopyAttempted;
    int IsHolderWordCopied;
    int IsNameBytesCopied;
    int IsHolderRecheckAvailable;
    int IsHolderWordUnchanged;
    int IsNameAvailable;
} ScenarioResourceNameCopy;

// Owned authored scalars/names only; no native owner/entry/result pointer.
typedef struct ScenarioAuthoredInputCopy {
    uint32_t MarkerID;
    uint32_t RawNounReference;
    uint32_t PositionBits[3];
    uint32_t RotationBits[3];
    uint32_t ScaleBits;
    uint32_t CopyByteCount;
    uint32_t CopyError;
    int IsCopyAttempted;
    int IsInputCopyAvailable;
    ScenarioResourceNameCopy MarkerSetName;
    ScenarioResourceNameCopy NounName;
    int IsNounAssociationRecheckAttempted;
    int IsNounAssociationRecheckAvailable;
    int IsNounAssociationUnchanged;
} ScenarioAuthoredInputCopy;
void copy_scenario_authored_input(const void* entry,
    uint32_t raw_marker_set_reference, ScenarioAuthoredInputCopy* copy);
#endif
#endif
#include "scenario_input.h"
#if FANG_SCENARIO
#include "hook.h"
#include <string.h>

static int is_scenario_name_equal(const char* actual, const char* expected) {
    unsigned int index;
    for (index = 0; index < 64; index++) {
        unsigned char actual_byte = (unsigned char)actual[index];
        unsigned char expected_byte = (unsigned char)expected[index];
        if (actual_byte >= 'A' && actual_byte <= 'Z') {
            actual_byte += 'a' - 'A';
        }
        if (expected_byte >= 'A' && expected_byte <= 'Z') {
            expected_byte += 'a' - 'A';
        }
        if (actual_byte != expected_byte) {
            return 0;
        }
        if (actual_byte == 0) {
            return 1;
        }
    }
    return 0;
}

static int is_supported_scenario_name(const char* name, int is_marker_set) {
    if (is_marker_set) {
        return is_scenario_name_equal(name, "zelems_3_design.Markerset") ||
            is_scenario_name_equal(name, "zelems_3_Smart_Objects_3.Markerset");
    }
    return is_scenario_name_equal(name,
        "DEST_prefab_islands_instrument_scitech_11.Noun") ||
        is_scenario_name_equal(name,
            "DEST_prefab_islands_instrument_scitech_3.Noun") ||
        is_scenario_name_equal(name,
            "DEST_prefab_islands_instrument_scitech_7.Noun");
}

static void copy_scenario_resource_name(uint32_t holder, int is_marker_set,
    ScenarioResourceNameCopy* copy) {
    uint32_t name_address = 0;
    uint32_t rechecked_address = 0;
    char copied_name[64] = {0};
    SIZE_T copied_count = 0;
    BOOL is_copied;
    unsigned int name_length;
    copy->IsCopyAttempted = 1;
    is_copied = ReadProcessMemory(GetCurrentProcess(),
        (const void*)(uintptr_t)holder, &name_address,
        sizeof(name_address), &copied_count);
    if (!is_copied || copied_count != sizeof(name_address)) {
        copy->Failure = SCENARIO_INPUT_NAME_HOLDER_UNREADABLE;
        return;
    }
    copy->IsHolderWordCopied = 1;
    if (name_address == 0) {
        copy->Failure = SCENARIO_INPUT_NAME_ADDRESS_ZERO;
    } else {
        copied_count = 0;
        is_copied = ReadProcessMemory(GetCurrentProcess(),
            (const void*)(uintptr_t)name_address, copied_name,
            sizeof(copied_name), &copied_count);
        copy->IsNameBytesCopied =
            is_copied && copied_count == sizeof(copied_name);
        if (!copy->IsNameBytesCopied) {
            copy->Failure = SCENARIO_INPUT_NAME_BYTES_UNREADABLE;
        }
    }
    copied_count = 0;
    is_copied = ReadProcessMemory(GetCurrentProcess(),
        (const void*)(uintptr_t)holder, &rechecked_address,
        sizeof(rechecked_address), &copied_count);
    if (!is_copied || copied_count != sizeof(rechecked_address)) {
        copy->Failure = SCENARIO_INPUT_NAME_RECHECK_UNREADABLE;
        return;
    }
    copy->IsHolderRecheckAvailable = 1;
    copy->IsHolderWordUnchanged = rechecked_address == name_address;
    if (!copy->IsHolderWordUnchanged) {
        copy->Failure = SCENARIO_INPUT_NAME_HOLDER_CHANGED;
        return;
    }
    if (!copy->IsNameBytesCopied) {
        return;
    }
    for (name_length = 0; name_length < sizeof(copied_name); name_length++) {
        unsigned char name_byte = (unsigned char)copied_name[name_length];
        if (name_byte == 0) {
            break;
        }
        if (name_byte > 0x7f) {
            copy->Failure = SCENARIO_INPUT_NAME_NON_ASCII;
            return;
        }
    }
    if (name_length == sizeof(copied_name)) {
        copy->Failure = SCENARIO_INPUT_NAME_UNTERMINATED;
        return;
    }
    if (name_length == 0) {
        copy->Failure = SCENARIO_INPUT_NAME_EMPTY;
        return;
    }
    if (!is_supported_scenario_name(copied_name, is_marker_set)) {
        copy->Failure = SCENARIO_INPUT_NAME_UNEXPECTED;
        return;
    }
    // Retain the actual copied spelling, not the expected allowlist constant.
    // Equal pointer rechecks are not atomicity, ABA exclusion or a lease.
    memcpy(copy->Name, copied_name, name_length + 1);
    copy->IsNameAvailable = 1;
}

void copy_scenario_authored_input(const void* entry,
    uint32_t raw_marker_set_reference, ScenarioAuthoredInputCopy* copy) {
    uint32_t words[16] = {0};
    uint32_t rechecked_noun_reference = 0;
    SIZE_T copied_count = 0;
    BOOL is_copied;
    memset(copy, 0, sizeof(*copy));
    copy->IsCopyAttempted = 1;
    // Exactly one initial 64-byte entry copy. Separately authorized names and
    // entry +8 recheck below never invoke a native accessor or loader.
    is_copied = ReadProcessMemory(GetCurrentProcess(), entry,
        words, sizeof(words), &copied_count);
    copy->CopyError = GetLastError();
    copy->CopyByteCount = (uint32_t)copied_count;
    if (is_copied && copied_count == sizeof(words)) {
        copy->IsInputCopyAvailable = 1;
        copy->CopyError = 0;
        copy->MarkerID = words[1];
        copy->RawNounReference = words[2];
        copy->PositionBits[0] = words[7];
        copy->PositionBits[1] = words[8];
        copy->PositionBits[2] = words[9];
        copy->RotationBits[0] = words[10];
        copy->RotationBits[1] = words[11];
        copy->RotationBits[2] = words[12];
        copy->ScaleBits = words[13];
    } else if (is_copied) {
        copy->CopyError = ERROR_PARTIAL_COPY;
    }
    copy_scenario_resource_name(raw_marker_set_reference, 1, &copy->MarkerSetName);
    if (copy->IsInputCopyAvailable) {
        copy_scenario_resource_name(copy->RawNounReference, 0, &copy->NounName);
        copy->IsNounAssociationRecheckAttempted = 1;
        copied_count = 0;
        is_copied = ReadProcessMemory(GetCurrentProcess(),
            (const void*)((uintptr_t)entry + 8), &rechecked_noun_reference,
            sizeof(rechecked_noun_reference), &copied_count);
        copy->IsNounAssociationRecheckAvailable =
            is_copied && copied_count == sizeof(rechecked_noun_reference);
        copy->IsNounAssociationUnchanged = copy->IsNounAssociationRecheckAvailable &&
            rechecked_noun_reference == copy->RawNounReference;
        if (!copy->IsNounAssociationUnchanged) {
            copy->NounName.IsNameAvailable = 0;
            copy->NounName.Name[0] = '\0';
            copy->NounName.Failure = copy->IsNounAssociationRecheckAvailable ?
                SCENARIO_INPUT_NAME_ENTRY_NOUN_CHANGED :
                SCENARIO_INPUT_NAME_ENTRY_RECHECK_UNREADABLE;
        }
    } else {
        copy->NounName.Failure = SCENARIO_INPUT_NAME_ENTRY_UNAVAILABLE;
    }
}
#endif

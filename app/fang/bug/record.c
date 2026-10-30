#include "bug/record.h"

#if FANG_SCENARIO
#include "scenario/primary.h"
#include "scenario/alternate.h"
#include "scenario/classification.h"
#endif

#include <ctype.h>
#include <limits.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

typedef struct snapshot_line {
    struct snapshot_line* next;
    ULONGLONG time_ms;
    unsigned int size;
    char contents[1];
} snapshot_line;

static SRWLOCK snapshot_ring_lock = SRWLOCK_INIT;
static snapshot_line* snapshot_ring_head;
static snapshot_line* snapshot_ring_tail;
static size_t snapshot_ring_byte_count;
static ULONGLONG snapshot_ring_capacity_dropped_line_count;
static ULONGLONG snapshot_ring_capacity_dropped_byte_count;
static ULONGLONG snapshot_ring_capacity_last_dropped_time_ms;

#if FANG_SCENARIO
void trace_scenario_primary_base(const ScenarioPrimaryBaseCallEvent* req) {
    const size_t maximum_bytes = 64u * 1024u * 1024u;
    const ULONGLONG maximum_age_ms = 300000;
    // Sticky loss bits: unavailable trace, failed/short write, ring allocation,
    // formatting, failed ring-node free. They never wrap or affect the callee.
    static volatile LONG recorder_loss_flags;
    DWORD last_error = GetLastError();
    char line[3072];
    char input[384];
    char name_jsons[2][512];
    char quoted_name[66];
    char call_ordinal[16];
    char loss_digits[9];
    const char* phase;
    const char* input_json = "null";
    const char* normal_return = "null";
    const char* result_nonzero = "null";
    const char* ordinal_json = "null";
    char* status_field;
    char* loss_field;
    snapshot_line* recorded;
    ULONGLONG now;
    DWORD written = 0;
    BOOL is_written = FALSE;
    LONG loss_flags;
    int length;
    int is_limit;
    size_t loss_offset;
    unsigned int name_index;
    const ScenarioPrimaryResourceNameCopy* name_copies[2];
    static const char* name_failures[] = {
        "none", "holder_word_unreadable", "name_address_zero",
        "name_bytes_unreadable", "holder_recheck_unreadable",
        "holder_word_changed", "name_unterminated", "name_empty",
        "name_non_ascii", "name_unexpected", "entry_copy_unavailable",
        "entry_noun_recheck_unreadable", "entry_noun_changed"
    };
    if (req == NULL) {
        return;
    }
    is_limit = req->Phase == SCENARIO_PRIMARY_BASE_LIMIT;
    if (req->Phase == SCENARIO_PRIMARY_BASE_ENTRY) {
        phase = "entry";
    } else if (req->Phase == SCENARIO_PRIMARY_BASE_NORMAL_RETURN) {
        phase = "normal_return";
        normal_return = "true";
        result_nonzero = req->IsResultNonzero ? "true" : "false";
    } else if (is_limit) {
        phase = "limit";
    } else {
        InterlockedOr(&recorder_loss_flags, 8);
        SetLastError(last_error);
        return;
    }
    if (!is_limit) {
        snprintf(call_ordinal, sizeof(call_ordinal), "%u", req->CallOrdinal);
        ordinal_json = call_ordinal;
    }
    if (req->IsInputCopyAvailable) {
        length = snprintf(input, sizeof(input),
            "{\"marker_id\":%u,\"raw_noun_reference\":%u,"
            "\"position_bits\":[%u,%u,%u],\"rotation_bits\":[%u,%u,%u],"
            "\"scale_bits\":%u}",
            req->MarkerID, req->RawNounReference,
            req->PositionBits[0], req->PositionBits[1], req->PositionBits[2],
            req->RotationBits[0], req->RotationBits[1], req->RotationBits[2],
            req->ScaleBits);
        if (length <= 0 || (size_t)length >= sizeof(input)) {
            InterlockedOr(&recorder_loss_flags, 8);
            SetLastError(last_error);
            return;
        }
        input_json = input;
    }
    name_copies[0] = &req->MarkerSetName;
    name_copies[1] = &req->NounName;
    for (name_index = 0; name_index < 2; name_index++) {
        const ScenarioPrimaryResourceNameCopy* copy = name_copies[name_index];
        const char* name_json = "null";
        const char* failure = "invalid_failure";
        if (copy->Failure >= SCENARIO_PRIMARY_NAME_NONE &&
            copy->Failure <= SCENARIO_PRIMARY_NAME_ENTRY_NOUN_CHANGED) {
            failure = name_failures[copy->Failure];
        }
        if (!copy->IsCopyAttempted && copy->Failure == SCENARIO_PRIMARY_NAME_NONE) {
            failure = "not_attempted";
        }
        if (copy->IsNameAvailable) {
            // The producer accepts only the five exact ASCII asset names;
            // their actual copied spelling contains no JSON escape characters.
            length = snprintf(quoted_name, sizeof(quoted_name), "\"%s\"", copy->Name);
            if (length <= 0 || (size_t)length >= sizeof(quoted_name)) {
                InterlockedOr(&recorder_loss_flags, 8);
                SetLastError(last_error);
                return;
            }
            name_json = quoted_name;
        }
        length = snprintf(name_jsons[name_index], sizeof(name_jsons[name_index]),
            "{\"name\":%s,\"is_name_available\":%s,"
            "\"is_copy_attempted\":%s,\"is_holder_word_copied\":%s,"
            "\"is_name_bytes_copied\":%s,\"is_holder_recheck_available\":%s,"
            "\"is_holder_word_unchanged\":%s,\"failure_reason\":\"%s\"}",
            name_json, copy->IsNameAvailable ? "true" : "false",
            copy->IsCopyAttempted ? "true" : "false",
            copy->IsHolderWordCopied ? "true" : "false",
            copy->IsNameBytesCopied ? "true" : "false",
            copy->IsHolderRecheckAvailable ? "true" : "false",
            copy->IsHolderRecheckAvailable ?
                (copy->IsHolderWordUnchanged ? "true" : "false") : "null", failure);
        if (length <= 0 || (size_t)length >= sizeof(name_jsons[name_index])) {
            InterlockedOr(&recorder_loss_flags, 8);
            SetLastError(last_error);
            return;
        }
    }
    recorded = (snapshot_line*)HeapAlloc(
        GetProcessHeap(), 0, sizeof(*recorded) - 1 + sizeof(line));
    if (recorded == NULL) {
        InterlockedOr(&recorder_loss_flags, 4);
    }
    if (!fang_record_trace_available()) {
        InterlockedOr(&recorder_loss_flags, 1);
    }
    loss_flags = InterlockedCompareExchange(&recorder_loss_flags, 0, 0);
    now = GetTickCount64();
    // Status 0 = not yet known, 1 = completed, 2 = failed/unavailable.
    // A raw trace row cannot attest to its own completed write. The ring copy
    // records the actual write outcome; neither row promises later retention.
    length = snprintf(line, sizeof(line),
        "{\"time_ms\":%llu,\"protocol\":\"client_state\","
        "\"kind\":\"scenario_primary_base_call\",\"phase\":\"%s\","
        "\"thread\":%lu,\"frame_sequence\":%lu,\"call_ordinal\":%s,"
        "\"raw_marker_set_reference\":%u,\"entry_ordinal\":%d,"
        "\"input_provenance\":\"pre_call\",\"input\":%s,"
        "\"is_copy_attempted\":%s,\"is_input_copy_available\":%s,"
        "\"copy_byte_count\":%u,\"copy_error\":%u,"
        "\"marker_set_name\":%s,\"noun_name\":%s,"
        "\"is_noun_association_recheck_attempted\":%s,"
        "\"is_noun_association_recheck_available\":%s,"
        "\"is_noun_association_unchanged\":%s,"
        "\"is_resource_name_copy_atomic\":null,\"is_holder_aba_excluded\":null,"
        "\"is_normal_return_observed\":%s,\"is_result_nonzero\":%s,"
        "\"is_call_limit_reached\":%s,\"call_limit\":4096,"
        "\"is_input_copy_atomic\":null,\"is_reentrancy_excluded\":null,"
        "\"trace_write_status\":0,\"ring_append_status\":%d,"
        "\"is_ring_retention_complete\":null,"
        "\"recorder_loss_flags\":\"0x%08lx\"}\r\n",
        (unsigned long long)now, phase, (unsigned long)GetCurrentThreadId(),
        (unsigned long)fang_record_frame_sequence(),
        ordinal_json, req->RawMarkerSetReference, req->EntryOrdinal, input_json,
        req->IsCopyAttempted ? "true" : "false",
        req->IsInputCopyAvailable ? "true" : "false",
        req->CopyByteCount, req->CopyError, name_jsons[0], name_jsons[1],
        req->IsNounAssociationRecheckAttempted ? "true" : "false",
        req->IsNounAssociationRecheckAvailable ? "true" : "false",
        req->IsNounAssociationRecheckAvailable ?
            (req->IsNounAssociationUnchanged ? "true" : "false") : "null",
        normal_return, result_nonzero,
        is_limit ? "true" : "false", recorded == NULL ? 2 : 0,
        (unsigned long)loss_flags);
    if (length <= 0 || (size_t)length >= sizeof(line)) {
        InterlockedOr(&recorder_loss_flags, 8);
        if (recorded != NULL && !HeapFree(GetProcessHeap(), 0, recorded)) {
            InterlockedOr(&recorder_loss_flags, 16);
        }
        SetLastError(last_error);
        return;
    }
    if (fang_record_trace_available()) {
        is_written = fang_record_trace_write(line, (DWORD)length, &written);
        if (!is_written || written != (DWORD)length) {
            InterlockedOr(&recorder_loss_flags, 2);
        }
    }
    if (recorded == NULL) {
        SetLastError(last_error);
        return;
    }
    status_field = strstr(line, "\"trace_write_status\":0");
    status_field[strlen("\"trace_write_status\":")] =
        is_written && written == (DWORD)length ? '1' : '2';
    status_field = strstr(line, "\"ring_append_status\":0");
    status_field[strlen("\"ring_append_status\":")] = '1';
    loss_field = strstr(line, "\"recorder_loss_flags\":\"0x");
    loss_offset = (size_t)(loss_field - line) +
        strlen("\"recorder_loss_flags\":\"0x");
    recorded->next = NULL;
    recorded->time_ms = now;
    recorded->size = (unsigned int)length;
    memcpy(recorded->contents, line, (size_t)length);
    AcquireSRWLockExclusive(&snapshot_ring_lock);
    if (snapshot_ring_tail == NULL) {
        snapshot_ring_head = recorded;
    } else {
        snapshot_ring_tail->next = recorded;
    }
    snapshot_ring_tail = recorded;
    snapshot_ring_byte_count += (size_t)length;
    while (snapshot_ring_head != NULL &&
        (snapshot_ring_byte_count > maximum_bytes ||
         now - snapshot_ring_head->time_ms > maximum_age_ms)) {
        snapshot_line* removed = snapshot_ring_head;
        int is_capacity_drop = snapshot_ring_byte_count > maximum_bytes;
        snapshot_ring_head = removed->next;
        snapshot_ring_byte_count -= removed->size;
        if (is_capacity_drop) {
            snapshot_ring_capacity_dropped_line_count++;
            snapshot_ring_capacity_dropped_byte_count += removed->size;
            snapshot_ring_capacity_last_dropped_time_ms = removed->time_ms;
        }
        if (!HeapFree(GetProcessHeap(), 0, removed)) {
            InterlockedOr(&recorder_loss_flags, 16);
        }
    }
    loss_flags = InterlockedCompareExchange(&recorder_loss_flags, 0, 0);
    snprintf(loss_digits, sizeof(loss_digits), "%08lx", (unsigned long)loss_flags);
    memcpy(recorded->contents + loss_offset, loss_digits, 8);
    ReleaseSRWLockExclusive(&snapshot_ring_lock);
    SetLastError(last_error);
}

void trace_scenario_alternate_selection(const ScenarioAlternateSelectionEvent* req) {
    const size_t maximum_bytes = 64u * 1024u * 1024u;
    const ULONGLONG maximum_age_ms = 300000;
    // Sticky loss bits: unavailable trace, failed/short write, ring allocation,
    // formatting, failed ring-node free; bit32 adds a saved-state-only bypass.
    // They never wrap or affect the callee.
    static volatile LONG recorder_loss_flags;
    DWORD last_error = GetLastError();
    char line[3072];
    char input[384];
    char name_jsons[2][512];
    char quoted_name[66];
    char call_ordinal[16];
    char loss_digits[9];
    const char* phase;
    const char* input_json = "null";
    const char* normal_return = "null";
    const char* ordinal_json = "null";
    char* status_field;
    char* loss_field;
    snapshot_line* recorded;
    ULONGLONG now;
    DWORD written = 0;
    BOOL is_written = FALSE;
    LONG loss_flags;
    int length;
    int is_limit;
    size_t loss_offset;
    unsigned int name_index;
    const ScenarioPrimaryResourceNameCopy* name_copies[2];
    static const char* name_failures[] = {
        "none", "holder_word_unreadable", "name_address_zero",
        "name_bytes_unreadable", "holder_recheck_unreadable",
        "holder_word_changed", "name_unterminated", "name_empty",
        "name_non_ascii", "name_unexpected", "entry_copy_unavailable",
        "entry_noun_recheck_unreadable", "entry_noun_changed"
    };
    if (req == NULL) {
        return;
    }
    InterlockedOr(&recorder_loss_flags, (LONG)req->LossFlags);
    is_limit = req->Phase == SCENARIO_ALTERNATE_LIMIT;
    if (req->Phase == SCENARIO_ALTERNATE_ENTRY) {
        phase = "entry";
    } else if (req->Phase == SCENARIO_ALTERNATE_NORMAL_RETURN) {
        phase = "normal_return";
        normal_return = "true";
    } else if (is_limit) {
        phase = "limit";
    } else {
        InterlockedOr(&recorder_loss_flags, 8);
        SetLastError(last_error);
        return;
    }
    if (!is_limit) {
        snprintf(call_ordinal, sizeof(call_ordinal), "%u", req->CallOrdinal);
        ordinal_json = call_ordinal;
    }
    if (req->Input.IsInputCopyAvailable) {
        length = snprintf(input, sizeof(input),
            "{\"marker_id\":%u,\"raw_noun_reference\":%u,"
            "\"position_bits\":[%u,%u,%u],\"rotation_bits\":[%u,%u,%u],"
            "\"scale_bits\":%u}",
            req->Input.MarkerID, req->Input.RawNounReference,
            req->Input.PositionBits[0], req->Input.PositionBits[1], req->Input.PositionBits[2],
            req->Input.RotationBits[0], req->Input.RotationBits[1], req->Input.RotationBits[2],
            req->Input.ScaleBits);
        if (length <= 0 || (size_t)length >= sizeof(input)) {
            InterlockedOr(&recorder_loss_flags, 8);
            SetLastError(last_error);
            return;
        }
        input_json = input;
    }
    name_copies[0] = &req->Input.MarkerSetName;
    name_copies[1] = &req->Input.NounName;
    for (name_index = 0; name_index < 2; name_index++) {
        const ScenarioPrimaryResourceNameCopy* copy = name_copies[name_index];
        const char* name_json = "null";
        const char* failure = "invalid_failure";
        if (copy->Failure >= SCENARIO_PRIMARY_NAME_NONE &&
            copy->Failure <= SCENARIO_PRIMARY_NAME_ENTRY_NOUN_CHANGED) {
            failure = name_failures[copy->Failure];
        }
        if (!copy->IsCopyAttempted && copy->Failure == SCENARIO_PRIMARY_NAME_NONE) {
            failure = "not_attempted";
        }
        if (copy->IsNameAvailable) {
            // The producer accepts only the five exact ASCII asset names;
            // their actual copied spelling contains no JSON escape characters.
            length = snprintf(quoted_name, sizeof(quoted_name), "\"%s\"", copy->Name);
            if (length <= 0 || (size_t)length >= sizeof(quoted_name)) {
                InterlockedOr(&recorder_loss_flags, 8);
                SetLastError(last_error);
                return;
            }
            name_json = quoted_name;
        }
        length = snprintf(name_jsons[name_index], sizeof(name_jsons[name_index]),
            "{\"name\":%s,\"is_name_available\":%s,"
            "\"is_copy_attempted\":%s,\"is_holder_word_copied\":%s,"
            "\"is_name_bytes_copied\":%s,\"is_holder_recheck_available\":%s,"
            "\"is_holder_word_unchanged\":%s,\"failure_reason\":\"%s\"}",
            name_json, copy->IsNameAvailable ? "true" : "false",
            copy->IsCopyAttempted ? "true" : "false",
            copy->IsHolderWordCopied ? "true" : "false",
            copy->IsNameBytesCopied ? "true" : "false",
            copy->IsHolderRecheckAvailable ? "true" : "false",
            copy->IsHolderRecheckAvailable ?
                (copy->IsHolderWordUnchanged ? "true" : "false") : "null", failure);
        if (length <= 0 || (size_t)length >= sizeof(name_jsons[name_index])) {
            InterlockedOr(&recorder_loss_flags, 8);
            SetLastError(last_error);
            return;
        }
    }
    recorded = (snapshot_line*)HeapAlloc(
        GetProcessHeap(), 0, sizeof(*recorded) - 1 + sizeof(line));
    if (recorded == NULL) {
        InterlockedOr(&recorder_loss_flags, 4);
    }
    if (!fang_record_trace_available()) {
        InterlockedOr(&recorder_loss_flags, 1);
    }
    loss_flags = InterlockedCompareExchange(&recorder_loss_flags, 0, 0);
    now = GetTickCount64();
    // Status 0 = not yet known, 1 = completed, 2 = failed/unavailable.
    // A raw trace row cannot attest to its own completed write. The ring copy
    // records the actual write outcome; neither row promises later retention.
    length = snprintf(line, sizeof(line),
        "{\"time_ms\":%llu,\"protocol\":\"client_state\","
        "\"kind\":\"scenario_alternate_selection_call\",\"phase\":\"%s\","
        "\"thread\":%lu,\"frame_sequence\":%lu,\"call_ordinal\":%s,"
        "\"raw_marker_set_reference\":%u,\"entry_ordinal\":%d,"
        "\"input_provenance\":\"pre_call\",\"input\":%s,"
        "\"is_copy_attempted\":%s,\"is_input_copy_available\":%s,"
        "\"copy_byte_count\":%u,\"copy_error\":%u,"
        "\"marker_set_name\":%s,\"noun_name\":%s,"
        "\"is_noun_association_recheck_attempted\":%s,"
        "\"is_noun_association_recheck_available\":%s,"
        "\"is_noun_association_unchanged\":%s,"
        "\"is_resource_name_copy_atomic\":null,\"is_holder_aba_excluded\":null,"
        "\"is_normal_return_observed\":%s,"
        "\"is_call_limit_reached\":%s,\"call_limit\":4096,"
        "\"is_input_copy_atomic\":null,\"is_reentrancy_excluded\":null,"
        "\"trace_write_status\":0,\"ring_append_status\":%d,"
        "\"is_ring_retention_complete\":null,"
        "\"recorder_loss_flags\":\"0x%08lx\"}\r\n",
        (unsigned long long)now, phase, (unsigned long)GetCurrentThreadId(),
        (unsigned long)fang_record_frame_sequence(),
        ordinal_json, req->RawMarkerSetReference, req->EntryOrdinal, input_json,
        req->Input.IsCopyAttempted ? "true" : "false",
        req->Input.IsInputCopyAvailable ? "true" : "false",
        req->Input.CopyByteCount, req->Input.CopyError, name_jsons[0], name_jsons[1],
        req->Input.IsNounAssociationRecheckAttempted ? "true" : "false",
        req->Input.IsNounAssociationRecheckAvailable ? "true" : "false",
        req->Input.IsNounAssociationRecheckAvailable ?
            (req->Input.IsNounAssociationUnchanged ? "true" : "false") : "null",
        normal_return,
        is_limit ? "true" : "false", recorded == NULL ? 2 : 0,
        (unsigned long)loss_flags);
    if (length <= 0 || (size_t)length >= sizeof(line)) {
        InterlockedOr(&recorder_loss_flags, 8);
        if (recorded != NULL && !HeapFree(GetProcessHeap(), 0, recorded)) {
            InterlockedOr(&recorder_loss_flags, 16);
        }
        SetLastError(last_error);
        return;
    }
    if (fang_record_trace_available()) {
        is_written = fang_record_trace_write(line, (DWORD)length, &written);
        if (!is_written || written != (DWORD)length) {
            InterlockedOr(&recorder_loss_flags, 2);
        }
    }
    if (recorded == NULL) {
        SetLastError(last_error);
        return;
    }
    status_field = strstr(line, "\"trace_write_status\":0");
    status_field[strlen("\"trace_write_status\":")] =
        is_written && written == (DWORD)length ? '1' : '2';
    status_field = strstr(line, "\"ring_append_status\":0");
    status_field[strlen("\"ring_append_status\":")] = '1';
    loss_field = strstr(line, "\"recorder_loss_flags\":\"0x");
    loss_offset = (size_t)(loss_field - line) +
        strlen("\"recorder_loss_flags\":\"0x");
    recorded->next = NULL;
    recorded->time_ms = now;
    recorded->size = (unsigned int)length;
    memcpy(recorded->contents, line, (size_t)length);
    AcquireSRWLockExclusive(&snapshot_ring_lock);
    if (snapshot_ring_tail == NULL) {
        snapshot_ring_head = recorded;
    } else {
        snapshot_ring_tail->next = recorded;
    }
    snapshot_ring_tail = recorded;
    snapshot_ring_byte_count += (size_t)length;
    while (snapshot_ring_head != NULL &&
        (snapshot_ring_byte_count > maximum_bytes ||
         now - snapshot_ring_head->time_ms > maximum_age_ms)) {
        snapshot_line* removed = snapshot_ring_head;
        int is_capacity_drop = snapshot_ring_byte_count > maximum_bytes;
        snapshot_ring_head = removed->next;
        snapshot_ring_byte_count -= removed->size;
        if (is_capacity_drop) {
            snapshot_ring_capacity_dropped_line_count++;
            snapshot_ring_capacity_dropped_byte_count += removed->size;
            snapshot_ring_capacity_last_dropped_time_ms = removed->time_ms;
        }
        if (!HeapFree(GetProcessHeap(), 0, removed)) {
            InterlockedOr(&recorder_loss_flags, 16);
        }
    }
    loss_flags = InterlockedCompareExchange(&recorder_loss_flags, 0, 0);
    snprintf(loss_digits, sizeof(loss_digits), "%08lx", (unsigned long)loss_flags);
    memcpy(recorded->contents + loss_offset, loss_digits, 8);
    ReleaseSRWLockExclusive(&snapshot_ring_lock);
    SetLastError(last_error);
}

void trace_scenario_marker_mask(const ScenarioMarkerMaskObservation* req) {
    const size_t maximum_bytes = 64u * 1024u * 1024u;
    const ULONGLONG maximum_age_ms = 300000;
    // Independent sticky loss: unavailable1/write2/allocation4/format8/free16,
    // plus saved-state bypass32. No record attests to exhaustive retention.
    static volatile LONG recorder_loss_flags;
    DWORD last_error = GetLastError();
    char line[768], digits[5][16], loss_digits[9], position_digits[35];
    const char* position_bits = "null";
    int digit_lengths[5];
    unsigned int digit_index;
    const char* phase;
    const char* normal_return = "null";
    const char* failure;
    static const char* failures[] = {
        "none", "entry_null", "address_range", "read_failed", "short_read"
    };
    snapshot_line* recorded;
    char* write_field;
    char* append_field;
    char* loss_field;
    size_t loss_offset;
    ULONGLONG now;
    DWORD written = 0;
    BOOL is_written = FALSE;
    LONG loss_flags;
    int length;
    int is_limit;
    if (req == NULL) {
        SetLastError(last_error);
        return;
    }
    InterlockedOr(&recorder_loss_flags, (LONG)req->LossFlags);
    is_limit = req->Phase == SCENARIO_MARKER_MASK_LIMIT;
    if (req->Phase == SCENARIO_MARKER_MASK_ENTRY) {
        phase = "entry";
    } else if (req->Phase == SCENARIO_MARKER_MASK_NORMAL_RETURN) {
        phase = "normal_return";
        normal_return = "true";
    } else if (is_limit) {
        phase = "limit";
    } else {
        InterlockedOr(&recorder_loss_flags, 8);
        SetLastError(last_error);
        return;
    }
    if (req->CopyFailure < SCENARIO_MARKER_COPY_NONE ||
        req->CopyFailure > SCENARIO_MARKER_COPY_SHORT_READ ||
        (is_limit && (req->CallOrdinal != 0 || req->IsCopyAttempted ||
            req->IsMarkerIDAvailable || req->IsPositionAvailable)) ||
        (!is_limit && (req->CallOrdinal == 0 || req->CallOrdinal > 4096)) ||
        req->IsPositionAvailable != req->IsMarkerIDAvailable ||
        (req->IsMarkerIDAvailable && (!req->IsCopyAttempted ||
            req->CopyFailure != SCENARIO_MARKER_COPY_NONE || req->CopyByteCount != 36))) {
        InterlockedOr(&recorder_loss_flags, 8);
        SetLastError(last_error);
        return;
    }
    failure = failures[req->CopyFailure];
    if (!req->IsCopyAttempted && req->CopyFailure == SCENARIO_MARKER_COPY_NONE) {
        failure = "not_attempted";
    }
    digit_lengths[0] = snprintf(digits[0], sizeof(digits[0]), "%u", (unsigned int)req->CallOrdinal);
    digit_lengths[1] = snprintf(digits[1], sizeof(digits[1]), "%u", (unsigned int)req->MarkerID);
    digit_lengths[2] = snprintf(digits[2], sizeof(digits[2]), "%u", (unsigned int)req->RawMask);
    digit_lengths[3] = snprintf(digits[3], sizeof(digits[3]), "%u", (unsigned int)req->CopyByteCount);
    digit_lengths[4] = snprintf(digits[4], sizeof(digits[4]), "%u", (unsigned int)req->CopyError);
    for (digit_index = 0; digit_index < 5; digit_index++) {
        if (digit_lengths[digit_index] <= 0 || (size_t)digit_lengths[digit_index] >= sizeof(digits[0])) {
            InterlockedOr(&recorder_loss_flags, 8);
            SetLastError(last_error);
            return;
        }
    }
    if (req->IsPositionAvailable) {
        length = snprintf(position_digits, sizeof(position_digits), "[%u,%u,%u]",
            (unsigned int)req->PositionBits[0], (unsigned int)req->PositionBits[1],
            (unsigned int)req->PositionBits[2]);
        if (length <= 0 || (size_t)length >= sizeof(position_digits)) {
            InterlockedOr(&recorder_loss_flags, 8);
            SetLastError(last_error);
            return;
        }
        position_bits = position_digits;
    }
    recorded = (snapshot_line*)HeapAlloc(
        GetProcessHeap(), 0, sizeof(*recorded) - 1 + sizeof(line));
    if (recorded == NULL) {
        InterlockedOr(&recorder_loss_flags, 4);
    }
    if (!fang_record_trace_available()) {
        InterlockedOr(&recorder_loss_flags, 1);
    }
    loss_flags = InterlockedCompareExchange(&recorder_loss_flags, 0, 0);
    now = GetTickCount64();
    // Raw own write/append starts unknown0; only the later ring copy can
    // describe actual completed write/append, without a retention promise.
    length = snprintf(line, sizeof(line),
        "{\"time_ms\":%llu,\"protocol\":\"client_state\","
        "\"kind\":\"scenario_marker_mask_call\",\"phase\":\"%s\","
        "\"thread\":%lu,\"frame_sequence\":%lu,\"call_ordinal\":%s,"
        "\"marker_id\":%s,\"raw_mask\":%s,\"input_provenance\":\"pre_call\","
        "\"is_copy_attempted\":%s,\"is_marker_id_available\":%s,"
        "\"is_position_available\":%s,\"authored_position_bits\":%s,"
        "\"copy_byte_count\":%s,\"copy_error\":%s,\"copy_failure\":\"%s\","
        "\"is_normal_return_observed\":%s,\"is_call_limit_reached\":%s,"
        "\"call_limit\":4096,\"is_input_copy_atomic\":null,"
        "\"is_reentrancy_excluded\":null,\"trace_write_status\":0,"
        "\"ring_append_status\":%d,\"is_ring_retention_complete\":null,"
        "\"recorder_loss_flags\":\"0x%08lx\"}\r\n",
        (unsigned long long)now, phase, (unsigned long)GetCurrentThreadId(),
        (unsigned long)fang_record_frame_sequence(),
        is_limit ? "null" : digits[0], req->IsMarkerIDAvailable ? digits[1] : "null",
        req->Phase == SCENARIO_MARKER_MASK_NORMAL_RETURN ? digits[2] : "null",
        req->IsCopyAttempted ? "true" : "false", req->IsMarkerIDAvailable ? "true" : "false",
        req->IsPositionAvailable ? "true" : "false", position_bits,
        req->IsCopyAttempted ? digits[3] : "null",
        req->CopyFailure == SCENARIO_MARKER_COPY_READ_FAILED ? digits[4] : "null",
        failure, normal_return, is_limit ? "true" : "false", recorded == NULL ? 2 : 0,
        (unsigned long)loss_flags);
    if (length <= 0 || (size_t)length >= sizeof(line)) {
        InterlockedOr(&recorder_loss_flags, 8);
        if (recorded != NULL && !HeapFree(GetProcessHeap(), 0, recorded)) {
            InterlockedOr(&recorder_loss_flags, 16);
        }
        SetLastError(last_error);
        return;
    }
    if (fang_record_trace_available()) {
        is_written = fang_record_trace_write(line, (DWORD)length, &written);
        if (!is_written || written != (DWORD)length) {
            InterlockedOr(&recorder_loss_flags, 2);
        }
    }
    if (recorded == NULL) {
        SetLastError(last_error);
        return;
    }
    write_field = strstr(line, "\"trace_write_status\":0");
    append_field = strstr(line, "\"ring_append_status\":0");
    loss_field = strstr(line, "\"recorder_loss_flags\":\"0x");
    if (write_field == NULL || append_field == NULL || loss_field == NULL) {
        InterlockedOr(&recorder_loss_flags, 8);
        if (!HeapFree(GetProcessHeap(), 0, recorded)) {
            InterlockedOr(&recorder_loss_flags, 16);
        }
        SetLastError(last_error);
        return;
    }
    loss_offset = (size_t)(loss_field - line) + strlen("\"recorder_loss_flags\":\"0x");
    if (loss_offset + 8 > (size_t)length) {
        InterlockedOr(&recorder_loss_flags, 8);
        if (!HeapFree(GetProcessHeap(), 0, recorded)) {
            InterlockedOr(&recorder_loss_flags, 16);
        }
        SetLastError(last_error);
        return;
    }
    write_field[strlen("\"trace_write_status\":")] =
        is_written && written == (DWORD)length ? '1' : '2';
    append_field[strlen("\"ring_append_status\":")] = '1';
    recorded->next = NULL;
    recorded->time_ms = now;
    recorded->size = (unsigned int)length;
    memcpy(recorded->contents, line, (size_t)length);
    AcquireSRWLockExclusive(&snapshot_ring_lock);
    if (snapshot_ring_tail == NULL) {
        snapshot_ring_head = recorded;
    } else {
        snapshot_ring_tail->next = recorded;
    }
    snapshot_ring_tail = recorded;
    snapshot_ring_byte_count += (size_t)length;
    while (snapshot_ring_head != NULL &&
        (snapshot_ring_byte_count > maximum_bytes ||
         (now >= snapshot_ring_head->time_ms &&
          now - snapshot_ring_head->time_ms > maximum_age_ms))) {
        snapshot_line* removed = snapshot_ring_head;
        int is_capacity_drop = snapshot_ring_byte_count > maximum_bytes;
        snapshot_ring_head = removed->next;
        snapshot_ring_byte_count -= removed->size;
        if (is_capacity_drop) {
            snapshot_ring_capacity_dropped_line_count++;
            snapshot_ring_capacity_dropped_byte_count += removed->size;
            snapshot_ring_capacity_last_dropped_time_ms = removed->time_ms;
        }
        if (!HeapFree(GetProcessHeap(), 0, removed)) {
            InterlockedOr(&recorder_loss_flags, 16);
        }
    }
    loss_flags = InterlockedCompareExchange(&recorder_loss_flags, 0, 0);
    length = snprintf(loss_digits, sizeof(loss_digits), "%08lx", (unsigned long)loss_flags);
    if (length == 8) {
        memcpy(recorded->contents + loss_offset, loss_digits, 8);
    } else {
        InterlockedOr(&recorder_loss_flags, 8);
    }
    ReleaseSRWLockExclusive(&snapshot_ring_lock);
    SetLastError(last_error);
}
#endif

static void encode_hex(char* destination, const BYTE* data, unsigned int size) {
    static const char hexadecimal[] = "0123456789abcdef";
    unsigned int index;
    for (index = 0; index < size; index++) {
        BYTE value = data[index];
        destination[index * 2] = hexadecimal[value >> 4];
        destination[index * 2 + 1] = hexadecimal[value & 0x0f];
    }
}

void snapshot_ring_append(const char* contents, unsigned int size) {
    const size_t maximum_bytes = 64u * 1024u * 1024u;
    const ULONGLONG maximum_age_ms = 300000;
    snapshot_line* line;
    ULONGLONG now;
    if (contents == NULL || size == 0 || size > maximum_bytes) {
        return;
    }
    line = (snapshot_line*)HeapAlloc(
        GetProcessHeap(), 0, sizeof(*line) - 1 + size);
    if (line == NULL) {
        return;
    }
    now = GetTickCount64();
    line->next = NULL;
    line->time_ms = now;
    line->size = size;
    memcpy(line->contents, contents, size);
    AcquireSRWLockExclusive(&snapshot_ring_lock);
    if (snapshot_ring_tail == NULL) {
        snapshot_ring_head = line;
    } else {
        snapshot_ring_tail->next = line;
    }
    snapshot_ring_tail = line;
    snapshot_ring_byte_count += size;
    while (snapshot_ring_head != NULL &&
        (snapshot_ring_byte_count > maximum_bytes ||
         now - snapshot_ring_head->time_ms > maximum_age_ms)) {
        snapshot_line* removed = snapshot_ring_head;
        int is_capacity_drop = snapshot_ring_byte_count > maximum_bytes;
        snapshot_ring_head = removed->next;
        snapshot_ring_byte_count -= removed->size;
        if (is_capacity_drop) {
            snapshot_ring_capacity_dropped_line_count++;
            snapshot_ring_capacity_dropped_byte_count += removed->size;
            snapshot_ring_capacity_last_dropped_time_ms = removed->time_ms;
        }
        HeapFree(GetProcessHeap(), 0, removed);
    }
    if (snapshot_ring_head == NULL) {
        snapshot_ring_tail = NULL;
    }
    ReleaseSRWLockExclusive(&snapshot_ring_lock);
}

void snapshot_ring_clear(void) {
    snapshot_line* line;
    AcquireSRWLockExclusive(&snapshot_ring_lock);
    line = snapshot_ring_head;
    snapshot_ring_head = NULL;
    snapshot_ring_tail = NULL;
    snapshot_ring_byte_count = 0;
    snapshot_ring_capacity_dropped_line_count = 0;
    snapshot_ring_capacity_dropped_byte_count = 0;
    snapshot_ring_capacity_last_dropped_time_ms = 0;
    while (line != NULL) {
        snapshot_line* next = line->next;
        HeapFree(GetProcessHeap(), 0, line);
        line = next;
    }
    ReleaseSRWLockExclusive(&snapshot_ring_lock);
}

void snapshot_ring_marker(const char* kind, const char* request,
    unsigned long long server_time_unix_nano, unsigned int buffer_ms,
    const char* keyframe_status, unsigned long long client_received_time_ms) {
    char line[768];
    int length;
    ULONGLONG oldest_time_ms = 0;
    ULONGLONG newest_time_ms = 0;
    ULONGLONG dropped_line_count;
    ULONGLONG dropped_byte_count;
    ULONGLONG last_dropped_time_ms;
    size_t byte_count;
    unsigned int line_count = 0;
    snapshot_line* ring_line;
    if (kind == NULL || request == NULL || keyframe_status == NULL) {
        return;
    }
    AcquireSRWLockShared(&snapshot_ring_lock);
    byte_count = snapshot_ring_byte_count;
    dropped_line_count = snapshot_ring_capacity_dropped_line_count;
    dropped_byte_count = snapshot_ring_capacity_dropped_byte_count;
    last_dropped_time_ms = snapshot_ring_capacity_last_dropped_time_ms;
    if (snapshot_ring_head != NULL) {
        oldest_time_ms = snapshot_ring_head->time_ms;
    }
    if (snapshot_ring_tail != NULL) {
        newest_time_ms = snapshot_ring_tail->time_ms;
    }
    for (ring_line = snapshot_ring_head; ring_line != NULL;
        ring_line = ring_line->next) {
        line_count++;
    }
    ReleaseSRWLockShared(&snapshot_ring_lock);
    length = snprintf(line, sizeof(line),
        "{\"time_ms\":%llu,\"protocol\":\"client_state\","
        "\"kind\":\"%s\",\"thread\":%lu,\"frame_sequence\":%lu,"
        "\"frame_delta_bits\":%lu,\"frame_time_ms\":%lu,\"request\":\"%s\","
        "\"server_time_unix_nano\":%llu,\"ring_line_count\":%u,"
        "\"ring_byte_count\":%llu,\"capacity_dropped_line_count\":%llu,"
        "\"capacity_dropped_byte_count\":%llu,\"oldest_time_ms\":%llu,"
        "\"newest_time_ms\":%llu,\"capacity_last_dropped_time_ms\":%llu,"
        "\"buffer_ms\":%u,\"keyframe_status\":\"%s\","
        "\"client_received_time_ms\":%llu,\"keyframe_started_time_ms\":%lu,"
        "\"keyframe_completed_time_ms\":%lu}\r\n",
        (unsigned long long)GetTickCount64(), kind,
        (unsigned long)GetCurrentThreadId(),
        (unsigned long)fang_record_frame_sequence(),
        (unsigned long)fang_record_frame_delta_bits(),
        (unsigned long)fang_record_frame_time_ms(),
        request, server_time_unix_nano, line_count,
        (unsigned long long)byte_count,
        (unsigned long long)dropped_line_count,
        (unsigned long long)dropped_byte_count,
        (unsigned long long)oldest_time_ms,
        (unsigned long long)newest_time_ms,
        (unsigned long long)last_dropped_time_ms, buffer_ms, keyframe_status,
        client_received_time_ms,
        (unsigned long)fang_record_keyframe_started_time_ms(),
        (unsigned long)fang_record_keyframe_completed_time_ms());
    if (length <= 0 || (size_t)length >= sizeof(line)) {
        return;
    }
    snapshot_ring_append(line, (unsigned int)length);
}

int valid_snapshot_directory_name(const char* name) {
    size_t index;
    size_t length;
    if (name == NULL) {
        return 0;
    }
    length = strlen(name);
    if (length == 0 || length >= 240) {
        return 0;
    }
    for (index = 0; index < length; index++) {
        unsigned char character = (unsigned char)name[index];
        if (!isalnum(character) && character != '-' && character != '_' &&
            character != '.') {
            return 0;
        }
    }
    return 1;
}

void snapshot_ring_dump(
    const char* directory_name, unsigned int buffer_ms) {
    char output_path[MAX_PATH * 4];
    char temporary_path[MAX_PATH * 4];
    snapshot_line* line;
    char* contents = NULL;
    size_t total = 0;
    size_t offset = 0;
    ULONGLONG now = GetTickCount64();
    ULONGLONG cutoff = now > buffer_ms ? now - buffer_ms : 0;
    HANDLE output;
    int is_complete = 0;
    int output_length;
    int temporary_length;
    if (!valid_snapshot_directory_name(directory_name) ||
        fang_record_directory()[0] == '\0') {
        return;
    }
    output_length = snprintf(output_path, sizeof(output_path),
        "%s\\snapshots\\%s\\client-memory.jsonl", fang_record_directory(), directory_name);
    if (output_length <= 0 || (size_t)output_length >= sizeof(output_path)) {
        return;
    }
    temporary_length = snprintf(
        temporary_path, sizeof(temporary_path), "%s.tmp", output_path);
    if (temporary_length <= 0 || (size_t)temporary_length >= sizeof(temporary_path)) {
        return;
    }
    AcquireSRWLockShared(&snapshot_ring_lock);
    for (line = snapshot_ring_head; line != NULL; line = line->next) {
        if (line->time_ms >= cutoff) {
            total += line->size;
        }
    }
    if (total != 0) {
        contents = (char*)HeapAlloc(GetProcessHeap(), 0, total);
    }
    if (total != 0 && contents != NULL) {
        for (line = snapshot_ring_head; line != NULL; line = line->next) {
            if (line->time_ms < cutoff) {
                continue;
            }
            memcpy(contents + offset, line->contents, line->size);
            offset += line->size;
        }
    }
    ReleaseSRWLockShared(&snapshot_ring_lock);
    if (total != 0 && contents == NULL) {
        return;
    }
    output = CreateFileA(
        temporary_path, GENERIC_WRITE, FILE_SHARE_READ,
        NULL, CREATE_ALWAYS, FILE_ATTRIBUTE_NORMAL, NULL);
    if (output != INVALID_HANDLE_VALUE) {
        size_t written_total = 0;
        while (written_total < total) {
            DWORD requested = (DWORD)(total - written_total);
            DWORD written = 0;
            if (!WriteFile(output, contents + written_total, requested, &written, NULL) ||
                written == 0) {
                break;
            }
            written_total += written;
        }
        if (written_total == total && FlushFileBuffers(output)) {
            is_complete = 1;
        }
        CloseHandle(output);
    }
    if (is_complete) {
        if (!MoveFileExA(temporary_path, output_path,
            MOVEFILE_REPLACE_EXISTING | MOVEFILE_WRITE_THROUGH)) {
            DeleteFileA(temporary_path);
        }
    } else {
        DeleteFileA(temporary_path);
    }
    if (contents != NULL) {
        HeapFree(GetProcessHeap(), 0, contents);
    }
}

void trace_hex_line(
    const char* header, int header_length,
    const BYTE* data, unsigned int size, const char* suffix) {
    size_t suffix_length = strlen(suffix);
    size_t total;
    char* line;
    DWORD written;
    if (header == NULL || header_length <= 0 || data == NULL || suffix == NULL ||
        size > (SIZE_MAX - (size_t)header_length - suffix_length) / 2) {
        return;
    }
    total = (size_t)header_length + (size_t)size * 2 + suffix_length;
    if (total > UINT_MAX) {
        return;
    }
    line = (char*)HeapAlloc(GetProcessHeap(), 0, total);
    if (line == NULL) {
        return;
    }
    memcpy(line, header, (size_t)header_length);
    encode_hex(line + header_length, data, size);
    memcpy(line + header_length + size * 2, suffix, suffix_length);
    if (fang_record_trace_available()) {
        fang_record_trace_write(line, (DWORD)total, &written);
    }
    snapshot_ring_append(line, (unsigned int)total);
    HeapFree(GetProcessHeap(), 0, line);
}


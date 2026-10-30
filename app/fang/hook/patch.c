#include "hook/patch.h"

#include <stdio.h>
#include <string.h>

enum { hook_capacity = 1024, hook_byte_capacity = 32 };
enum hook_failure {
    hook_ok, hook_argument, hook_unreadable, hook_expected, hook_conflict,
    hook_capacity_exceeded, hook_protection, hook_flush, hook_restore
};

typedef struct hook_record {
    const char* owner;
    const char* name;
    const char* evidence;
    const char* source;
    unsigned int line;
    BYTE* target;
    size_t length;
    BYTE original[hook_byte_capacity];
    BYTE replacement[hook_byte_capacity];
    enum hook_failure failure;
    DWORD system_error;
    DWORD protection;
    int is_protection_known;
    int is_installed;
    int is_removed;
    int is_cleanup_pending;
    uint64_t generation;
    int is_code;
} hook_record;

static SRWLOCK hook_lock = SRWLOCK_INIT;
static hook_record hook_records[hook_capacity];
static unsigned int hook_count;
static uint64_t hook_generation;
static volatile LONG hook_capacity_rejections;

/* Callers hold hook_lock. Save/restore whole instructions, not just rel32,
 * and never restore over a patch that no longer belongs to this record. */
static int hook_write(hook_record* record, const BYTE* bytes) {
    DWORD protection;
    DWORD ignored;
    if (!VirtualProtect(record->target, record->length,
        record->is_code ? PAGE_EXECUTE_READWRITE : PAGE_READWRITE, &protection)) {
        record->failure = hook_protection;
        record->system_error = GetLastError();
        return 0;
    }
    if (!record->is_protection_known) {
        record->protection = protection;
        record->is_protection_known = 1;
    }
    if (!record->is_code && record->length == sizeof(void*)) {
        void* pointer;
        memcpy(&pointer, bytes, sizeof(pointer));
        *(void**)record->target = pointer;
    } else {
        memcpy(record->target, bytes, record->length);
    }
    if (record->is_code && !FlushInstructionCache(GetCurrentProcess(), record->target, record->length)) {
        record->failure = hook_flush;
        record->system_error = GetLastError();
    }
    if (!VirtualProtect(record->target, record->length, record->protection, &ignored)) {
        record->failure = hook_restore;
        record->system_error = GetLastError();
    }
    record->is_cleanup_pending = record->failure != hook_ok;
    return !record->is_cleanup_pending;
}

static int hook_remove(hook_record* record) {
    enum hook_failure previous_failure = record->failure;
    DWORD previous_error = record->system_error;
    int is_success;
    if (!record->is_installed && !record->is_cleanup_pending) return 1;
    if (!readable_range(record->target, record->length) ||
        memcmp(record->target, record->is_installed ? record->replacement : record->original, record->length) != 0) {
        record->failure = hook_conflict;
        return 0;
    }
    record->failure = hook_ok;
    record->system_error = 0;
    hook_write(record, record->original);
    if (memcmp(record->target, record->original, record->length) == 0) {
        record->is_installed = 0;
        record->is_removed = 1;
    }
    is_success = !record->is_installed && record->failure == hook_ok;
    if (is_success && previous_failure != hook_ok) {
        record->failure = previous_failure;
        record->system_error = previous_error;
    }
    return is_success;
}

static hook_record* hook_record_for(const char* owner, const char* name, BYTE* target,
    size_t length, unsigned int* index) {
    unsigned int ordinal;
    for (ordinal = 0; ordinal < hook_count; ordinal++) {
        hook_record* record = &hook_records[ordinal];
        if (record->target == target && record->length == length &&
            strcmp(record->owner, owner) == 0 && strcmp(record->name, name) == 0) {
            *index = ordinal;
            return record;
        }
    }
    if (hook_count == hook_capacity) return NULL;
    *index = hook_count++;
    return &hook_records[*index];
}

static int hook_apply(fang_hook_set* set, const char* owner, const char* name,
    const char* evidence, const char* source, unsigned int line, void* target,
    const BYTE* expected, const BYTE* replacement, size_t length, int is_legacy, int is_code) {
    hook_record* record;
    unsigned int index;
    unsigned int ordinal;
    int is_success;
    if (owner == NULL || name == NULL || length == 0 || length > hook_byte_capacity || replacement == NULL) return 0;
    AcquireSRWLockExclusive(&hook_lock);
    if (set != NULL && (set->count >= FANG_HOOK_SET_CAPACITY || set->is_committed || set->generation == 0)) {
        ReleaseSRWLockExclusive(&hook_lock);
        return 0;
    }
    /* Existing feature rollback calls express the inverse checked operation.
     * Recognize them rather than registering a second owner over the first. */
    if (is_legacy && expected != NULL) {
        for (ordinal = 0; ordinal < hook_count; ordinal++) {
            record = &hook_records[ordinal];
            if (record->generation == 0 && (record->is_installed || record->is_cleanup_pending) &&
                record->target == target && record->length == length &&
                memcmp(expected, record->replacement, length) == 0 &&
                memcmp(replacement, record->original, length) == 0) {
                is_success = hook_remove(record);
                ReleaseSRWLockExclusive(&hook_lock);
                return is_success;
            }
        }
    }
    record = hook_record_for(owner, name, target, length, &index);
    if (record == NULL) {
        ReleaseSRWLockExclusive(&hook_lock);
        InterlockedIncrement(&hook_capacity_rejections);
        return 0;
    }
    record->owner = owner;
    record->name = name;
    record->evidence = evidence;
    record->source = source;
    record->line = line;
    record->target = (BYTE*)target;
    record->length = length;
    record->failure = hook_ok;
    record->system_error = 0;
    if (!readable_range(target, length)) record->failure = hook_unreadable;
    else if (expected != NULL && memcmp(target, expected, length) != 0) record->failure = hook_expected;
    if (record->failure == hook_ok) {
        for (ordinal = 0; ordinal < hook_count; ordinal++) {
            hook_record* other = &hook_records[ordinal];
            if (!other->is_installed && !other->is_cleanup_pending) continue;
            if ((uintptr_t)target < (uintptr_t)other->target + other->length &&
                (uintptr_t)other->target < (uintptr_t)target + length) {
                record->failure = hook_conflict;
                break;
            }
        }
    }
    if (record->failure != hook_ok) {
        ReleaseSRWLockExclusive(&hook_lock);
        return 0;
    }
    memcpy(record->original, target, length);
    memcpy(record->replacement, replacement, length);
    record->is_removed = 0;
    record->is_protection_known = 0;
    record->is_cleanup_pending = 0;
    record->generation = set == NULL ? 0 : set->generation;
    record->is_code = is_code;
    /* Track even a partially failed write, so rollback can still restore it. */
    record->is_installed = 1;
    if (set != NULL) set->records[set->count++] = index;
    is_success = hook_write(record, replacement);
    if (!is_success && memcmp(target, replacement, length) != 0) record->is_installed = 0;
    if (!is_success && set == NULL) hook_remove(record);
    ReleaseSRWLockExclusive(&hook_lock);
    return is_success;
}

void fang_hook_begin(fang_hook_set* set, const char* feature) {
    if (set == NULL || feature == NULL) return;
    AcquireSRWLockExclusive(&hook_lock);
    if (set->count != 0) {
        ReleaseSRWLockExclusive(&hook_lock);
        return;
    }
    memset(set, 0, sizeof(*set));
    set->feature = feature;
    hook_generation++;
    if (hook_generation == 0) hook_generation++;
    set->generation = hook_generation;
    ReleaseSRWLockExclusive(&hook_lock);
}

static int hook_call_bytes(void* instruction, void* expected_target, void* replacement,
    BYTE* expected, BYTE* patched) {
    int32_t relative;
    if (instruction == NULL || replacement == NULL) return 0;
    expected[0] = patched[0] = 0xE8;
    relative = (int32_t)((uintptr_t)expected_target - ((uintptr_t)instruction + 5));
    memcpy(expected + 1, &relative, sizeof(relative));
    relative = (int32_t)((uintptr_t)replacement - ((uintptr_t)instruction + 5));
    memcpy(patched + 1, &relative, sizeof(relative));
    return 1;
}

int fang_hook_call(fang_hook_set* set, const char* name, void* instruction,
    void* expected_target, void* replacement, const char* evidence) {
    BYTE expected[5], patched[5];
    if (set == NULL || !hook_call_bytes(instruction, expected_target, replacement, expected, patched)) return 0;
    return hook_apply(set, set->feature, name, evidence, NULL, 0, instruction, expected, patched, 5, 0, 1);
}

int fang_hook_pointer(fang_hook_set* set, const char* name, void** target,
    void* expected, void* replacement, const char* evidence) {
    if (set == NULL || replacement == NULL) return 0;
    return hook_apply(set, set->feature, name, evidence, NULL, 0, target,
        (const BYTE*)&expected, (const BYTE*)&replacement, sizeof(void*), 0, 0);
}

int fang_hook_bytes(fang_hook_set* set, const char* name, void* target,
    const BYTE* expected, const BYTE* replacement, size_t length, const char* evidence) {
    if (set == NULL || expected == NULL) return 0;
    return hook_apply(set, set->feature, name, evidence, NULL, 0, target, expected, replacement, length, 0, 1);
}

void fang_hook_commit(fang_hook_set* set) {
    AcquireSRWLockExclusive(&hook_lock);
    set->is_committed = 1;
    ReleaseSRWLockExclusive(&hook_lock);
}

int fang_hook_rollback(fang_hook_set* set) {
    int is_success = 1;
    unsigned int pending[FANG_HOOK_SET_CAPACITY];
    unsigned int pending_count = 0;
    unsigned int index;
    AcquireSRWLockExclusive(&hook_lock);
    while (set->count != 0) {
        unsigned int record_index = set->records[--set->count];
        hook_record* record = &hook_records[record_index];
        if (record->generation != set->generation) {
            is_success = 0;
            continue;
        }
        if (!hook_remove(record)) {
            pending[pending_count++] = record_index;
            is_success = 0;
        }
    }
    for (index = 0; index < pending_count; index++) set->records[index] = pending[pending_count - index - 1];
    set->count = pending_count;
    set->is_committed = 0;
    ReleaseSRWLockExclusive(&hook_lock);
    return is_success;
}

int fang_hook_legacy_call(const char* owner, const char* name, const char* source, unsigned int line,
    void* instruction, void* expected_target, void* replacement) {
    BYTE expected[5], patched[5];
    if (!hook_call_bytes(instruction, expected_target, replacement, expected, patched)) return 0;
    return hook_apply(NULL, owner, name, "legacy-source-site", source, line,
        instruction, expected, patched, 5, 1, 1);
}

int fang_hook_legacy_pointer(const char* owner, const char* name, const char* source, unsigned int line,
    void** target, void* expected, void* replacement) {
    if (replacement == NULL) return 0;
    return hook_apply(NULL, owner, name, "legacy-source-site", source, line,
        target, (const BYTE*)&expected, (const BYTE*)&replacement, sizeof(void*), 1, 0);
}

int fang_hook_legacy_jump(const char* owner, const char* name, const char* source, unsigned int line,
    void* target, void* replacement) {
    BYTE patched[5] = {0xE9};
    int32_t relative;
    if (target == NULL || replacement == NULL) return 0;
    relative = (int32_t)((uintptr_t)replacement - ((uintptr_t)target + 5));
    memcpy(patched + 1, &relative, sizeof(relative));
    return hook_apply(NULL, owner, name, "legacy-source-site", source, line, target, NULL, patched, 5, 1, 1);
}

int fang_hook_legacy_bytes(const char* owner, const char* name, const char* source, unsigned int line,
    void* target, const BYTE* replacement, size_t length) {
    return hook_apply(NULL, owner, name, "legacy-source-site", source, line, target, NULL, replacement, length, 1, 1);
}

/* Source paths are compiler-owned. Normalize separators and escape all JSON
 * metacharacters before handing a line to the existing bug-report recorder. */
static void hook_json_text(char* output, size_t capacity, const char* text) {
    size_t count = 0;
    if (text == NULL) text = "";
    while (*text != 0 && count + 2 < capacity) {
        unsigned char character = (unsigned char)*text++;
        if (character == '\\') character = '/';
        if (character < 32) continue;
        if (character == '"') output[count++] = '\\';
        output[count++] = (char)character;
    }
    output[count] = 0;
}

void fang_hook_report(void (*write_line)(const char*, unsigned int)) {
    unsigned int index, count;
    DWORD previous_error = GetLastError();
    static const char* failures[] = {"none", "invalid_argument", "unreadable",
        "expected_bytes_mismatch", "ownership_conflict", "capacity_exceeded",
        "write_protection", "instruction_flush", "protection_restore"};
    if (write_line == NULL) return;
    AcquireSRWLockShared(&hook_lock);
    count = hook_count;
    ReleaseSRWLockShared(&hook_lock);
    for (index = 0; index < count; index++) {
        hook_record record;
        char owner[192], name[128], evidence[256], source[256], line[1200];
        int length;
        AcquireSRWLockShared(&hook_lock);
        record = hook_records[index];
        ReleaseSRWLockShared(&hook_lock);
        hook_json_text(owner, sizeof(owner), record.owner);
        hook_json_text(name, sizeof(name), record.name);
        hook_json_text(evidence, sizeof(evidence), record.evidence);
        hook_json_text(source, sizeof(source), record.source);
        length = snprintf(line, sizeof(line),
            "{\"time_ms\":%llu,\"protocol\":\"fang_hook\",\"build\":103,\"feature\":\"%s\",\"symbol\":\"%s\","
            "\"address\":%llu,\"size\":%u,\"is_installed\":%s,\"is_removed\":%s,\"is_cleanup_pending\":%s,"
            "\"failure\":\"%s\",\"system_error\":%lu,\"evidence\":\"%s\",\"source\":\"%s\",\"line\":%u}\r\n",
            (unsigned long long)GetTickCount64(), owner, name, (unsigned long long)(uintptr_t)record.target, (unsigned int)record.length,
            record.is_installed ? "true" : "false", record.is_removed ? "true" : "false",
            record.is_cleanup_pending ? "true" : "false",
            failures[record.failure], (unsigned long)record.system_error, evidence, source, record.line);
        if (length > 0 && (size_t)length < sizeof(line)) write_line(line, (unsigned int)length);
    }
    {
        char line[160];
        int length = snprintf(line, sizeof(line),
            "{\"protocol\":\"fang_hook_summary\",\"records\":%u,\"capacity_rejections\":%ld}\r\n",
            count, (long)InterlockedCompareExchange(&hook_capacity_rejections, 0, 0));
        if (length > 0 && (size_t)length < sizeof(line)) write_line(line, (unsigned int)length);
    }
    SetLastError(previous_error);
}

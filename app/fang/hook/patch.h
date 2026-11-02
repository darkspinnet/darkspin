#ifndef DARKSPIN_FANG_PATCH_H
#define DARKSPIN_FANG_PATCH_H

#include "fang.h"
#include <stddef.h>
#include <stdint.h>

enum { FANG_HOOK_SET_CAPACITY = 32 };

/* Zero-initialized, feature-owned installation transaction. Begin/install/
 * commit run in the owner's initialization sequence. Failed rollback keeps
 * its records for retry. Removal does not make live DLL hot-unload safe. */
typedef struct fang_hook_set {
    const char* feature;
    unsigned int records[FANG_HOOK_SET_CAPACITY];
    unsigned int count;
    int is_committed;
    uint64_t generation;
} fang_hook_set;

int readable_range(const void* pointer, size_t size);
int fang_readable_pointer(const void* pointer);
BYTE* fang_find_signature(HMODULE module, const BYTE* signature, size_t length);

void fang_hook_begin(fang_hook_set* set, const char* feature);
int fang_hook_call(fang_hook_set* set, const char* name, void* instruction,
    void* expected_target, void* replacement, const char* evidence);
int fang_hook_pointer(fang_hook_set* set, const char* name, void** target,
    void* expected, void* replacement, const char* evidence);
int fang_hook_bytes(fang_hook_set* set, const char* name, void* target,
    const BYTE* expected, const BYTE* replacement, size_t length,
    const char* evidence);
void fang_hook_commit(fang_hook_set* set);
int fang_hook_rollback(fang_hook_set* set);
void fang_hook_report(void (*write_line)(const char*, unsigned int));

/* Existing callers retain their checked semantics and gain patch tracking.
 * The function and source location identify legacy bindings until migrated. */
int fang_hook_legacy_call(const char* owner, const char* name, const char* source, unsigned int line,
    void* instruction, void* expected_target, void* replacement);
int fang_hook_legacy_pointer(const char* owner, const char* name, const char* source, unsigned int line,
    void** target, void* expected, void* replacement);
int fang_hook_legacy_jump(const char* owner, const char* name, const char* source, unsigned int line,
    void* target, void* replacement);
int fang_hook_legacy_bytes(const char* owner, const char* name, const char* source, unsigned int line,
    void* target, const BYTE* replacement, size_t length);

#define patch_call(instruction, expected, replacement) \
    fang_hook_legacy_call(__func__, #replacement, __FILE__, __LINE__, instruction, expected, replacement)
#define patch_pointer(target, expected, replacement) \
    fang_hook_legacy_pointer(__func__, #replacement, __FILE__, __LINE__, target, expected, replacement)
#define patch_jump(target, replacement) \
    fang_hook_legacy_jump(__func__, #replacement, __FILE__, __LINE__, target, replacement)
#define patch_bytes(target, replacement, length) \
    fang_hook_legacy_bytes(__func__, #target, __FILE__, __LINE__, target, replacement, length)

#endif

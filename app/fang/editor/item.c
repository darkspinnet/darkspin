#include "editor/editor.h"
#include "hook.h"

#include <stdint.h>
#include <string.h>

typedef int (FANG_THISCALL *editor_available_fn)(BYTE*, const BYTE*, BYTE*);
typedef void (FANG_THISCALL *editor_message_fn)(BYTE*, unsigned int, unsigned int, int);
typedef void* (__cdecl *editor_account_fn)(void);
typedef const BYTE* (FANG_THISCALL *editor_entry_fn)(void*, unsigned int, unsigned int);
typedef unsigned char (FANG_THISCALL *editor_mirror_fn)(const BYTE*);

static editor_available_fn native_editor_available;
static editor_message_fn native_editor_message;
static editor_account_fn native_editor_account;
static editor_entry_fn native_editor_entry;
static editor_mirror_fn native_editor_mirror;
static uint64_t editor_last_reserved_id;

/* sub_4CC100 matches physical item IDs against the live creature's parts
 * and their mirrored children. sub_69C050/sub_69D010 expose the creature's
 * pointer vector at [24,28); placed-part IDs live at [40,48). Use that live
 * geometry at editor +176, as passed by native callers to sub_4CC100, rather
 * than the separate editor +156 field or the last saved equipment set.
 * Only a new palette placement is rejected; existing mirrored hands/feet
 * and ordinary manipulation of the attached geometry remain native. */
// Reads: checks a live part's item ID, returning an error indicator for unreadable memory.
static int editor_part_has_item(const BYTE* part, uint64_t item_id) {
    uint64_t attached_id;
    if (!readable_range(part, 48)) {
        return -1;
    }
    memcpy(&attached_id, part + 40, sizeof(attached_id));
    return attached_id == item_id;
}

// Reads: searches live parts and mirrored children for an item ID, reporting unreadable geometry.
static int editor_model_has_item(const BYTE* model, uint64_t item_id) {
    const BYTE* const* parts;
    const BYTE* const* end;
    if (model == NULL) {
        return 0;
    }
    if (!readable_range(model, 32)) {
        return -1;
    }
    parts = *(const BYTE* const**)(model + 24);
    end = *(const BYTE* const**)(model + 28);
    if ((uintptr_t)end < (uintptr_t)parts ||
        ((uintptr_t)end - (uintptr_t)parts) % sizeof(*parts) != 0 ||
        ((uintptr_t)end - (uintptr_t)parts) / sizeof(*parts) > 4096 ||
        (parts != end && !readable_range(parts, (uintptr_t)end - (uintptr_t)parts))) {
        return -1;
    }
    for (; parts != end; ++parts) {
        const BYTE* part = *parts;
        const BYTE* const* children;
        const BYTE* const* child_end;
        int is_present;
        if (part == NULL) {
            continue;
        }
        is_present = editor_part_has_item(part, item_id);
        if (is_present != 0) {
            return is_present;
        }
        if (!readable_range(part, 61)) {
            return -1;
        }
        if (!native_editor_mirror(part)) {
            continue;
        }
        if (!readable_range(part, 844)) {
            return -1;
        }
        children = *(const BYTE* const**)(part + 836);
        child_end = *(const BYTE* const**)(part + 840);
        if ((uintptr_t)child_end < (uintptr_t)children ||
            ((uintptr_t)child_end - (uintptr_t)children) % sizeof(*children) != 0 ||
            ((uintptr_t)child_end - (uintptr_t)children) / sizeof(*children) > 4096 ||
            (children != child_end && !readable_range(children, (uintptr_t)child_end - (uintptr_t)children))) {
            return -1;
        }
        for (; children != child_end; ++children) {
            if (*children == NULL) {
                continue;
            }
            is_present = editor_part_has_item(*children, item_id);
            if (is_present != 0) {
                return is_present;
            }
        }
    }
    return 0;
}

// Modifies: populates native unavailable-item error messages and returns the locked result.
static int editor_item_locked(BYTE* error) {
    if (error != NULL && readable_range(error, 48)) {
        /* Same unavailable-item message and validation result as 4C39D0. */
        native_editor_message(error, 1231813414, 0x103, 0);
        native_editor_message(error + 20, 1231813414, 0x103, 0);
        *(unsigned int*)(error + 44) = 583304543;
    }
    return 2;
}

// Reads/modifies: checks ownership and live reservations, updates diagnostics, and delegates native validation or writes a locked error.
static int FANG_THISCALL editor_available(BYTE* collection, const BYTE* item, BYTE* error) {
    const BYTE* editor;
    const BYTE* entry;
    void* account;
    uint64_t item_id;
    int is_reserved;
    if (!readable_range(collection, 60) || !readable_range(item, 124)) {
        return editor_item_locked(error);
    }
    memcpy(&item_id, item + 112, sizeof(item_id));
    if (item_id == 0) {
        return native_editor_available(collection, item, error);
    }
    account = native_editor_account();
    if (account == NULL) {
        return editor_item_locked(error);
    }
    entry = native_editor_entry(account, (unsigned int)item_id, (unsigned int)(item_id >> 32));
    if (!readable_range(entry, 80) || *(const unsigned int*)(entry + 32) != 0) {
        return editor_item_locked(error);
    }
    editor = *(const BYTE**)(collection + 56);
    if (!readable_range(editor, 180)) {
        return native_editor_available(collection, item, error);
    }
    is_reserved = editor_model_has_item(*(const BYTE**)(editor + 176), item_id);
    if (is_reserved != 0) {
        if (editor_last_reserved_id != item_id) {
            trace_client_state(is_reserved < 0 ? "editor_item_model_invalid" :
                "editor_item_reserved", (unsigned int)item_id);
            editor_last_reserved_id = item_id;
        }
        /* An unreadable model is not evidence that this item is equipped.
         * Keep the native stock/eligibility result in that case. */
        if (is_reserved < 0) {
            return native_editor_available(collection, item, error);
        }
        return editor_item_locked(error);
    }
    editor_last_reserved_id = 0;
    return native_editor_available(collection, item, error);
}

// Modifies: binds editor helpers and replaces the native item availability callback.
int fang_install_editor_item(HMODULE executable) {
    BYTE* base = (BYTE*)executable;
    void** available = (void**)(base + 0xBD9FB4);
    if (sizeof(void*) != 4 || !readable_range(available, sizeof(*available)) ||
        *available != base + 0xC39D0) {
        return 0;
    }
    native_editor_available = (editor_available_fn)(base + 0xC39D0);
    native_editor_message = (editor_message_fn)(base + 0x3AE8A0);
    native_editor_account = (editor_account_fn)(base + 0xE5AC0);
    native_editor_entry = (editor_entry_fn)(base + 0xC0FD0);
    native_editor_mirror = (editor_mirror_fn)(base + 0x2A2470);
    return patch_pointer(available, native_editor_available, editor_available);
}

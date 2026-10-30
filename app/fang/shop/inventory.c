#include "shop/shop.h"
#include "hook.h"

#include <stdint.h>
#include <string.h>

typedef unsigned char (FANG_THISCALL *shop_editor_part_fn)(void*, const BYTE*, int);
static shop_editor_part_fn native_editor_part;
static fang_hook_set inventory_hooks;

static unsigned char FANG_THISCALL shop_editor_part(void* palette,
    const BYTE* entry, int is_update) {
    /* Build 103 sub_4BDFF0 checks genesis, class and equipped hero but not
     * market status. Both palette construction and incremental updates must
     * exclude unpurchased offers and sold items. Native code still owns the
     * handling of parts attached to the hero currently being edited. */
    if (!readable_range(entry, 80) || *(const unsigned int*)(entry + 32) != 0) {
        return 0;
    }
    return native_editor_part(palette, entry, is_update);
}

/* At 431DC0 EDI is the live inventory entry. Replace only the Sell Value
 * localization selection; the existing half-cost math and server quote
 * bridge remain intact. 097AA9A1 is the client's normal purchase Cost text. */
static void __attribute__((naked)) shop_cost_label(void) {
    __asm__(
        "pushfl\n"
        "mov $0x0b638b18, %eax\n"
        "test %edi, %edi\n"
        "jz 1f\n"
        "cmpl $1, 32(%edi)\n"
        "jne 1f\n"
        "mov $0x097aa9a1, %eax\n"
        "1: popfl\n"
        "ret\n");
}

int fang_install_shop_inventory(HMODULE executable) {
    static const unsigned int editor_calls[] = {0xBE2C6, 0xBE758, 0xBFE69, 0xC0457};
    static const BYTE label_signature[] = {0xB8, 0x18, 0x8B, 0x63, 0x0B};
    BYTE* base = (BYTE*)executable;
    BYTE* label = base + 0x31DC0;
    BYTE replacement[5] = {0xE8, 0, 0, 0, 0};
    unsigned int index;
    int32_t relative;
    native_editor_part = (shop_editor_part_fn)(base + 0xC8E20);
    if (sizeof(void*) != 4 || !readable_range(label, 5) ||
        memcmp(label, label_signature, 5) != 0) {
        return 0;
    }
    for (index = 0; index < 4; ++index) {
        BYTE* call = base + editor_calls[index];
        if (!readable_range(call, 5) || call[0] != 0xE8) {
            return 0;
        }
        memcpy(&relative, call + 1, 4);
        if (call + 5 + relative != (BYTE*)native_editor_part) {
            return 0;
        }
    }
    fang_hook_begin(&inventory_hooks, "shopInventory");
    for (index = 0; index < 4; ++index) {
        if (!fang_hook_call(&inventory_hooks, "editorPart", base + editor_calls[index],
            native_editor_part, shop_editor_part, "build103: existing editor part CALL target")) {
            goto rollback;
        }
    }
    relative = (int32_t)((BYTE*)shop_cost_label - (label + 5));
    memcpy(replacement + 1, &relative, 4);
    if (!fang_hook_bytes(&inventory_hooks, "costLabel", label, label_signature,
        replacement, sizeof(replacement), "build103: existing cost label MOV signature")) {
        goto rollback;
    }
    fang_hook_commit(&inventory_hooks);
    return 1;
rollback:
    if (!fang_hook_rollback(&inventory_hooks)) {
        trace_client_state("shop_inventory_rollback_failed", 1);
    }
    return 0;
}

#include "client103/shop.h"

#include <string.h>

/* RVAs and checks migrated from the existing build-103 shop pricing bridge.
 * No build-127 address, layout or prologue is an admission check here. */
enum {
    shop_parse_rva = 0xACC60,
    shop_loot_rva = 0x5CD7C0,
    shop_account_rva = 0xE5AC0,
    shop_funds_rva = 0x35904
};

typedef struct shop_call_definition {
    const char* name;
    uint32_t rva;
    int is_parser;
    const char* evidence;
} shop_call_definition;

static const shop_call_definition shop_calls[] = {
    {"parse_call_4ad74e", 0xAD74E, 1,
        "build103: existing shop parser CALL 004AD74E -> 004ACC60"},
    {"parse_call_4ae42f", 0xAE42F, 1,
        "build103: existing shop parser CALL 004AE42F -> 004ACC60"},
    {"price_call_43159d", 0x3159D, 0,
        "build103: existing shop loot CALL 0043159D -> 009CD7C0"},
    {"price_call_4358c9", 0x358C9, 0,
        "build103: existing shop loot CALL 004358C9 -> 009CD7C0"},
    {"price_call_436d51", 0x36D51, 0,
        "build103: existing shop loot CALL 00436D51 -> 009CD7C0"},
    {"price_call_436e43", 0x36E43, 0,
        "build103: existing shop loot CALL 00436E43 -> 009CD7C0"}
};

static const BYTE shop_funds_signature[FANG_CLIENT103_SHOP_FUNDS_SIZE] = {
    0x39, 0x6E, 0x24, 0x76, 0x09
};

// Reads/modifies: validates build-103 shop targets, writes resolved bindings, and records validation diagnostics.
int fang_client103_bind_shop(HMODULE executable, fang_client103_shop* binding) {
    fang_client103_shop resolved;
    unsigned int index;
    if (binding == NULL || sizeof(void*) != 4) {
        return 0;
    }
    memset(&resolved, 0, sizeof(resolved));
    if (!fang_client103_image_open(executable, shop_loot_rva + 1, &resolved.image)) {
        return 0;
    }
    resolved.parse = (fang_client103_shop_parse_fn)fang_client103_at(
        &resolved.image, shop_parse_rva, 1);
    resolved.loot = (fang_client103_shop_loot_fn)fang_client103_at(
        &resolved.image, shop_loot_rva, 1);
    resolved.account = (fang_client103_shop_account_fn)fang_client103_at(
        &resolved.image, shop_account_rva, 1);
    if (resolved.parse == NULL || resolved.loot == NULL || resolved.account == NULL) {
        return 0;
    }
    for (index = 0; index < FANG_CLIENT103_SHOP_CALL_COUNT; ++index) {
        const shop_call_definition* definition = &shop_calls[index];
        fang_client103_shop_call* call = &resolved.calls[index];
        BYTE expected[FANG_CLIENT103_SHOP_CALL_SIZE] = {0xE8, 0, 0, 0, 0};
        uint32_t relative;
        call->name = definition->name;
        call->evidence = definition->evidence;
        call->is_parser = definition->is_parser;
        call->instruction = (BYTE*)fang_client103_at(
            &resolved.image, definition->rva, sizeof(expected));
        call->expected_target = call->is_parser ?
            (void*)resolved.parse : (void*)resolved.loot;
        if (call->instruction == NULL) {
            return 0;
        }
        relative = (uint32_t)((uintptr_t)call->expected_target -
            (uintptr_t)call->instruction - sizeof(expected));
        memcpy(expected + 1, &relative, sizeof(relative));
        if (!fang_client103_expect(&resolved.image, "shop_pricing", call->name,
                definition->rva, expected, sizeof(expected), call->evidence)) {
            return 0;
        }
    }
    resolved.funds_name = "funds_compare_435904";
    resolved.funds_evidence =
        "build103: 00435904 cmp [esi+24h],ebp / jbe +9; preserve registers and x87";
    resolved.funds_signature = shop_funds_signature;
    resolved.funds = (BYTE*)fang_client103_at(
        &resolved.image, shop_funds_rva, sizeof(shop_funds_signature));
    if (resolved.funds == NULL ||
        !fang_client103_expect(&resolved.image, "shop_pricing", resolved.funds_name,
            shop_funds_rva, shop_funds_signature, sizeof(shop_funds_signature),
            resolved.funds_evidence)) {
        return 0;
    }
    *binding = resolved;
    return 1;
}

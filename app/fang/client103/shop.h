#ifndef DARKSPIN_FANG_CLIENT103_SHOP_H
#define DARKSPIN_FANG_CLIENT103_SHOP_H

#include "fang.h"
#include "client103/image.h"
#include <stdint.h>
#include <stddef.h>

/* Existing build-103 pricing bridge contracts. These are native function
 * pointers, not feature callbacks; all three functions use cdecl on x86. */
typedef void* (__cdecl* fang_client103_shop_parse_fn)(void* output, void* nodes);
typedef unsigned char (__cdecl* fang_client103_shop_loot_fn)(void* output,
    const void* loot, int is_local, int unknown);
typedef void* (__cdecl* fang_client103_shop_account_fn)(void);

enum {
    FANG_CLIENT103_SHOP_CALL_COUNT = 6,
    FANG_CLIENT103_SHOP_CALL_SIZE = 5,
    FANG_CLIENT103_SHOP_FUNDS_SIZE = 5,
    FANG_CLIENT103_SHOP_VECTOR_SIZE = 12,
    FANG_CLIENT103_SHOP_VECTOR_BEGIN = 0,
    FANG_CLIENT103_SHOP_VECTOR_END = 4,
    FANG_CLIENT103_SHOP_ENTRY_SIZE = 80,
    FANG_CLIENT103_SHOP_ENTRY_REFERENCE = 8,
    FANG_CLIENT103_SHOP_ENTRY_MARKET_STATUS = 24,
    FANG_CLIENT103_SHOP_ENTRY_PRICE = 28,
    FANG_CLIENT103_SHOP_ENTRY_CATEGORY = 32,
    FANG_CLIENT103_SHOP_ENTRY_IS_OWNED = 40,
    FANG_CLIENT103_SHOP_ACCOUNT_READ_SIZE = 26936,
    FANG_CLIENT103_SHOP_ACCOUNT_BEGIN = 26928,
    FANG_CLIENT103_SHOP_ACCOUNT_END = 26932,
    FANG_CLIENT103_SHOP_LOOT_REFERENCE_SIZE = 8,
    FANG_CLIENT103_SHOP_OUTPUT_PRICE = 8,
    FANG_CLIENT103_SHOP_CONTROLLER_DNA = 36
};

/* The complete parsed vector element is authored as an 80-byte record.
 * This is not the layout or size of the owning account/loot objects. */
typedef struct fang_client103_shop_entry_view {
    BYTE before_reference[8];
    uint64_t reference;
    BYTE before_status[8];
    uint32_t market_status;
    uint32_t price;
    uint32_t category;
    BYTE before_owned[4];
    uint8_t is_owned;
    BYTE remaining[39];
} fang_client103_shop_entry_view;

_Static_assert(sizeof(fang_client103_shop_entry_view) == FANG_CLIENT103_SHOP_ENTRY_SIZE, "shop entry size");
_Static_assert(offsetof(fang_client103_shop_entry_view, reference) == FANG_CLIENT103_SHOP_ENTRY_REFERENCE, "shop reference offset");
_Static_assert(offsetof(fang_client103_shop_entry_view, market_status) == FANG_CLIENT103_SHOP_ENTRY_MARKET_STATUS, "shop status offset");
_Static_assert(offsetof(fang_client103_shop_entry_view, price) == FANG_CLIENT103_SHOP_ENTRY_PRICE, "shop price offset");
_Static_assert(offsetof(fang_client103_shop_entry_view, category) == FANG_CLIENT103_SHOP_ENTRY_CATEGORY, "shop category offset");
_Static_assert(offsetof(fang_client103_shop_entry_view, is_owned) == FANG_CLIENT103_SHOP_ENTRY_IS_OWNED, "shop ownership offset");

typedef struct fang_client103_shop_call {
    const char* name;
    BYTE* instruction;
    void* expected_target;
    const char* evidence;
    int is_parser;
} fang_client103_shop_call;

typedef struct fang_client103_shop {
    fang_client103_image image;
    fang_client103_shop_parse_fn parse;
    fang_client103_shop_loot_fn loot;
    fang_client103_shop_account_fn account;
    fang_client103_shop_call calls[FANG_CLIENT103_SHOP_CALL_COUNT];
    BYTE* funds;
    const BYTE* funds_signature;
    const char* funds_name;
    const char* funds_evidence;
} fang_client103_shop;

/* Resolve and validate every consumed code range before any installation. */
int fang_client103_bind_shop(HMODULE executable, fang_client103_shop* binding);

#endif

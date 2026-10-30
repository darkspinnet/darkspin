#include "shop/shop.h"
#include "hook.h"
#include "client103/shop.h"
#include "hook/patch.h"

#include <stdint.h>
#include <string.h>

/* Build 127 sub_511210 prices Parts offers independently of resale, using
 * trunc(level cost * server rarity multiplier). Build 103 parses XML cost
 * in sub_4ACC60 but drops it in sub_4C1990. Retain the server's final price
 * here; do not alter loot levels, stats, rarity or the global resale helper.
 * Server and Fang share the 2^23 limit so doubling the price for 103's
 * float32 half-cost helper is exact and fits its signed integer result. */
enum { shop_capacity = 4096, shop_max_price = 1 << 23 };

typedef struct shop_offer {
    uint64_t reference_id;
    unsigned int price;
} shop_offer;

static fang_client103_shop shop_binding;
static fang_hook_set shop_hooks;
static SRWLOCK shop_lock = SRWLOCK_INIT;
static shop_offer shop_offers[shop_capacity];
static unsigned int shop_next_slot;
static volatile LONG is_shop_enabled;

// Modifies: updates or inserts a cached server offer price under the shop lock.
static void shop_record(uint64_t reference_id, unsigned int price) {
    unsigned int index;
    AcquireSRWLockExclusive(&shop_lock);
    for (index = 0; index < shop_capacity; ++index) {
        if (shop_offers[index].reference_id == reference_id) {
            shop_offers[index].price = price;
            ReleaseSRWLockExclusive(&shop_lock);
            return;
        }
    }
    if (price == 0) {
        ReleaseSRWLockExclusive(&shop_lock);
        return;
    }
    shop_offers[shop_next_slot].reference_id = reference_id;
    shop_offers[shop_next_slot].price = price;
    shop_next_slot = (shop_next_slot + 1) % shop_capacity;
    ReleaseSRWLockExclusive(&shop_lock);
}

// Reads: returns the cached offer price under the shop lock.
static unsigned int shop_price(uint64_t reference_id) {
    unsigned int index;
    unsigned int price = 0;
    AcquireSRWLockShared(&shop_lock);
    for (index = 0; index < shop_capacity; ++index) {
        if (shop_offers[index].reference_id == reference_id) {
            price = shop_offers[index].price;
            break;
        }
    }
    ReleaseSRWLockShared(&shop_lock);
    return price;
}

// Modifies: invokes native offer parsing, updates the price cache, and logs valid quotes.
static void* __cdecl shop_parse(void* output, void* nodes) {
    void* result = shop_binding.parse(output, nodes);
    const BYTE* entry;
    const BYTE* end;
    if (!InterlockedCompareExchange(&is_shop_enabled, 0, 0) ||
        !readable_range(output, FANG_CLIENT103_SHOP_VECTOR_SIZE)) {
        return result;
    }
    entry = *(const BYTE**)((const BYTE*)output + FANG_CLIENT103_SHOP_VECTOR_BEGIN);
    end = *(const BYTE**)((const BYTE*)output + FANG_CLIENT103_SHOP_VECTOR_END);
    if ((uintptr_t)end < (uintptr_t)entry ||
        ((uintptr_t)end - (uintptr_t)entry) % FANG_CLIENT103_SHOP_ENTRY_SIZE != 0 ||
        ((uintptr_t)end - (uintptr_t)entry) / FANG_CLIENT103_SHOP_ENTRY_SIZE > shop_capacity ||
        !readable_range(entry, (uintptr_t)end - (uintptr_t)entry)) {
        return result;
    }
    for (; entry != end; entry += FANG_CLIENT103_SHOP_ENTRY_SIZE) {
        uint64_t reference_id = *(const uint64_t*)(entry + FANG_CLIENT103_SHOP_ENTRY_REFERENCE);
        unsigned int price = *(const unsigned int*)(entry + FANG_CLIENT103_SHOP_ENTRY_PRICE);
        /* Parsed market status has already been converted from wire 2 to 1.
         * Non-offers invalidate a prior quote for this account-scoped ID. */
        if (reference_id == 0) {
            continue;
        }
        if (*(const unsigned int*)(entry + FANG_CLIENT103_SHOP_ENTRY_MARKET_STATUS) != 1 ||
            price == 0 || price > shop_max_price) {
            shop_record(reference_id, 0);
            continue;
        }
        shop_record(reference_id, price);
        trace_client_state("shop_offer_price", price);
    }
    return result;
}

// Reads: finds the unowned Parts account entry matching a loot reference.
static const BYTE* shop_entry(const void* loot) {
    const BYTE* account = (const BYTE*)shop_binding.account();
    const BYTE* entry;
    const BYTE* end;
    uint64_t reference_id;
    if (!readable_range(loot, FANG_CLIENT103_SHOP_LOOT_REFERENCE_SIZE) ||
        !readable_range(account, FANG_CLIENT103_SHOP_ACCOUNT_READ_SIZE)) {
        return NULL;
    }
    reference_id = *(const uint64_t*)loot;
    entry = *(const BYTE**)(account + FANG_CLIENT103_SHOP_ACCOUNT_BEGIN);
    end = *(const BYTE**)(account + FANG_CLIENT103_SHOP_ACCOUNT_END);
    if ((uintptr_t)end < (uintptr_t)entry ||
        ((uintptr_t)end - (uintptr_t)entry) % FANG_CLIENT103_SHOP_ENTRY_SIZE != 0 ||
        ((uintptr_t)end - (uintptr_t)entry) / FANG_CLIENT103_SHOP_ENTRY_SIZE > shop_capacity ||
        !readable_range(entry, (uintptr_t)end - (uintptr_t)entry)) {
        return NULL;
    }
    for (; entry != end; entry += FANG_CLIENT103_SHOP_ENTRY_SIZE) {
        if (*(const uint64_t*)(entry + FANG_CLIENT103_SHOP_ENTRY_REFERENCE) == reference_id &&
            *(const unsigned int*)(entry + FANG_CLIENT103_SHOP_ENTRY_CATEGORY) == 1 &&
            entry[FANG_CLIENT103_SHOP_ENTRY_IS_OWNED] == 0) {
            return entry;
        }
    }
    return NULL;
}

// Modifies: invokes native loot resolution and writes the server quote into its price output, logging missing quotes.
static unsigned char __cdecl shop_loot(void* output, const void* loot,
    int is_local, int unknown) {
    unsigned char is_resolved = shop_binding.loot(output, loot, is_local, unknown);
    const BYTE* entry;
    unsigned int price;
    if (!is_resolved || !InterlockedCompareExchange(&is_shop_enabled, 0, 0)) {
        return is_resolved;
    }
    entry = shop_entry(loot);
    if (entry == NULL) {
        return is_resolved;
    }
    price = shop_price(*(const uint64_t*)(entry + FANG_CLIENT103_SHOP_ENTRY_REFERENCE));
    if (price == 0) {
        /* Missing/invalid quotes cannot silently fall back to a cheaper price. */
        trace_client_state("shop_offer_unpriced", 1);
        return 0;
    }
    *(unsigned int*)((BYTE*)output + FANG_CLIENT103_SHOP_OUTPUT_PRICE) = price * 2;
    return is_resolved;
}

/* Only a Parts offer gets build 127's >= check. Native buyback retains >.
 * This call replaces cmp [esi+24],ebp / jbe at 435904, preserving all registers
 * and the x87 stack; the following capacity check remains entirely native. */
static int __cdecl shop_has_funds(const BYTE* controller, unsigned int price,
    const BYTE* entry) __attribute__((used, noinline));

// Reads: checks native funds, allowing an exact balance for a valid quoted Parts offer.
static int __cdecl shop_has_funds(const BYTE* controller, unsigned int price,
    const BYTE* entry) {
    unsigned int dna = *(const unsigned int*)(controller + FANG_CLIENT103_SHOP_CONTROLLER_DNA);
    if (dna > price) {
        return 1;
    }
    return InterlockedCompareExchange(&is_shop_enabled, 0, 0) &&
        dna == price && *(const unsigned int*)(entry + FANG_CLIENT103_SHOP_ENTRY_CATEGORY) == 1 &&
        entry[FANG_CLIENT103_SHOP_ENTRY_IS_OWNED] == 0 && price != 0 &&
        shop_price(*(const uint64_t*)(entry + FANG_CLIENT103_SHOP_ENTRY_REFERENCE)) == price;
}

// Modifies: redirects the native return path when the offer funds check fails while preserving registers and flags.
static void __attribute__((naked)) shop_funds_bridge(void) {
    __asm__(
        "pushfl\n"
        "pushal\n"
        "push %edi\n"
        "push %ebp\n"
        "push %esi\n"
        "call _shop_has_funds\n"
        "add $12, %esp\n"
        "test %eax, %eax\n"
        "jz 1f\n"
        "popal\n"
        "popfl\n"
        "ret\n"
        "1: popal\n"
        "popfl\n"
        "addl $9, (%esp)\n"
        "ret\n");
}

// Modifies: patches shop parsing, loot pricing, and funds checks and updates feature bookkeeping.
int fang_install_shop_pricing(HMODULE executable) {
    fang_client103_shop binding;
    BYTE funds_call[FANG_CLIENT103_SHOP_FUNDS_SIZE] = {0xE8, 0, 0, 0, 0};
    unsigned int index;
    uint32_t relative;
    if (!fang_client103_bind_shop(executable, &binding)) {
        return 0;
    }
    shop_binding = binding;
    fang_hook_begin(&shop_hooks, "shop_pricing");
    for (index = 0; index < FANG_CLIENT103_SHOP_CALL_COUNT; ++index) {
        const fang_client103_shop_call* call = &binding.calls[index];
        void* replacement = call->is_parser ? (void*)shop_parse : (void*)shop_loot;
        if (!fang_hook_call(&shop_hooks, call->name, call->instruction,
                call->expected_target, replacement, call->evidence)) {
            goto rollback;
        }
    }
    relative = (uint32_t)((uintptr_t)shop_funds_bridge -
        (uintptr_t)binding.funds - sizeof(funds_call));
    memcpy(funds_call + 1, &relative, sizeof(relative));
    if (!fang_hook_bytes(&shop_hooks, binding.funds_name, binding.funds,
            binding.funds_signature, funds_call, sizeof(funds_call), binding.funds_evidence)) {
        goto rollback;
    }
    fang_hook_commit(&shop_hooks);
    InterlockedExchange(&is_shop_enabled, 1);
    return 1;

rollback:
    if (!fang_hook_rollback(&shop_hooks)) {
        trace_client_state("shop_pricing_rollback_failed", shop_hooks.count);
    }
    return 0;
}

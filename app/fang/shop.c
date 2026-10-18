#include "hook.h"

#include <stdint.h>
#include <string.h>

typedef void* (__cdecl* shop_parse_fn)(void* output, void* nodes);
typedef unsigned char (__cdecl* shop_loot_fn)(void* output, const void* loot,
    int is_local, int unknown);
typedef void* (__cdecl* shop_account_fn)(void);

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

static shop_parse_fn native_shop_parse;
static shop_loot_fn native_shop_loot;
static shop_account_fn native_shop_account;
static SRWLOCK shop_lock = SRWLOCK_INIT;
static shop_offer shop_offers[shop_capacity];
static unsigned int shop_next_slot;
static volatile LONG is_shop_enabled;

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

static void* __cdecl shop_parse(void* output, void* nodes) {
    void* result = native_shop_parse(output, nodes);
    const BYTE* entry;
    const BYTE* end;
    if (!InterlockedCompareExchange(&is_shop_enabled, 0, 0) ||
        !readable_range(output, 12)) {
        return result;
    }
    entry = *(const BYTE**)output;
    end = *(const BYTE**)((const BYTE*)output + 4);
    if ((uintptr_t)end < (uintptr_t)entry ||
        ((uintptr_t)end - (uintptr_t)entry) % 80 != 0 ||
        ((uintptr_t)end - (uintptr_t)entry) / 80 > shop_capacity ||
        !readable_range(entry, (uintptr_t)end - (uintptr_t)entry)) {
        return result;
    }
    for (; entry != end; entry += 80) {
        uint64_t reference_id = *(const uint64_t*)(entry + 8);
        unsigned int price = *(const unsigned int*)(entry + 28);
        /* Parsed market status has already been converted from wire 2 to 1.
         * Non-offers invalidate a prior quote for this account-scoped ID. */
        if (reference_id == 0) {
            continue;
        }
        if (*(const unsigned int*)(entry + 24) != 1 ||
            price == 0 || price > shop_max_price) {
            shop_record(reference_id, 0);
            continue;
        }
        shop_record(reference_id, price);
        trace_client_state("shop_offer_price", price);
    }
    return result;
}

static const BYTE* shop_entry(const void* loot) {
    const BYTE* account = (const BYTE*)native_shop_account();
    const BYTE* entry;
    const BYTE* end;
    uint64_t reference_id;
    if (!readable_range(loot, 8) || !readable_range(account, 26936)) {
        return NULL;
    }
    reference_id = *(const uint64_t*)loot;
    entry = *(const BYTE**)(account + 26928);
    end = *(const BYTE**)(account + 26932);
    if ((uintptr_t)end < (uintptr_t)entry ||
        ((uintptr_t)end - (uintptr_t)entry) % 80 != 0 ||
        ((uintptr_t)end - (uintptr_t)entry) / 80 > shop_capacity ||
        !readable_range(entry, (uintptr_t)end - (uintptr_t)entry)) {
        return NULL;
    }
    for (; entry != end; entry += 80) {
        if (*(const uint64_t*)(entry + 8) == reference_id &&
            *(const unsigned int*)(entry + 32) == 1 && entry[40] == 0) {
            return entry;
        }
    }
    return NULL;
}

static unsigned char __cdecl shop_loot(void* output, const void* loot,
    int is_local, int unknown) {
    unsigned char is_resolved = native_shop_loot(output, loot, is_local, unknown);
    const BYTE* entry;
    unsigned int price;
    if (!is_resolved || !InterlockedCompareExchange(&is_shop_enabled, 0, 0)) {
        return is_resolved;
    }
    entry = shop_entry(loot);
    if (entry == NULL) {
        return is_resolved;
    }
    price = shop_price(*(const uint64_t*)(entry + 8));
    if (price == 0) {
        /* Missing/invalid quotes cannot silently fall back to a cheaper price. */
        trace_client_state("shop_offer_unpriced", 1);
        return 0;
    }
    *(unsigned int*)((BYTE*)output + 8) = price * 2;
    return is_resolved;
}

/* Only a Parts offer gets build 127's >= check. Native buyback retains >.
 * This call replaces cmp [esi+24],ebp / jbe at 435904, preserving all registers
 * and the x87 stack; the following capacity check remains entirely native. */
static int __cdecl shop_has_funds(const BYTE* controller, unsigned int price,
    const BYTE* entry) __attribute__((used, noinline));

static int __cdecl shop_has_funds(const BYTE* controller, unsigned int price,
    const BYTE* entry) {
    unsigned int dna = *(const unsigned int*)(controller + 36);
    if (dna > price) {
        return 1;
    }
    return InterlockedCompareExchange(&is_shop_enabled, 0, 0) &&
        dna == price && *(const unsigned int*)(entry + 32) == 1 &&
        entry[40] == 0 && price != 0 &&
        shop_price(*(const uint64_t*)(entry + 8)) == price;
}

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

static int shop_is_call(const BYTE* instruction, const void* target) {
    int32_t relative;
    if (!readable_range(instruction, 5) || instruction[0] != 0xE8) {
        return 0;
    }
    memcpy(&relative, instruction + 1, sizeof(relative));
    return instruction + 5 + relative == (const BYTE*)target;
}

int fang_install_shop_pricing(HMODULE executable) {
    static const unsigned int parse_calls[] = {0xAD74E, 0xAE42F};
    static const unsigned int price_calls[] = {0x3159D, 0x358C9, 0x36D51, 0x36E43};
    static const BYTE funds_signature[] = {0x39, 0x6E, 0x24, 0x76, 0x09};
    BYTE* base = (BYTE*)executable;
    IMAGE_DOS_HEADER* dos = (IMAGE_DOS_HEADER*)base;
    IMAGE_NT_HEADERS* nt;
    BYTE* funds = base + 0x35904;
    BYTE funds_call[5] = {0xE8, 0, 0, 0, 0};
    BYTE* calls[6];
    void* originals[6];
    void* replacements[6];
    unsigned int index;
    unsigned int installed = 0;
    DWORD old_protection;
    DWORD ignored;
    int32_t relative;
    if (sizeof(void*) != 4 || !readable_range(dos, sizeof(*dos)) ||
        dos->e_magic != IMAGE_DOS_SIGNATURE || dos->e_lfanew <= 0) {
        return 0;
    }
    nt = (IMAGE_NT_HEADERS*)(base + dos->e_lfanew);
    if (!readable_range(nt, sizeof(*nt)) || nt->Signature != IMAGE_NT_SIGNATURE ||
        nt->FileHeader.Machine != IMAGE_FILE_MACHINE_I386 ||
        nt->OptionalHeader.SizeOfImage < 0xE5AD0) {
        return 0;
    }
    native_shop_parse = (shop_parse_fn)(base + 0xACC60);
    native_shop_loot = (shop_loot_fn)(base + 0x5CD7C0);
    native_shop_account = (shop_account_fn)(base + 0xE5AC0);
    for (index = 0; index < 6; ++index) {
        int is_parser = index < 2;
        calls[index] = base + (is_parser ? parse_calls[index] : price_calls[index - 2]);
        originals[index] = is_parser ? (void*)native_shop_parse : (void*)native_shop_loot;
        replacements[index] = is_parser ? (void*)shop_parse : (void*)shop_loot;
        if (!shop_is_call(calls[index], originals[index])) {
            return 0;
        }
    }
    if (!readable_range(funds, sizeof(funds_signature)) ||
        memcmp(funds, funds_signature, sizeof(funds_signature)) != 0) {
        return 0;
    }
    for (index = 0; index < 6; ++index) {
        if (!patch_call(calls[index], originals[index], replacements[index])) {
            goto rollback;
        }
        ++installed;
    }
    if (!VirtualProtect(funds, sizeof(funds_call), PAGE_EXECUTE_READWRITE, &old_protection)) {
        goto rollback;
    }
    relative = (int32_t)((BYTE*)shop_funds_bridge - (funds + 5));
    memcpy(funds_call + 1, &relative, sizeof(relative));
    memcpy(funds, funds_call, sizeof(funds_call));
    FlushInstructionCache(GetCurrentProcess(), funds, sizeof(funds_call));
    VirtualProtect(funds, sizeof(funds_call), old_protection, &ignored);
    InterlockedExchange(&is_shop_enabled, 1);
    return 1;

rollback:
    while (installed != 0) {
        --installed;
        if (!patch_call(calls[installed], replacements[installed], originals[installed])) {
            trace_client_state("shop_pricing_rollback_failed", installed);
        }
    }
    return 0;
}

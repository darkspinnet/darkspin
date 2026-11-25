#include "tooltip/tooltip.h"
#include "client103/image.h"
#include "hook.h"

#include <stdint.h>
#include <string.h>

typedef int (__cdecl* register_modifier_fn)(BYTE* state);

static register_modifier_fn native_register_modifier;
static fang_hook_set tooltip_hooks;

/* Build 103 uses float Lua numbers: TValue=8, Node=20, CallInfo=24.
 * Only the installed FieldMedicPassive TranslateToken prototype is changed.
 * The authored chunk is neither copied into Fang nor modified on disk. */

// Reads: compares a validated installed Lua string with a bounded literal.
static int lua_string_equals(BYTE* string, const char* expected) {
    size_t length = strlen(expected);
    return readable_range(string, 16 + length + 1) && string[4] == 4 &&
        *(uint32_t*)(string + 12) == length &&
        memcmp(string + 16, expected, length + 1) == 0;
}

// Reads: checks one string constant in the installed float-based Lua prototype.
static int constant_equals(BYTE* constants, unsigned int index, const char* expected) {
    BYTE* entry = constants + index * 8;
    return *(uint32_t*)(entry + 4) == 4 &&
        lua_string_equals(*(BYTE**)entry, expected);
}

// Reads: encodes the build-103 Lua 5.1 ABC instruction fields.
static uint32_t instruction(unsigned int opcode, unsigned int a,
    unsigned int b, unsigned int c) {
    return opcode | (a << 6) | (c << 14) | (b << 23);
}

// Reads: validates the complete bounded writable instruction allocation.
static int writable_code(void* code, size_t length) {
    MEMORY_BASIC_INFORMATION region;
    DWORD protection;
    if (VirtualQuery(code, &region, sizeof(region)) != sizeof(region) ||
        region.State != MEM_COMMIT || (region.Protect & PAGE_GUARD) != 0 ||
        (uintptr_t)code < (uintptr_t)region.BaseAddress ||
        (uintptr_t)code - (uintptr_t)region.BaseAddress > region.RegionSize ||
        length > region.RegionSize - ((uintptr_t)code - (uintptr_t)region.BaseAddress)) return 0;
    protection = region.Protect & 0xff;
    return protection == PAGE_READWRITE || protection == PAGE_WRITECOPY ||
        protection == PAGE_EXECUTE_READWRITE || protection == PAGE_EXECUTE_WRITECOPY;
}

// Modifies: keeps calculated tooltip damage in registers instead of the shared rank table.
static int repair_translation(BYTE* prototype) {
    static const unsigned int slots[] = {12, 13, 22, 23, 37, 38, 47, 48};
    BYTE* constants;
    uint32_t* code;
    uint32_t expected[8], replacements[8];
    unsigned int index;
    if (!readable_range(prototype, 76) || prototype[4] != 9 ||
        *(uint32_t*)(prototype + 40) != 16 ||
        *(uint32_t*)(prototype + 44) != 57 ||
        *(uint32_t*)(prototype + 60) != 33 ||
        *(uint32_t*)(prototype + 64) != 51 ||
        prototype[72] != 0 || prototype[73] != 2 ||
        prototype[74] != 0 || prototype[75] != 9) return 0;
    constants = *(BYTE**)(prototype + 8);
    code = *(uint32_t**)(prototype + 12);
    if (!readable_range(constants, 16 * 8) || !readable_range(code, 57 * 4) ||
        !writable_code(code, 57 * 4) ||
        !constant_equals(constants, 0, "petMinDamage") ||
        !constant_equals(constants, 1, "nAbility_SentryDroneLaser") ||
        !constant_equals(constants, 2, "damage") ||
        !constant_equals(constants, 6, "nModifier_FieldMedicPassive") ||
        !constant_equals(constants, 7, "petPrimaryStatOffset") ||
        !constant_equals(constants, 9, "GetAttributeValue") ||
        !constant_equals(constants, 11, "PetDamage") ||
        !constant_equals(constants, 12, "petMaxDamage") ||
        code[11] != instruction(14, 5, 5, 6) ||
        code[21] != instruction(14, 5, 5, 6) ||
        code[36] != instruction(14, 5, 5, 6) ||
        code[46] != instruction(14, 5, 5, 6)) return 0;
    for (index = 0; index < 8; ++index) {
        unsigned int offset = index % 4;
        unsigned int key = index < 4 ? 261 : 269;
        expected[index] = offset == 0 || offset == 2 ?
            instruction(9, 3, key, 5) :
            instruction(6, offset == 1 ? 5 : 2, 3, key);
        replacements[index] = instruction(0, offset < 2 ? 5 : 2, 5, 0);
    }
    for (index = 0; index < 8; ++index) {
        if (code[slots[index]] != expected[index]) return 0;
    }
    /* RegisterModifier is executing the owning root chunk, before it assigns
     * or invokes TranslateToken. No closure/table is retained across Lua GC. */
    for (index = 0; index < 8; ++index) code[slots[index]] = replacements[index];
    return 1;
}

// Reads/modifies: validates the named registration's caller and repairs its unexecuted child.
static void repair_registered_modifier(BYTE* state) {
    BYTE* base;
    BYTE* top;
    BYTE* call;
    BYTE* function;
    BYTE* closure;
    BYTE* root;
    BYTE** children;
    if (!readable_range(state, 24)) return;
    top = *(BYTE**)(state + 8);
    base = *(BYTE**)(state + 12);
    if ((uintptr_t)top < (uintptr_t)base ||
        (uintptr_t)top - (uintptr_t)base < 16 || !readable_range(base, 16) ||
        *(uint32_t*)(base + 4) != 4 ||
        !lua_string_equals(*(BYTE**)base, "FieldMedicPassive")) return;
    call = *(BYTE**)(state + 20);
    if ((uintptr_t)call < 24 || !readable_range(call - 24, 24)) goto rejected;
    function = *(BYTE**)(call - 24 + 4);
    if (!readable_range(function, 8) || *(uint32_t*)(function + 4) != 6) goto rejected;
    closure = *(BYTE**)function;
    if (!readable_range(closure, 20) || closure[4] != 6 || closure[6] != 0) goto rejected;
    root = *(BYTE**)(closure + 16);
    if (!readable_range(root, 76) || root[4] != 9 ||
        *(uint32_t*)(root + 44) != 106 ||
        *(uint32_t*)(root + 52) != 5) goto rejected;
    children = *(BYTE***)(root + 16);
    if (!readable_range(children, 5 * sizeof(*children)) ||
        !repair_translation(children[1])) goto rejected;
    trace_client_state("sentry_tooltip_repaired", 1);
    return;
rejected:
    trace_client_state("sentry_tooltip_layout_rejected", 1);
}

// Reads/modifies: repairs only Sentry Drone translation, then performs ordinary registration.
static int __cdecl register_modifier(BYTE* state) {
    repair_registered_modifier(state);
    return native_register_modifier(state);
}

// Modifies: substitutes the checked Lua registration callback in the loaded build-103 image.
int fang_tooltip_install(HMODULE executable) {
    fang_client103_image image;
    BYTE expected[8] = {0xc7, 0x44, 0x24, 0x78};
    BYTE replacement[8];
    BYTE* site;
    void* callback = register_modifier;
    if (!fang_client103_image_open(executable, 0xc2d1b9, &image)) return 0;
    site = fang_client103_binding(&image, "sentryTooltip", "registerModifierSlot",
        0x6687af, 8, "build103 REA: MOV [esp+78], nModifier.RegisterModifier");
    native_register_modifier = (register_modifier_fn)fang_client103_binding(&image,
        "sentryTooltip", "registerModifier", 0x667d70, 9,
        "build103 REA: native Lua C callback, cdecl lua_State*");
    if (site == NULL || native_register_modifier == NULL ||
        !readable_range(image.base + 0x6687ab, 4) ||
        *(BYTE**)(image.base + 0x6687ab) != image.base + 0xc2d1a8 ||
        memcmp(image.base + 0xc2d1a8, "RegisterModifier", 17) != 0) return 0;
    memcpy(expected + 4, &native_register_modifier, 4);
    memcpy(replacement, expected, sizeof(replacement));
    memcpy(replacement + 4, &callback, 4);
    fang_hook_begin(&tooltip_hooks, "sentryTooltip");
    if (!fang_hook_bytes(&tooltip_hooks, "registerModifierCallback", site,
        expected, replacement, sizeof(expected),
        "build103: checked installed registration initializer; user-approved tooltip repair")) return 0;
    fang_hook_commit(&tooltip_hooks);
    return 1;
}

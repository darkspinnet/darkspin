#include "hook.h"

#include <stdint.h>
#include <string.h>
#include <wchar.h>

typedef void (FANG_THISCALL* tip_ref_fn)(BYTE*);
typedef BYTE* (FANG_THISCALL* tip_hover_fn)(BYTE*);
typedef const wchar_t* (FANG_THISCALL* tip_caption_fn)(BYTE*);
typedef void (FANG_THISCALL* tip_caption_set_fn)(BYTE*, const wchar_t*);

static BYTE* tip_base;
static void* tip_card_table[8];

/* Keep real localized names. Only unfinished internal labels need this
 * readable fallback; do not invent gameplay stats or equipment ownership. */
static int tip_name(const wchar_t* source, wchar_t* output, unsigned int capacity) {
    unsigned int index = 0;
    unsigned int count = 0;
    wchar_t previous = 0;
    int is_internal = 0;
    while (index < 255 && readable_range(source + index, sizeof(wchar_t)) && source[index]) {
        if (source[index] == L'_') is_internal = 1;
        ++index;
    }
    if (!index || index == 255 || !readable_range(source + index, sizeof(wchar_t))) return 0;
    if (wcsstr(source, L"PLACEHOLDER")) is_internal = 1;
    if (!is_internal) return 0;
    if (wcsncmp(source, L"ce_", 3) == 0 || wcsncmp(source, L"sa_", 3) == 0 ||
        wcsncmp(source, L"ta_", 3) == 0 || wcsncmp(source, L"ac_", 3) == 0) source += 3;
    for (index = 0; source[index] && count + 2 < capacity; ++index) {
        wchar_t letter = source[index];
        wchar_t next = source[index + 1];
        int is_upper = letter >= L'A' && letter <= L'Z';
        int is_lower = letter >= L'a' && letter <= L'z';
        if (letter == L'(' || wcsncmp(source + index, L"-symmetric", 10) == 0) break;
        if (letter == L'_' || letter == L'-' || letter == L' ') {
            if (count && output[count - 1] != L' ') output[count++] = L' ';
            previous = 0;
            continue;
        }
        if (is_upper && count && output[count - 1] != L' ' &&
            ((previous >= L'a' && previous <= L'z') ||
             (previous >= L'A' && previous <= L'Z' && next >= L'a' && next <= L'z')))
            output[count++] = L' ';
        if (is_lower && (!previous || (count && output[count - 1] == L' ')))
            letter = letter - L'a' + L'A';
        output[count++] = letter;
        previous = letter;
    }
    while (count && output[count - 1] == L' ') --count;
    output[count] = 0;
    return count != 0;
}

static BYTE* FANG_THISCALL tip_card_hover(BYTE* card) {
    BYTE* result = ((tip_hover_fn)(tip_base + 0x352040))(card);
    BYTE* manager = ((BYTE* (__cdecl*)(void))(tip_base + 0x264280))();
    BYTE* tooltip;
    BYTE* title;
    const wchar_t* caption;
    wchar_t name[256];
    if (!manager || !card[28]) return result;
    tooltip = ((BYTE* (FANG_THISCALL*)(BYTE*))(tip_base + 0x463970))(manager);
    if (!readable_range(tooltip, 152)) return result;
    title = *(BYTE**)(tooltip + 148);
    if (!readable_range(title, 4) || !readable_range(*(void**)title, 132)) return result;
    caption = ((tip_caption_fn)(*(void***)title)[15])(title);
    if (!caption || !tip_name(caption, name, sizeof(name) / sizeof(name[0]))) return result;
    ((tip_caption_set_fn)(*(void***)title)[32])(title, name);
    return result;
}

void fang_mod_palette_tip_card(BYTE* base, BYTE* card) {
    if (!readable_range(card, 4) || *(void**)card != base + 0xbfba08 ||
        !readable_range(base + 0xbfba04, sizeof(tip_card_table))) return;
    tip_base = base;
    memcpy(tip_card_table, base + 0xbfba04, sizeof(tip_card_table));
    tip_card_table[6] = tip_card_hover;
    *(void***)card = tip_card_table + 1;
}

static const wchar_t* tip_category_name(BYTE* source) {
    const wchar_t* name;
    if (!readable_range(source, 104)) return L"Body parts";
    name = ((const wchar_t* (FANG_THISCALL*)(BYTE*))(tip_base + 0x3ae420))(source + 84);
    if (name && readable_range(name, 8) && name[0] && wcscmp(name, L"***") != 0) return name;
    /* Authored fallback labels from native Editors.package. These categories
     * reference unavailable localization tables, which resolve to ***. Their
     * private catalogue sequence numbers are stable; preserve other locales. */
    switch (*(unsigned int*)(source + 64)) {
    case 10: return L"Tech";
    case 20: return L"Plasma";
    case 30: return L"BioGenesis";
    case 40: return L"Spacetime";
    case 50: return L"Necro";
    case 60: return L"Armor";
    case 70: return L"Class Weapons";
    case 700: return L"Galactic Adventures";
    case 800: return L"Cell Parts";
    case 100000: return L"Labs User";
    default: return L"Body parts";
    }
}

int fang_mod_palette_tip_categories(BYTE* base, BYTE* panel) {
    BYTE** buttons;
    BYTE** categories;
    unsigned int count;
    unsigned int index;
    uintptr_t begin;
    uintptr_t end;
    if (!readable_range(panel, 76)) return 0;
    begin = *(uintptr_t*)(panel + 68);
    end = *(uintptr_t*)(panel + 72);
    if (end < begin || (end - begin) % 4 || end - begin > 256 * 4 ||
        (end != begin && !readable_range((void*)begin, end - begin))) return 0;
    count = (end - begin) / 4;
    categories = *(BYTE***)(panel + 52);
    if (*(uintptr_t*)(panel + 56) - (uintptr_t)categories != count * 4 ||
        !readable_range(categories, count * 4)) return 0;
    buttons = (BYTE**)begin;
    tip_base = base;
    for (index = 0; index < count; ++index) {
        BYTE* window;
        BYTE* tooltip;
        BYTE* allocation;
        BYTE* allocator;
        const wchar_t* name;
        if (!readable_range(buttons[index], 4) ||
            !readable_range(*(void**)buttons[index], 20) ||
            !readable_range(categories[index], 112)) return 0;
        window = ((BYTE* (FANG_THISCALL*)(BYTE*))(*(void***)buttons[index])[4])(buttons[index]);
        if (!readable_range(window, 4) || !readable_range(*(void**)window, 264)) return 0;
        name = tip_category_name(*(BYTE**)(categories[index] + 108));
        allocator = ((BYTE* (__cdecl*)(void))(base + 0x7de7d0))();
        allocation = ((BYTE* (__cdecl*)(unsigned int, unsigned int, const char*, BYTE*))
            (base + 0x7de7e0))(104, 4, "Darkspin/PartsTooltip", allocator);
        if (!allocation) return 0;
        tooltip = ((BYTE* (FANG_THISCALL*)(BYTE*))(base + 0x51ce20))(allocation);
        ((tip_ref_fn)(*(void***)tooltip)[0])(tooltip);
        ((void (FANG_THISCALL*)(BYTE*, const wchar_t*, int, unsigned char))
            (base + 0x51d030))(tooltip, name, -1, 0);
        ((void (FANG_THISCALL*)(BYTE*, BYTE*))(*(void***)window)[65])(window, tooltip);
        ((tip_ref_fn)(*(void***)tooltip)[1])(tooltip);
    }
    trace_client_state("mod_editor_parts_category_tooltips", count);
    return 1;
}

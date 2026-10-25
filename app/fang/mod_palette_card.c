#include "hook.h"

#include <stdint.h>
#include <string.h>

/* User-approved Mod palette compatibility. Equipment cards require controls
 * absent from the creature layout (4D3C73). Use the retained generic creature
 * cards on private palette pages; do not replace the global card factory. */
typedef BYTE* (__cdecl* card_alloc_fn)(unsigned int, const char*, unsigned int);
typedef BYTE* (FANG_THISCALL* card_construct_fn)(BYTE*);
typedef void (FANG_THISCALL* card_ref_fn)(BYTE*);
typedef void (FANG_THISCALL* card_init_fn)(BYTE*, BYTE*, BYTE*, const float*, BYTE*);
typedef float* (FANG_THISCALL* card_area_fn)(BYTE*, float*, unsigned int);
typedef void (FANG_THISCALL* card_append_fn)(BYTE*, BYTE**);
typedef void (__stdcall* card_release_range_fn)(BYTE**, BYTE**);
typedef int (FANG_THISCALL* card_rebuild_fn)(BYTE*, BYTE*, BYTE*);
typedef int (FANG_THISCALL* card_build_fn)(BYTE*);
typedef void (FANG_THISCALL* card_window_flag_fn)(BYTE*, unsigned int, unsigned char);
typedef void (FANG_THISCALL* card_window_area_fn)(BYTE*, const float*);

typedef struct card_page {
    BYTE* page;
    BYTE flags[4];
} card_page;

static BYTE* card_base;
/* Preserve the preceding native vtable word and every callable slot. */
static void* card_page_table[13];
static void* card_category_table[8];
static card_build_fn card_native_build;
static unsigned int card_rebuilding;
static unsigned int card_deferred_count;
static struct {
    BYTE* panel;
    unsigned int category;
    unsigned int page;
    unsigned int created;
    int is_complete;
} card_warm;

static void card_restart_warm(void) {
    card_warm.category = 0;
    card_warm.page = 0;
    card_warm.is_complete = 0;
}

static int card_vector(BYTE* owner, unsigned int offset, unsigned int stride,
    unsigned int limit, BYTE** begin, unsigned int* count) {
    uintptr_t start;
    uintptr_t finish;
    uintptr_t size;
    if (!readable_range(owner, offset + 8)) return 0;
    start = *(uintptr_t*)(owner + offset);
    finish = *(uintptr_t*)(owner + offset + 4);
    if (finish < start) return 0;
    size = finish - start;
    if (size % stride || size / stride > limit ||
        (size && !readable_range((void*)start, size))) return 0;
    *begin = (BYTE*)start;
    *count = size / stride;
    return 1;
}

static int card_fill(BYTE* page, unsigned int limit, DWORD deadline) {
    BYTE* source;
    BYTE* entries;
    unsigned int count;
    unsigned int index;
    unsigned int added = 0;
    unsigned int cursor = 0;
    if (!readable_range(page, 64) || *(void***)page != card_page_table + 1) return -1;
    source = *(BYTE**)(page + 24);
    if (!card_vector(source, 116, 4, 4096, &entries, &count)) return -1;
    if (!count) return 1;
    if (fang_mod_palette_is_styled()) {
        unsigned int row_count = (count + FANG_PART_COLUMNS - 1) / FANG_PART_COLUMNS;
        if (row_count < FANG_PART_ROWS) row_count = FANG_PART_ROWS;
        /* Keep the compact four-column, ten-row creator palette. The native
         * rectangle calculation must reserve the same rows as pagination;
         * otherwise its last thumbnail row collides with the page controls. */
        /* 74FEA0 adds these borders to each cell's stride, not just the
         * outside margin. A nonzero top border pushes row ten past the page. */
        *(float*)(source + 44) = 0;
        *(float*)(source + 48) = 0;
        *(float*)(source + 52) = 0;
        *(float*)(source + 56) = 0;
        *(float*)(source + 60) = 0.15f;
        *(float*)(source + 64) = 1.f;
        source[68] = 1;
        source[69] = 0;
        source[70] = 0;
        *(float*)(source + 72) = 0.9f / row_count;
        *(unsigned int*)(source + 80) = row_count;
        *(unsigned int*)(source + 84) = FANG_PART_COLUMNS;
    }
    if (!readable_range(*(BYTE**)(page + 16), 4)) return -1;
    for (index = 0; index < count; ++index) {
        BYTE* entry = ((BYTE**)entries)[index];
        BYTE* cards;
        BYTE* card;
        unsigned int existing_count;
        unsigned int existing;
        float area[4] = {0};
        if (!entry) continue;
        if (!readable_range(entry, 104) ||
            !card_vector(page, 36, 4, 4096, &cards, &existing_count)) return -1;
        /* Cards normally form an ordered prefix of their source entries.
         * Consume that prefix once instead of searching it again for each
         * slot. Keep the search fallback for duplicate or reordered entries. */
        if (cursor < existing_count) {
            card = ((BYTE**)cards)[cursor];
            if (readable_range(card, 16) && *(BYTE**)(card + 12) == entry) {
                ++cursor;
                continue;
            }
        }
        for (existing = 0; existing < existing_count; ++existing) {
            card = ((BYTE**)cards)[existing];
            if (readable_range(card, 16) && *(BYTE**)(card + 12) == entry) break;
        }
        if (existing != existing_count) continue;
        /* Scanning existing cards must not consume the entire allowance on
         * every frame and permanently starve a later thumbnail. Always make
         * one unit of progress; the remaining creations respect the budget. */
        if (added >= limit || (added && (LONG)(GetTickCount() - deadline) >= 0)) return 0;
        /* Same rectangle, constructor, initializer and EASTL insertion as
         * native 750550's generic branch. Native handlers own input. */
        ((card_area_fn)(card_base + 0x34fea0))(page, area, index);
        if (!card_warm.created && area[2] > area[0] && area[3] > area[1] &&
            area[2] - area[0] < 4096 && area[3] - area[1] < 4096) {
            trace_client_state("mod_editor_parts_card_width", (unsigned int)(area[2] - area[0]));
            trace_client_state("mod_editor_parts_card_height", (unsigned int)(area[3] - area[1]));
        }
        card = ((card_alloc_fn)(card_base + 0x118550))(32, "SP_Editor/CreatureCard", 0);
        if (!card) {
            trace_client_state("mod_editor_parts_card_alloc_failed", index);
            return -1;
        }
        ((card_construct_fn)(card_base + 0x351f80))(card);
        ((card_ref_fn)(*(void***)card)[1])(card);
        ((card_init_fn)(card_base + 0x352450))(card, entry,
            *(BYTE**)(page + 16), area, *(BYTE**)(page + 20));
        fang_mod_palette_tip_card(card_base, card);
        fang_mod_palette_drag_card(card_base, card);
        ((card_append_fn)(card_base + 0x350300))(page + 36, &card);
        ((card_ref_fn)(*(void***)card)[2])(card);
        ++cursor;
        ++added;
        ++card_warm.created;
        if (card_warm.created % 64 == 0)
            trace_client_state("mod_editor_parts_warm_cards", card_warm.created);
    }
    return 1;
}

static int FANG_THISCALL card_build(BYTE* page) {
    BYTE* cards;
    unsigned int count;
    unsigned int index;
    if (*(void***)page != card_page_table + 1) {
        /* Native Init calls this before we can replace its equipment cards.
         * Defer only the private load/rebuild, never an equipment catalogue. */
        if (fang_mod_palette_is_loading() || card_rebuilding) {
            ++card_deferred_count;
            return 1;
        }
        return card_native_build(page);
    }
    /* Native dirty-page notifications may reposition existing windows, but
     * card/resource creation belongs exclusively to the frame budget below. */
    card_restart_warm();
    if (!card_vector(page, 36, 4, 4096, &cards, &count)) return 0;
    for (index = 0; index < count; ++index) {
        BYTE* card = ((BYTE**)cards)[index];
        BYTE* window;
        float area[4] = {0};
        if (!readable_range(card, 28)) return 0;
        window = *(BYTE**)(card + 24);
        if (!readable_range(window, 4) || !readable_range(*(void**)window, 112)) return 0;
        ((card_area_fn)(card_base + 0x34fea0))(page, area, index);
        ((card_window_area_fn)(*(void***)window)[27])(window, area);
    }
    return 1;
}

int fang_mod_install_palette_cards(BYTE* base) {
    if (!readable_range(base + 0xbda34c, sizeof(card_page_table)) ||
        !readable_range(base + 0xbfb720, sizeof(card_category_table)) ||
        *(void**)(base + 0xbda378) != base + 0x350550 ||
        *(void**)(base + 0xbfb73c) != base + 0x34f640) return 0;
    card_base = base;
    card_native_build = (card_build_fn)(base + 0x350550);
    memcpy(card_page_table, base + 0xbda34c, sizeof(card_page_table));
    card_page_table[11] = card_build;
    memcpy(card_category_table, base + 0xbfb720, sizeof(card_category_table));
    return patch_pointer((void**)(base + 0xbda378), card_native_build, card_build);
}

static int card_prepare_page(BYTE* page) {
    BYTE* cards;
    unsigned int count;
    unsigned int index;
    if (!readable_range(page, 64)) return 0;
    if (*(void***)page == card_page_table + 1) return 1;
    if (*(void**)page != card_base + 0xbda350 ||
        !card_vector(page, 52, 4, 4096, &cards, &count)) return 0;
    for (index = 0; index < count; ++index) {
        BYTE* card = ((BYTE**)cards)[index];
        if (!readable_range(card, 4) || !readable_range(*(void**)card, 12)) return 0;
    }
    /* Initial custom cards exist, but their first update has not run yet.
     * Native page teardown unregisters these controls before releasing their
     * interfaces. Keep the page layout and its owning references intact. */
    for (index = 0; index < count; ++index) {
        BYTE* card = ((BYTE**)cards)[index];
        ((card_ref_fn)(*(void***)card)[2])(card);
    }
    if (count) ((card_release_range_fn)(card_base + 0xc9b10))(
        (BYTE**)cards, (BYTE**)cards + count);
    *(BYTE**)(page + 56) = *(BYTE**)(page + 52);
    *(void***)page = card_page_table + 1;
    return 1;
}

static int card_prepare_category(BYTE* category) {
    BYTE* pages;
    unsigned int count;
    unsigned int index;
    if (fang_mod_palette_is_styled()) {
        BYTE* source;
        int result;
        if (!readable_range(category, 228)) return 0;
        source = *(BYTE**)(category + 108);
        result = fang_mod_palette_pages(card_base, source);
        if (result < 0) return 0;
        if (result > 0) {
            ++card_rebuilding;
            ((card_rebuild_fn)(card_base + 0x34f640))(
                category, source, *(BYTE**)(category + 224));
            --card_rebuilding;
        }
    }
    if (!card_vector(category, 132, sizeof(card_page), 512, &pages, &count)) return 0;
    for (index = 0; index < count; ++index) {
        if (!card_prepare_page(((card_page*)pages)[index].page)) return 0;
    }
    return 1;
}

static int FANG_THISCALL card_rebuild(BYTE* category, BYTE* source, BYTE* context) {
    int result;
    ++card_rebuilding;
    result = ((card_rebuild_fn)(card_base + 0x34f640))(category, source, context);
    --card_rebuilding;
    card_restart_warm();
    if (!card_prepare_category(category)) {
        BYTE* window = *(BYTE**)(category + 24);
        /* Prevent 74D770 from updating an incompatible rebuilt page. */
        if (readable_range(window, 4) && readable_range(*(void**)window, 128))
            ((card_window_flag_fn)(*(void***)window)[31])(window, 1, 0);
        trace_client_state("mod_editor_parts_rebuild_failed", 1);
    }
    return result;
}

int fang_mod_palette_cards(BYTE* base, BYTE* panel) {
    BYTE* categories;
    BYTE* context;
    unsigned int count;
    unsigned int index;
    if (!readable_range(base + 0xbda34c, sizeof(card_page_table)) ||
        !readable_range(base + 0xbfb720, sizeof(card_category_table)) ||
        *(void**)(base + 0xbda378) != (void*)card_build ||
        *(void**)(base + 0xbfb73c) != base + 0x34f640 ||
        !readable_range(panel, 116) ||
        !card_vector(panel, 52, 4, 256, &categories, &count) || !count) return 0;
    memset(&card_warm, 0, sizeof(card_warm));
    card_warm.panel = panel;
    card_category_table[7] = card_rebuild;
    context = *(BYTE**)(panel + 48);
    if (!readable_range(context, 48)) return 0;
    /* Fresh private context: the generic null-account path supplies native
     * creature icons/drag handlers without querying hero equipment ownership. */
    if (*(BYTE**)(context + 16)) {
        BYTE* account = *(BYTE**)(context + 16);
        *(BYTE**)(context + 16) = NULL;
        ((card_ref_fn)(*(void***)account)[1])(account);
    }
    for (index = 0; index < count; ++index) {
        BYTE* category = ((BYTE**)categories)[index];
        if (!readable_range(category, 228) ||
            *(void**)category != base + 0xbfb724) return 0;
        *(void***)category = card_category_table + 1;
        if (!card_prepare_category(category)) return 0;
    }
    if (!fang_mod_palette_tip_categories(base, panel))
        trace_client_state("mod_editor_parts_category_tooltips_failed", 1);
    trace_client_state("mod_editor_parts_creature_cards", count);
    trace_client_state("mod_editor_parts_cards_deferred", card_deferred_count);
    return 1;
}

int fang_mod_palette_warm(BYTE* panel) {
    BYTE* categories;
    BYTE* active;
    BYTE* pages;
    BYTE* priority = NULL;
    unsigned int count;
    unsigned int page_count;
    unsigned int selected;
    DWORD started = GetTickCount();
    int result;
    if (panel != card_warm.panel ||
        !card_vector(panel, 52, 4, 256, &categories, &count)) return -1;
    active = *(BYTE**)(panel + 108);
    /* Prefer the native selected category/page even while the root is hidden
     * for preloading. Visibility flags on hidden ancestors cannot identify it. */
    for (selected = 0; selected < count; ++selected)
        if (((BYTE**)categories)[selected] == active) break;
    if (selected < count) {
        if (!readable_range(active, 156) ||
            !card_vector(active, 132, sizeof(card_page), 512, &pages, &page_count)) return -1;
        selected = *(unsigned int*)(active + 152);
        if (selected < page_count) priority = ((card_page*)pages)[selected].page;
    }
    if (priority) {
        result = card_fill(priority, 4, started + 4);
        if (result <= 0) return result;
    }
    if (card_warm.is_complete) return 1;
    /* One off-screen card per frame. No retained page/entry pointers survive
     * a frame: native rebuilds and editor teardown remain their sole owners. */
    while (card_warm.category < count) {
        BYTE* category = ((BYTE**)categories)[card_warm.category];
        if (!card_vector(category, 132, sizeof(card_page), 512, &pages, &page_count)) return -1;
        while (card_warm.page < page_count) {
            BYTE* page = ((card_page*)pages)[card_warm.page].page;
            unsigned int before = card_warm.created;
            result = page == priority ? 1 : card_fill(page, 1, started + 4);
            if (result < 0) return -1;
            if (result) ++card_warm.page;
            if (!result || card_warm.created != before ||
                (LONG)(GetTickCount() - started) >= 4) return 0;
        }
        ++card_warm.category;
        card_warm.page = 0;
    }
    card_warm.is_complete = 1;
    trace_client_state("mod_editor_parts_warm_complete", card_warm.created);
    return 1;
}

void fang_mod_palette_cards_leave(BYTE* panel) {
    if (panel == card_warm.panel) memset(&card_warm, 0, sizeof(card_warm));
}

int fang_mod_palette_contains(BYTE* panel, BYTE* entry) {
    BYTE* categories;
    unsigned int count;
    unsigned int index;
    if (!entry || !card_vector(panel, 52, 4, 256, &categories, &count)) return 0;
    for (index = 0; index < count; ++index) {
        BYTE* pages;
        unsigned int page_count;
        unsigned int page_index;
        if (!card_vector(((BYTE**)categories)[index], 132, sizeof(card_page), 512,
            &pages, &page_count)) return 0;
        for (page_index = 0; page_index < page_count; ++page_index) {
            BYTE* page = ((card_page*)pages)[page_index].page;
            BYTE* entries;
            unsigned int entry_count;
            unsigned int entry_index;
            if (!readable_range(page, 28) || !card_vector(*(BYTE**)(page + 24),
                116, 4, 4096, &entries, &entry_count)) return 0;
            for (entry_index = 0; entry_index < entry_count; ++entry_index)
                if (((BYTE**)entries)[entry_index] == entry) return 1;
        }
    }
    return 0;
}

#include "mod/mod.h"
#include "hook.h"

#include <stdint.h>
#include <string.h>

/* Layout policy only. All windows, drawables and artwork are instantiated by
 * the client from its installed UI.package. Never serialize or embed them. */
typedef BYTE* (FANG_THISCALL* view_parent_fn)(BYTE*);
typedef const float* (FANG_THISCALL* view_area_fn)(BYTE*);
typedef void (FANG_THISCALL* view_set_area_fn)(BYTE*, const float*);
typedef BYTE* (FANG_THISCALL* view_find_fn)(BYTE*, uint32_t, int);

static int view_window(BYTE* window) {
    return readable_range(window, 4) && readable_range(*(void**)window, 128);
}

static BYTE* view_parent(BYTE* window) {
    if (!view_window(window)) return NULL;
    return ((view_parent_fn)(*(void***)window)[4])(window);
}

static void view_area(BYTE* window, float left, float top, float right, float bottom) {
    const float bounds[4] = {left, top, right, bottom};
    const float* current;
    if (!view_window(window)) return;
    current = ((view_area_fn)(*(void***)window)[14])(window);
    if (!readable_range(current, sizeof(bounds)) || memcmp(current, bounds, sizeof(bounds)) == 0) return;
    /* Apply bounds through the native anchored-window setter; page and
     * frame resources retain their authored transforms and resize handlers. */
    ((view_set_area_fn)(*(void***)window)[27])(window, bounds);
}

static void view_arrow(BYTE* base, BYTE* layout, uint32_t id, int is_right) {
    BYTE* arrow = ((view_find_fn)(base + 0x50c0e0))(layout, id, 1);
    BYTE* cap = view_parent(arrow);
    if (view_window(cap) && ((BYTE* (FANG_THISCALL*)(BYTE*))(*(void***)cap)[42])(cap))
        ((void (FANG_THISCALL*)(BYTE*, BYTE*))(*(void***)cap)[44])(cap, NULL);
    view_area(cap, 0, 0, 18, 38);
    if (is_right) view_area(view_parent(cap), 246, 0, 264, 38);
    view_area(arrow, 2, 11, 16, 27);
}

static void view_pages(BYTE* category) {
    uintptr_t begin;
    uintptr_t end;
    unsigned int index;
    unsigned int count;
    if (!readable_range(category, 140)) return;
    begin = *(uintptr_t*)(category + 132);
    end = *(uintptr_t*)(category + 136);
    if (end < begin || (end - begin) % 8 || end - begin > 512 * 8 ||
        (end != begin && !readable_range((void*)begin, end - begin))) return;
    count = (end - begin) / 8;
    for (index = 0; index < count; ++index) {
        BYTE* page = *(BYTE**)(begin + index * 8);
        if (!readable_range(page, 20)) continue;
        view_area(*(BYTE**)(page + 16), 0, 0, 296, 315);
    }
}

void fang_mod_palette_view(BYTE* base, BYTE* panel) {
    BYTE* tabs;
    BYTE* tab_container;
    BYTE* header;
    BYTE* layout;
    BYTE** categories;
    uintptr_t begin;
    uintptr_t end;
    unsigned int index;
    unsigned int count;
    if (!readable_range(panel, 116)) return;
    tabs = *(BYTE**)(panel + 16);
    tab_container = view_parent(tabs);
    header = view_parent(tab_container);
    view_area(header, 8, 108, 272, 169);
    view_area(tab_container, 18, 0, 246, 38);
    view_area(tabs, 0, 3, 228, 35);
    fang_mod_palette_tabs_update(panel);
    fang_mod_palette_title(base, panel);
    view_area(*(BYTE**)(panel + 36), 8, 146, 328, 540);
    layout = *(BYTE**)(panel + 12);
    if (readable_range(layout, 4)) {
        view_arrow(base, layout, 0x05aec4b8, 0);
        view_arrow(base, layout, 0x05aec4b9, 1);
    }
    begin = *(uintptr_t*)(panel + 52);
    end = *(uintptr_t*)(panel + 56);
    if (end < begin || (end - begin) % 4 || end - begin > 256 * 4 ||
        (end != begin && !readable_range((void*)begin, end - begin))) return;
    categories = (BYTE**)begin;
    count = (end - begin) / 4;
    for (index = 0; index < count; ++index) {
        BYTE* category = categories[index];
        BYTE* root;
        if (!readable_range(category, 44)) continue;
        layout = *(BYTE**)(category + 16);
        if (!readable_range(layout, 4)) continue;
        root = ((view_find_fn)(base + 0x50c0e0))(layout, UINT32_MAX, 1);
        view_area(root, 0, 0, 320, 394);
        /* Match the native frame's rendered width. Its centered 80% page
         * transform and fixed-size artwork must share one visible origin;
         * widening just the viewport puts column four outside that frame. */
        view_area(*(BYTE**)(category + 24), -16, 12, 280, 327);
        view_area(((view_find_fn)(base + 0x50c0e0))(layout, 0x0af12396, 1),
            -15, -50, 314, 414);
        view_pages(category);
        /* Page windows are parented below the viewport, while the paging
         * strip belongs to its decorative frame, whose authored origin is
         * above the viewport. Place the strip after the entire grid in that
         * parent's coordinates, rather than over its last two rows. */
        view_area(*(BYTE**)(category + 28), 0, 364, 329, 392);
        view_area(((view_find_fn)(base + 0x50c0e0))(layout, 0x734afa63, 1),
            146, 5, 183, 23);
    }
}

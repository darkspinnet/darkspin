#include "hook.h"

#include <stdint.h>
#include <string.h>

typedef BYTE* (FANG_THISCALL* parts_tab_get_fn)(BYTE*);
typedef void (FANG_THISCALL* parts_tab_ref_fn)(BYTE*);
typedef unsigned char (FANG_THISCALL* parts_tab_child_fn)(BYTE*, BYTE*);
typedef void (FANG_THISCALL* parts_tab_draw_fn)(BYTE*, BYTE*);

static BYTE* tab_owner;
static BYTE* tab_frames[256];
static unsigned int tab_count;

void fang_mod_palette_tabs_leave(void) {
    /* The native window tree owns these references and destroys the frames
     * with the palette. Do not retain windows beyond that tree's lifetime. */
    fang_mod_palette_title_leave();
    tab_owner = NULL;
    tab_count = 0;
    memset(tab_frames, 0, sizeof(tab_frames));
}

static int parts_tab_window(BYTE* window) {
    return readable_range(window, 4) && readable_range(*(void**)window, 264);
}

static BYTE* parts_tab_window_from_button(BYTE* button) {
    if (!readable_range(button, 4) || !readable_range(*(void**)button, 20)) return NULL;
    return ((parts_tab_get_fn)(*(void***)button)[4])(button);
}

static BYTE* parts_tab_wrap(BYTE* base, BYTE* window, BYTE* donor) {
    BYTE* parent = ((parts_tab_get_fn)(*(void***)window)[4])(window);
    BYTE* allocator;
    BYTE* allocation;
    BYTE* instance;
    BYTE* frame;
    if (!parts_tab_window(parent)) return NULL;
    allocator = ((BYTE* (__cdecl*)(void))(base + 0x7de7d0))();
    allocation = ((BYTE* (__cdecl*)(unsigned int, unsigned int, const char*, BYTE*))
        (base + 0x7de7e0))(2148, 4, "Darkspin/PartsTab", allocator);
    if (!allocation) return NULL;
    instance = ((BYTE* (FANG_THISCALL*)(BYTE*))(base + 0x7ea860))(allocation);
    if (!instance) return NULL;
    frame = instance + 4;
    ((parts_tab_ref_fn)(*(void***)frame)[0])(frame);
    if (!fang_mod_editor_button_skin(base, frame, donor)) {
        ((parts_tab_ref_fn)(*(void***)frame)[1])(frame);
        return NULL;
    }
    {
        BYTE* drawable = ((parts_tab_get_fn)(*(void***)frame)[42])(frame);
        if (readable_range(drawable, 76)) {
            /* A mode-tab skin is 79 pixels wide. Its nine-slice corners
             * overlap inside a 25-pixel category button; scale this private
             * background as a whole instead of retaining those wide corners. */
            *(unsigned int*)(drawable + 48) = 1;
        }
    }
    {
        BYTE* button = ((BYTE* (FANG_THISCALL*)(BYTE*, uint32_t))
            (*(void***)frame)[3])(frame, 0x8ed27e7au);
        if (!readable_range(button, 4) || !readable_range(*(void**)button, 44)) {
            ((parts_tab_ref_fn)(*(void***)frame)[1])(frame);
            return NULL;
        }
        ((void (FANG_THISCALL*)(BYTE*, unsigned int))(*(void***)button)[7])(button, 3);
    }
    if (!((parts_tab_child_fn)(*(void***)parent)[54])(parent, frame)) {
        if (((parts_tab_get_fn)(*(void***)frame)[4])(frame) == parent)
            ((parts_tab_child_fn)(*(void***)parent)[55])(parent, frame);
        ((parts_tab_ref_fn)(*(void***)frame)[1])(frame);
        return NULL;
    }
    /* Preserve the original button, command, listeners and tooltip. Only its
     * parent changes; an explicit reference spans RemoveWindow/AddWindow. */
    ((parts_tab_ref_fn)(*(void***)window)[0])(window);
    ((parts_tab_child_fn)(*(void***)parent)[55])(parent, window);
    if (!((parts_tab_child_fn)(*(void***)frame)[54])(frame, window)) {
        if (((parts_tab_get_fn)(*(void***)window)[4])(window) == frame)
            ((parts_tab_child_fn)(*(void***)frame)[55])(frame, window);
        if (!((parts_tab_child_fn)(*(void***)parent)[54])(parent, window))
            trace_client_state("mod_editor_parts_tab_restore_failed", 1);
        ((parts_tab_ref_fn)(*(void***)window)[1])(window);
        ((parts_tab_child_fn)(*(void***)parent)[55])(parent, frame);
        ((parts_tab_ref_fn)(*(void***)frame)[1])(frame);
        return NULL;
    }
    ((parts_tab_ref_fn)(*(void***)window)[1])(window);
    ((parts_tab_ref_fn)(*(void***)frame)[1])(frame);
    return frame;
}

int fang_mod_palette_tabs(BYTE* base, BYTE* panel, BYTE* equipment) {
    BYTE** buttons;
    BYTE** categories;
    BYTE* donor;
    BYTE* drawable;
    uintptr_t begin;
    uintptr_t end;
    unsigned int count;
    unsigned int index;
    if (!readable_range(panel, 116) || !readable_range(equipment, 76)) return 0;
    begin = *(uintptr_t*)(panel + 68);
    end = *(uintptr_t*)(panel + 72);
    if (end <= begin || (end - begin) % 4 || end - begin > sizeof(tab_frames) ||
        !readable_range((void*)begin, end - begin)) return 0;
    count = (end - begin) / 4;
    buttons = (BYTE**)begin;
    categories = *(BYTE***)(panel + 52);
    if (*(uintptr_t*)(panel + 56) - (uintptr_t)categories != count * 4 ||
        !readable_range(categories, count * 4)) return 0;
    begin = *(uintptr_t*)(equipment + 68);
    if (*(uintptr_t*)(equipment + 72) <= begin || !readable_range((void*)begin, 4)) return 0;
    donor = parts_tab_window_from_button(*(BYTE**)begin);
    if (!parts_tab_window(donor)) return 0;
    {
        BYTE* root = donor;
        BYTE* parent;
        BYTE* mode;
        unsigned int depth;
        for (depth = 0; depth < 24; ++depth) {
            parent = ((parts_tab_get_fn)(*(void***)root)[4])(root);
            if (!parent) break;
            root = parent;
        }
        mode = ((BYTE* (FANG_THISCALL*)(BYTE*, uint32_t, int))
            (*(void***)root)[60])(root, 0xf019c2e7u, 1);
        if (parts_tab_window(mode)) donor = mode;
    }
    drawable = ((parts_tab_get_fn)(*(void***)donor)[42])(donor);
    if (!drawable) return 0;
    fang_mod_palette_tabs_leave();
    tab_owner = panel;
    tab_count = count;
    for (index = 0; index < count; ++index) {
        BYTE* window = parts_tab_window_from_button(buttons[index]);
        BYTE* category = categories[index];
        BYTE* source;
        uint32_t* icons;
        unsigned int state;
        if (!parts_tab_window(window) || !readable_range(category, 112)) break;
        source = *(BYTE**)(category + 108);
        if (!readable_range(source, 144)) break;
        icons = *(uint32_t**)(source + 140);
        if (!readable_range(icons, 96)) break;
        /* Preserve the installed normal, hover, pressed and checked glyphs. */
        for (state = 0; state < 8; ++state) {
            if (!((unsigned char (__cdecl*)(BYTE*, uint32_t*, unsigned int))
                (base + 0x50a360))(window, icons + state * 3, state)) break;
        }
        if (state != 8) break;
        tab_frames[index] = parts_tab_wrap(base, window, donor);
        if (!tab_frames[index]) break;
    }
    trace_client_state("mod_editor_parts_native_tabs", index);
    trace_client_state("mod_editor_parts_tab_header_version", 2);
    if (index != count) trace_client_state("mod_editor_parts_native_tabs_incomplete", count);
    return index == count;
}

static void parts_tab_area(BYTE* window, const float* area) {
    const float* current = ((const float* (FANG_THISCALL*)(BYTE*))(*(void***)window)[14])(window);
    if (!readable_range(current, 16) || memcmp(current, area, 16) == 0) return;
    ((void (FANG_THISCALL*)(BYTE*, const float*))(*(void***)window)[27])(window, area);
}

void fang_mod_palette_tabs_update(BYTE* panel) {
    BYTE** buttons;
    BYTE** categories;
    unsigned int index;
    if (tab_owner != panel || !readable_range(panel, 116)) return;
    buttons = *(BYTE***)(panel + 68);
    categories = *(BYTE***)(panel + 52);
    for (index = 0; index < tab_count; ++index) {
        BYTE* frame = tab_frames[index];
        BYTE* window;
        BYTE* button;
        unsigned int state;
        float left = (index % 9) * (228.0f / 9.0f);
        float area[4] = {left, 0, left + 228.0f / 9.0f - 1, 32};
        const float glyph[4] = {(228.0f / 9.0f - 20) * .5f, 6,
            (228.0f / 9.0f + 20) * .5f, 26};
        if (!parts_tab_window(frame)) continue;
        window = parts_tab_window_from_button(buttons[index]);
        if (!parts_tab_window(window)) continue;
        parts_tab_area(frame, area);
        parts_tab_area(window, glyph);
        {
            /* 7413E0 explicitly shows this child on selection. It is the
             * legacy selected-tab flourish, not the button's drawable; merely
             * clearing GetDrawable leaves its tall white flap on screen. */
            BYTE* decoration = ((BYTE* (FANG_THISCALL*)(BYTE*, uint32_t, int))
                (*(void***)window)[60])(window, 0x0af4c2c6u, 1);
            if (parts_tab_window(decoration))
                ((void (FANG_THISCALL*)(BYTE*, unsigned int, unsigned char))
                    (*(void***)decoration)[31])(decoration, 1, 0);
        }
        ((void (FANG_THISCALL*)(BYTE*, unsigned int, unsigned char))
            (*(void***)frame)[31])(frame, 1, index / 9 == *(unsigned int*)(panel + 104));
        ((void (FANG_THISCALL*)(BYTE*, unsigned int, unsigned char))
            (*(void***)frame)[31])(frame, 2, 1);
        state = ((unsigned int (FANG_THISCALL*)(BYTE*))(*(void***)window)[11])(window);
        ((void (FANG_THISCALL*)(BYTE*, unsigned int))(*(void***)frame)[22])(frame, state);
        button = ((BYTE* (FANG_THISCALL*)(BYTE*, uint32_t))
            (*(void***)frame)[3])(frame, 0x8ed27e7a);
        if (readable_range(button, 4) && readable_range(*(void**)button, 44)) {
            ((void (FANG_THISCALL*)(BYTE*, unsigned int, int))
                (*(void***)button)[10])(button, 4, categories[index] == *(BYTE**)(panel + 108));
            ((parts_tab_ref_fn)(*(void***)frame)[37])(frame);
        }
    }
    /* These are the legacy strip's left, right and selected-middle
     * drawables. Retain its controller and paging arrows, remove its art. */
    for (index = 20; index <= 32; index += 4) {
        BYTE* window = *(BYTE**)(panel + index);
        if (index == 32 && window) window += 4;
        if (!parts_tab_window(window)) continue;
        if (((parts_tab_get_fn)(*(void***)window)[42])(window))
            ((parts_tab_draw_fn)(*(void***)window)[44])(window, NULL);
        /* The selector at +32 paints through its own animated-control
         * renderer even with no IWindow drawable. Keep its controller alive,
         * but hide the entire old decoration on this private palette. */
        ((void (FANG_THISCALL*)(BYTE*, unsigned int, unsigned char))
            (*(void***)window)[31])(window, 1, 0);
    }
}

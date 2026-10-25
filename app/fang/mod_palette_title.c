#include "hook.h"

#include <stdint.h>
#include <wchar.h>

static BYTE* title_owner;
static BYTE* title_window;

typedef BYTE* (FANG_THISCALL* title_get_fn)(BYTE*);
typedef void (FANG_THISCALL* title_ref_fn)(BYTE*);

static BYTE* title_donor(BYTE* window, unsigned int depth) {
    const wchar_t* caption;
    BYTE* begin;
    BYTE* end;
    BYTE* link;
    unsigned int count = 0;
    if (depth > 24 || !readable_range(window, 4) ||
        !readable_range(*(void**)window, 264)) return NULL;
    caption = ((const wchar_t* (FANG_THISCALL*)(BYTE*))(*(void***)window)[15])(window);
    /* This installed paint label supplies the native editor heading style. */
    if (caption && readable_range(caption, sizeof(wchar_t)) && caption[0] &&
        ((unsigned int (FANG_THISCALL*)(BYTE*))(*(void***)window)[16])(window) == 148555246u) {
        BYTE* parent = ((title_get_fn)(*(void***)window)[4])(window);
        if (parent && ((unsigned int (FANG_THISCALL*)(BYTE*))(*(void***)parent)[7])(parent) == 0x0af4c2c6u)
            return window;
    }
    ((void (FANG_THISCALL*)(BYTE*, BYTE**))(*(void***)window)[51])(window, &begin);
    ((void (FANG_THISCALL*)(BYTE*, BYTE**))(*(void***)window)[52])(window, &end);
    for (link = begin; link != end && count < 4096; ++count) {
        BYTE* found;
        if (!readable_range(link, 4)) break;
        found = title_donor(link - 4, depth + 1);
        if (found) return found;
        link = *(BYTE**)link;
    }
    return NULL;
}

void fang_mod_palette_title_leave(void) {
    /* Native parent owns the control; clear only our borrowed pointer. */
    title_owner = NULL;
    title_window = NULL;
}

void fang_mod_palette_title(BYTE* base, BYTE* panel) {
    BYTE* tabs = *(BYTE**)(panel + 16);
    BYTE* container = ((title_get_fn)(*(void***)tabs)[4])(tabs);
    BYTE* header = ((title_get_fn)(*(void***)container)[4])(container);
    BYTE* category = *(BYTE**)(panel + 108);
    const wchar_t* caption;
    if (!readable_range(category, 112)) return;
    if (title_owner != panel) {
        BYTE* root = header;
        BYTE* parent;
        BYTE* donor;
        BYTE* allocator;
        BYTE* allocation;
        BYTE* instance;
        unsigned int depth;
        unsigned int font = 148555246u;
        for (depth = 0; depth < 24; ++depth) {
            parent = ((title_get_fn)(*(void***)root)[4])(root);
            if (!parent) break;
            root = parent;
        }
        donor = title_donor(root, 0);
        if (donor) font = ((unsigned int (FANG_THISCALL*)(BYTE*))(*(void***)donor)[16])(donor);
        allocator = ((BYTE* (__cdecl*)(void))(base + 0x7de7d0))();
        if (donor) {
            BYTE* decoration;
            BYTE* source = ((BYTE* (FANG_THISCALL*)(BYTE*, uint32_t, int))
                (*(void***)root)[60])(root, 0xf019c2e7u, 1);
            const float area[4] = {2, 34, 258, 60};
            allocation = ((BYTE* (__cdecl*)(unsigned int, unsigned int, const char*, BYTE*))
                (base + 0x7de7e0))(2060, 4, "Darkspin/PartsHeading", allocator);
            if (!allocation) return;
            instance = ((BYTE* (FANG_THISCALL*)(BYTE*))(base + 0x7e72e0))(allocation);
            if (!instance) return;
            decoration = instance + 4;
            ((title_ref_fn)(*(void***)decoration)[0])(decoration);
            /* Keep the heading frame separate from category selection. The
             * paint heading includes a wide selected-tab outline in its
             * image, which otherwise stays stuck behind the first icons. */
            if (readable_range(source, 4) && readable_range(*(void**)source, 180))
                fang_mod_editor_button_skin(base, decoration, source);
            ((void (FANG_THISCALL*)(BYTE*, const float*))(*(void***)decoration)[27])(decoration, area);
            ((void (FANG_THISCALL*)(BYTE*, unsigned int, unsigned char))(*(void***)decoration)[31])(decoration, 2, 0);
            if (!((unsigned char (FANG_THISCALL*)(BYTE*, BYTE*))(*(void***)header)[54])(header, decoration)) {
                if (((title_get_fn)(*(void***)decoration)[4])(decoration) == header)
                    ((unsigned char (FANG_THISCALL*)(BYTE*, BYTE*))(*(void***)header)[55])(header, decoration);
            }
            ((title_ref_fn)(*(void***)decoration)[1])(decoration);
        }
        /* The background must precede the existing native tab container in
         * both drawing and hit-test order. Disabled decorations still occlude
         * lower siblings in the native window hit search. */
        ((title_ref_fn)(*(void***)container)[0])(container);
        ((unsigned char (FANG_THISCALL*)(BYTE*, BYTE*))(*(void***)header)[55])(header, container);
        if (!((unsigned char (FANG_THISCALL*)(BYTE*, BYTE*))(*(void***)header)[54])(header, container) &&
            ((title_get_fn)(*(void***)container)[4])(container) != header) {
            /* Keep the native controller alive if attachment failed. */
            trace_client_state("mod_editor_parts_header_attach_failed", 1);
            return;
        }
        ((title_ref_fn)(*(void***)container)[1])(container);
        allocation = ((BYTE* (__cdecl*)(unsigned int, unsigned int, const char*, BYTE*))
            (base + 0x7de7e0))(2060, 4, "Darkspin/PartsTitle", allocator);
        if (!allocation) return;
        instance = ((BYTE* (FANG_THISCALL*)(BYTE*))(base + 0x7e72e0))(allocation);
        if (!instance) return;
        title_window = instance + 4;
        ((title_ref_fn)(*(void***)title_window)[0])(title_window);
        ((void (FANG_THISCALL*)(BYTE*, unsigned int))(*(void***)title_window)[33])(title_window, font);
        {
            const float area[4] = {8, 38, 256, 57};
            ((void (FANG_THISCALL*)(BYTE*, const float*))(*(void***)title_window)[27])(title_window, area);
        }
        if (!((unsigned char (FANG_THISCALL*)(BYTE*, BYTE*))(*(void***)header)[54])(header, title_window)) {
            if (((title_get_fn)(*(void***)title_window)[4])(title_window) == header)
                ((unsigned char (FANG_THISCALL*)(BYTE*, BYTE*))(*(void***)header)[55])(header, title_window);
            ((title_ref_fn)(*(void***)title_window)[1])(title_window);
            title_window = NULL;
            return;
        }
        ((title_ref_fn)(*(void***)title_window)[1])(title_window);
        title_owner = panel;
    }
    caption = fang_mod_palette_category_name(*(BYTE**)(category + 108));
    ((void (FANG_THISCALL*)(BYTE*, const wchar_t*))(*(void***)title_window)[32])(title_window, caption);
    ((void (FANG_THISCALL*)(BYTE*, unsigned int, unsigned char))(*(void***)title_window)[31])(title_window, 1, 1);
    ((void (FANG_THISCALL*)(BYTE*, unsigned int, unsigned char))(*(void***)title_window)[31])(title_window, 2, 0);
}

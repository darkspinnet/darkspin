#include "mod/mod.h"
#include "hook.h"

#include <stdint.h>
#include <string.h>

/* Private catalogue pages only. Native category paging consumes this same
 * ref-counted vector; splitting it keeps every entry and its drag identity. */
typedef BYTE* (__cdecl* page_alloc_fn)(unsigned int, const char*, unsigned int);
typedef BYTE* (FANG_THISCALL* page_construct_fn)(BYTE*);
typedef void (FANG_THISCALL* page_ref_fn)(BYTE*);

static int page_entries(BYTE* page, BYTE*** entries, unsigned int* count) {
    uintptr_t begin;
    uintptr_t end;
    if (!readable_range(page, 128)) return 0;
    begin = *(uintptr_t*)(page + 116);
    end = *(uintptr_t*)(page + 120);
    if (end < begin || (end - begin) % 4 || end - begin > 4096 * 4 ||
        (end != begin && !readable_range((void*)begin, end - begin))) return 0;
    *entries = (BYTE**)begin;
    *count = (end - begin) / 4;
    return 1;
}

static unsigned int page_item_count(BYTE** entries, unsigned int count) {
    unsigned int index;
    unsigned int item_count = 0;
    for (index = 0; index < count; ++index)
        if (entries[index]) ++item_count;
    return item_count;
}

int fang_mod_palette_pages(BYTE* base, BYTE* category) {
    BYTE** pages;
    BYTE** replacements;
    BYTE** entries;
    unsigned int count;
    unsigned int entry_count;
    unsigned int item_count = 0;
    unsigned int replacement_count = 0;
    unsigned int created_count = 0;
    unsigned int copied_count = 0;
    unsigned int filled_count = 0;
    BYTE* page = NULL;
    BYTE** copies = NULL;
    int is_replacement_needed = 0;
    unsigned int index;
    uintptr_t begin;
    uintptr_t end;
    if (!readable_range(category, 24)) return -1;
    begin = *(uintptr_t*)(category + 12);
    end = *(uintptr_t*)(category + 16);
    if (end < begin || (end - begin) % 4 || end - begin > 512 * 4 ||
        (end != begin && !readable_range((void*)begin, end - begin))) return -1;
    count = (end - begin) / 4;
    pages = (BYTE**)begin;
    for (index = 0; index < count; ++index) {
        unsigned int page_count;
        if (!page_entries(pages[index], &entries, &entry_count)) return -1;
        /* Authored grids include null padding slots. Count actual parts so
         * padding cannot become an entire selectable blank page. */
        page_count = page_item_count(entries, entry_count);
        if (!page_count || page_count != entry_count || page_count > FANG_PART_PAGE_SIZE ||
            (index + 1 < count && page_count != FANG_PART_PAGE_SIZE))
            is_replacement_needed = 1;
        item_count += page_count;
        if (item_count > 4096 * FANG_PART_PAGE_SIZE) return -1;
    }
    if (!is_replacement_needed || !item_count) return 0;
    replacement_count = (item_count + FANG_PART_PAGE_SIZE - 1) / FANG_PART_PAGE_SIZE;
    replacements = (BYTE**)((page_alloc_fn)(base + 0x118550))(
        replacement_count * 4, "Darkspin/PartsPages", 0);
    if (!replacements) return -1;
    for (index = 0; index < count; ++index) {
        unsigned int offset;
        if (!page_entries(pages[index], &entries, &entry_count)) goto failed;
        /* Carry a partially filled replacement across authored page breaks.
         * Those breaks are not category boundaries; preserve entry order. */
        for (offset = 0; offset < entry_count; ++offset) {
            if (!entries[offset]) continue;
            if (!page) {
                unsigned int size = item_count - copied_count;
                if (size > FANG_PART_PAGE_SIZE) size = FANG_PART_PAGE_SIZE;
                page = ((page_alloc_fn)(base + 0x118550))(128, "Darkspin/PartsPage", 0);
                if (!page) goto failed;
                ((page_construct_fn)(base + 0x34c7e0))(page);
                ((page_ref_fn)(*(void***)page)[1])(page);
                replacements[created_count++] = page;
                /* Scalar page configuration ends at the entry vector. The
                 * constructor's vtables and vector ownership stay intact. */
                memcpy(page + 12, pages[index] + 12, 104);
                *(unsigned int*)(page + 92) = created_count;
                copies = (BYTE**)((page_alloc_fn)(base + 0x118550))(
                    size * 4, "Darkspin/PartsEntries", 0);
                if (!copies) goto failed;
                *(BYTE***)(page + 116) = copies;
                *(BYTE***)(page + 120) = copies;
                *(BYTE***)(page + 124) = copies + size;
                filled_count = 0;
            }
            copies[filled_count] = entries[offset];
            ((page_ref_fn)(*(void***)entries[offset])[1])(entries[offset]);
            ++filled_count;
            ++copied_count;
            *(BYTE***)(page + 120) = copies + filled_count;
            if (filled_count == FANG_PART_PAGE_SIZE) page = NULL;
        }
    }
    /* 74C7A0 releases each IRefCount and its native EASTL allocation. Existing
     * render pages retain their own references until the immediate rebuild. */
    ((page_ref_fn)(base + 0x34c7a0))(category + 12);
    *(BYTE***)(category + 12) = replacements;
    *(BYTE***)(category + 16) = replacements + replacement_count;
    *(BYTE***)(category + 20) = replacements + replacement_count;
    trace_client_state("mod_editor_parts_page_count", replacement_count);
    return 1;

failed:
    for (index = 0; index < created_count; ++index)
        ((page_ref_fn)(*(void***)replacements[index])[2])(replacements[index]);
    ((void (__cdecl*)(void*))(base + 0x118530))(replacements);
    return -1;
}

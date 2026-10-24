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

int fang_mod_palette_pages(BYTE* base, BYTE* category) {
    BYTE** pages;
    BYTE** replacements;
    BYTE** entries;
    unsigned int count;
    unsigned int entry_count;
    unsigned int replacement_count = 0;
    unsigned int created_count = 0;
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
        if (!page_entries(pages[index], &entries, &entry_count)) return -1;
        replacement_count += entry_count ?
            (entry_count + FANG_PART_PAGE_SIZE - 1) / FANG_PART_PAGE_SIZE : 1;
    }
    if (replacement_count == count) return 0;
    if (replacement_count > 4096) return -1;
    replacements = (BYTE**)((page_alloc_fn)(base + 0x118550))(
        replacement_count * 4, "Darkspin/PartsPages", 0);
    if (!replacements) return -1;
    for (index = 0; index < count; ++index) {
        unsigned int offset = 0;
        if (!page_entries(pages[index], &entries, &entry_count)) goto failed;
        do {
            unsigned int size = entry_count - offset;
            unsigned int item;
            BYTE** copies = NULL;
            BYTE* page;
            if (size > FANG_PART_PAGE_SIZE) size = FANG_PART_PAGE_SIZE;
            page = ((page_alloc_fn)(base + 0x118550))(128, "Darkspin/PartsPage", 0);
            if (!page) goto failed;
            ((page_construct_fn)(base + 0x34c7e0))(page);
            ((page_ref_fn)(*(void***)page)[1])(page);
            replacements[created_count++] = page;
            /* Scalar page configuration ends at the entry vector. The
             * constructor's vtables, count and vector ownership stay intact. */
            memcpy(page + 12, pages[index] + 12, 104);
            *(unsigned int*)(page + 92) = created_count;
            if (size) {
                copies = (BYTE**)((page_alloc_fn)(base + 0x118550))(
                    size * 4, "Darkspin/PartsEntries", 0);
                if (!copies) goto failed;
                for (item = 0; item < size; ++item) {
                    copies[item] = entries[offset + item];
                    if (copies[item]) ((page_ref_fn)(*(void***)copies[item])[1])(copies[item]);
                }
            }
            *(BYTE***)(page + 116) = copies;
            *(BYTE***)(page + 120) = copies ? copies + size : NULL;
            *(BYTE***)(page + 124) = copies ? copies + size : NULL;
            offset += size;
        } while (offset < entry_count);
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

#include "hook/patch.h"

#include <string.h>

// Reads: checks whether a pointer lies in committed memory without guard or no-access protection.
int fang_readable_pointer(const void* pointer) {
    MEMORY_BASIC_INFORMATION information;
    if (pointer == NULL || VirtualQuery(pointer, &information, sizeof(information)) == 0) {
        return 0;
    }
    if (information.State != MEM_COMMIT) {
        return 0;
    }
    if ((information.Protect & (PAGE_GUARD | PAGE_NOACCESS)) != 0) {
        return 0;
    }
    return 1;
}

// Reads: checks whether a span fits within one committed, accessible memory region.
int readable_range(const void* pointer, size_t size) {
    MEMORY_BASIC_INFORMATION information;
    uintptr_t start = (uintptr_t)pointer;
    uintptr_t end;
    uintptr_t region_end;
    if (pointer == NULL || size == 0 || start > (uintptr_t)-1 - size) {
        return 0;
    }
    if (VirtualQuery(pointer, &information, sizeof(information)) == 0 ||
        information.State != MEM_COMMIT ||
        (information.Protect & (PAGE_GUARD | PAGE_NOACCESS)) != 0) {
        return 0;
    }
    end = start + size;
    region_end = (uintptr_t)information.BaseAddress + information.RegionSize;
    return end <= region_end;
}

// Reads: searches readable PE sections for an exact byte signature.
BYTE* fang_find_signature(HMODULE module, const BYTE* signature, size_t signature_length) {
    BYTE* base = (BYTE*)module;
    IMAGE_DOS_HEADER* dos = (IMAGE_DOS_HEADER*)base;
    IMAGE_NT_HEADERS* nt;
    IMAGE_SECTION_HEADER* section;
    WORD section_index;
    if (base == NULL || dos->e_magic != IMAGE_DOS_SIGNATURE) {
        return NULL;
    }
    nt = (IMAGE_NT_HEADERS*)(base + dos->e_lfanew);
    if (nt->Signature != IMAGE_NT_SIGNATURE) {
        return NULL;
    }
    section = IMAGE_FIRST_SECTION(nt);
    for (section_index = 0; section_index < nt->FileHeader.NumberOfSections; section_index++, section++) {
        BYTE* start;
        size_t size;
        size_t index;
        if ((section->Characteristics & IMAGE_SCN_MEM_READ) == 0) {
            continue;
        }
        size = section->Misc.VirtualSize;
        if (size < section->SizeOfRawData) {
            size = section->SizeOfRawData;
        }
        if (size < signature_length || section->VirtualAddress >= nt->OptionalHeader.SizeOfImage) {
            continue;
        }
        if (size > nt->OptionalHeader.SizeOfImage - section->VirtualAddress) {
            size = nt->OptionalHeader.SizeOfImage - section->VirtualAddress;
        }
        start = base + section->VirtualAddress;
        for (index = 0; index <= size - signature_length; index++) {
            if (memcmp(start + index, signature, signature_length) == 0) {
                return start + index;
            }
        }
    }
    return NULL;
}


#include "hook/import.h"
#include "hook/patch.h"

#include <string.h>

int fang_patch_import(HMODULE module, const char* library_name, const char* function_name, WORD function_ordinal, void* replacement, unsigned int* diagnostics) {
    BYTE* base = (BYTE*)module;
    IMAGE_DOS_HEADER* dos = (IMAGE_DOS_HEADER*)base;
    IMAGE_NT_HEADERS* nt;
    IMAGE_IMPORT_DESCRIPTOR* descriptor;
    int patched = 0;
    if (diagnostics != NULL) {
        *diagnostics |= 0x1000;
    }
    if (dos == NULL || dos->e_magic != IMAGE_DOS_SIGNATURE) {
        return 0;
    }
    nt = (IMAGE_NT_HEADERS*)(base + dos->e_lfanew);
    if (nt->Signature != IMAGE_NT_SIGNATURE) {
        return 0;
    }
    if (nt->OptionalHeader.DataDirectory[IMAGE_DIRECTORY_ENTRY_IMPORT].VirtualAddress == 0) {
        return 0;
    }
    descriptor = (IMAGE_IMPORT_DESCRIPTOR*)(base + nt->OptionalHeader.DataDirectory[IMAGE_DIRECTORY_ENTRY_IMPORT].VirtualAddress);
    for (; descriptor->Name != 0; descriptor++) {
        IMAGE_THUNK_DATA* names;
        IMAGE_THUNK_DATA* addresses;
        size_t index;
        if (lstrcmpiA((const char*)(base + descriptor->Name), library_name) != 0) {
            continue;
        }
        if (diagnostics != NULL) {
            *diagnostics |= 0x2000;
        }
        if (descriptor->OriginalFirstThunk == 0 || descriptor->FirstThunk == 0) {
            continue;
        }
        names = (IMAGE_THUNK_DATA*)(base + descriptor->OriginalFirstThunk);
        addresses = (IMAGE_THUNK_DATA*)(base + descriptor->FirstThunk);
        for (index = 0; names[index].u1.AddressOfData != 0; index++) {
            IMAGE_IMPORT_BY_NAME* imported_name;
            if (IMAGE_SNAP_BY_ORDINAL(names[index].u1.Ordinal)) {
                if (IMAGE_ORDINAL(names[index].u1.Ordinal) != function_ordinal) {
                    continue;
                }
            } else {
                imported_name = (IMAGE_IMPORT_BY_NAME*)(base + names[index].u1.AddressOfData);
                if (strcmp((const char*)imported_name->Name, function_name) != 0) {
                    continue;
                }
            }
            if (diagnostics != NULL) {
                *diagnostics |= 0x4000;
            }
            if (addresses[index].u1.Function == (ULONG_PTR)replacement) {
                /* A late-module retry still counts an already-correct slot. */
                patched++;
                continue;
            }
            if (!patch_pointer((void**)&addresses[index].u1.Function,
                (void*)addresses[index].u1.Function, replacement)) {
                if (diagnostics != NULL) {
                    *diagnostics |= 0x8000;
                }
                continue;
            }
            patched++;
        }
    }
    return patched;
}

int fang_patch_import_address(HMODULE module, void* original, void* replacement, unsigned int* diagnostics) {
    BYTE* base = (BYTE*)module;
    IMAGE_DOS_HEADER* dos = (IMAGE_DOS_HEADER*)base;
    IMAGE_NT_HEADERS* nt;
    IMAGE_IMPORT_DESCRIPTOR* descriptor;
    int patched = 0;
    if (diagnostics != NULL) {
        *diagnostics |= 0x10000;
    }
    if (dos == NULL || dos->e_magic != IMAGE_DOS_SIGNATURE) {
        return 0;
    }
    nt = (IMAGE_NT_HEADERS*)(base + dos->e_lfanew);
    if (nt->Signature != IMAGE_NT_SIGNATURE) {
        return 0;
    }
    if (nt->OptionalHeader.DataDirectory[IMAGE_DIRECTORY_ENTRY_IMPORT].VirtualAddress == 0) {
        return 0;
    }
    descriptor = (IMAGE_IMPORT_DESCRIPTOR*)(base + nt->OptionalHeader.DataDirectory[IMAGE_DIRECTORY_ENTRY_IMPORT].VirtualAddress);
    for (; descriptor->Name != 0; descriptor++) {
        IMAGE_THUNK_DATA* addresses;
        size_t index;
        if (descriptor->FirstThunk == 0) {
            continue;
        }
        addresses = (IMAGE_THUNK_DATA*)(base + descriptor->FirstThunk);
        for (index = 0; addresses[index].u1.Function != 0; index++) {
            if (addresses[index].u1.Function != (ULONG_PTR)original) {
                continue;
            }
            if (diagnostics != NULL) {
                *diagnostics |= 0x20000;
            }
            if (addresses[index].u1.Function == (ULONG_PTR)replacement) {
                patched++;
                continue;
            }
            if (!patch_pointer((void**)&addresses[index].u1.Function,
                (void*)addresses[index].u1.Function, replacement)) {
                if (diagnostics != NULL) {
                    *diagnostics |= 0x40000;
                }
                continue;
            }
            patched++;
        }
    }
    return patched;
}






#include "mod/mod.h"

#include <string.h>
#include <wchar.h>

// Reads/modifies: retains only configured path bytes for process-lifetime bindings.
wchar_t* fang_mod_retain_path(const wchar_t* path) {
    size_t size;
    wchar_t* retained;
    if (path == NULL) return NULL;
    size = (wcslen(path) + 1) * sizeof(wchar_t);
    retained = (wchar_t*)HeapAlloc(GetProcessHeap(), 0, size);
    if (retained == NULL) return NULL;
    memcpy(retained, path, size);
    return retained;
}

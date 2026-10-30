#ifndef DARKSPIN_FANG_IMPORT_H
#define DARKSPIN_FANG_IMPORT_H

#include "fang.h"

/* The caller owns optional diagnostic bits; the importer owns no mutable state. */
int fang_patch_import(HMODULE module, const char* library_name,
    const char* function_name, WORD function_ordinal, void* replacement,
    unsigned int* diagnostics);
int fang_patch_import_address(HMODULE module, void* original, void* replacement,
    unsigned int* diagnostics);

#endif

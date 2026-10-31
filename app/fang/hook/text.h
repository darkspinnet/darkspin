#ifndef DARKSPIN_FANG_TEXT_H
#define DARKSPIN_FANG_TEXT_H

#include <stddef.h>
#include <wchar.h>

int fang_set_translation(const char* source, const char* translation);
const char* fang_text(const char* source);
const wchar_t* fang_text_wide(const wchar_t* source);
void fang_format_text(char* output, size_t capacity, const char* source,
    const char* const* arguments, unsigned int count);

#endif

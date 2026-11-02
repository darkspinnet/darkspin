#include "hook.h"

#include <stdlib.h>
#include <string.h>

typedef struct fang_translation {
    char* source;
    char* text;
    wchar_t* wide_source;
    wchar_t* wide_text;
    struct fang_translation* next;
} fang_translation;

/* Initialized before installing hooks; strings live until process exit so
 * Scaleform and native caption setters never receive a freed translation. */
static fang_translation* translations;

// Reads: copies validated UTF-8 into a process-owned native caption.
static wchar_t* wide_text(const char* text) {
    int length = MultiByteToWideChar(CP_UTF8, MB_ERR_INVALID_CHARS, text, -1, NULL, 0);
    wchar_t* output;
    if (!length) return NULL;
    output = (wchar_t*)calloc(length, sizeof(wchar_t));
    if (!output) return NULL;
    if (!MultiByteToWideChar(CP_UTF8, MB_ERR_INVALID_CHARS, text, -1, output, length)) {
        free(output);
        return NULL;
    }
    return output;
}

// Modifies: retains a translation before any client UI hook becomes active.
int fang_set_translation(const char* source, const char* translation) {
    fang_translation* entry;
    if (!source || !translation || !*translation || strcmp(source, translation) == 0) return 1;
    entry = (fang_translation*)calloc(1, sizeof(*entry));
    if (!entry) return 0;
    entry->source = _strdup(source);
    entry->text = _strdup(translation);
    entry->wide_source = wide_text(source);
    entry->wide_text = wide_text(translation);
    if (!entry->source || !entry->text || !entry->wide_source || !entry->wide_text) {
        free(entry->source);
        free(entry->text);
        free(entry->wide_source);
        free(entry->wide_text);
        free(entry);
        return 0;
    }
    entry->next = translations;
    translations = entry;
    return 1;
}

// Reads: resolves a UTF-8 caption, preserving English for unfinished entries.
const char* fang_text(const char* source) {
    fang_translation* entry;
    if (!source) return source;
    for (entry = translations; entry; entry = entry->next) {
        if (strcmp(entry->source, source) == 0) return entry->text;
    }
    return source;
}

// Reads: resolves a UTF-16 caption for native build-103 controls.
const wchar_t* fang_text_wide(const wchar_t* source) {
    fang_translation* entry;
    if (!source) return source;
    for (entry = translations; entry; entry = entry->next) {
        if (wcscmp(entry->wide_source, source) == 0) return entry->wide_text;
    }
    return source;
}

// Modifies: formats numbered catalog slots without interpreting translated text
// as a printf format. Slots may be reordered and all inserted text stays literal.
void fang_format_text(char* output, size_t capacity, const char* source,
    const char* const* arguments, unsigned int count) {
    const char* cursor = fang_text(source);
    size_t used = 0;
    if (!capacity) return;
    while (*cursor && used + 1 < capacity) {
        const char* part = cursor;
        size_t length = 1;
        if (cursor[0] == '{' && cursor[1] >= '0' && cursor[1] <= '9' && cursor[2] == '}') {
            unsigned int index = (unsigned int)(cursor[1] - '0');
            if (index < count && arguments[index]) {
                part = arguments[index];
                length = strlen(part);
                cursor += 3;
            } else {
                ++cursor;
            }
        } else {
            ++cursor;
        }
        if (length > capacity - used - 1) length = capacity - used - 1;
        memcpy(output + used, part, length);
        used += length;
    }
    /* A long translated hint can end at capacity. Remove an incomplete UTF-8
     * sequence so its conversion to the native wide caption still succeeds. */
    if (used) {
        size_t start = used - 1;
        unsigned char lead;
        size_t length;
        while (start && ((unsigned char)output[start] & 0xC0) == 0x80) --start;
        lead = (unsigned char)output[start];
        length = lead < 0x80 ? 1 : lead < 0xE0 ? 2 : lead < 0xF0 ? 3 : 4;
        if (used - start < length) used = start;
    }
    output[used] = '\0';
}

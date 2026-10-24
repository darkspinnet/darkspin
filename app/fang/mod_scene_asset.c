#include "hook.h"

#include <stdint.h>
#include <string.h>

static uint32_t scene_word(const BYTE* source) {
    uint32_t word;
    memcpy(&word, source, 4);
    return word;
}

/* Bounded RefPack expansion for the private backdrop copy. The database
 * stream continues serving every other resource directly from its source. */
int fang_mod_scene_expand(const BYTE* source, unsigned int size, BYTE* output, unsigned int decoded) {
    unsigned int cursor = 5;
    unsigned int position = 0;
    if (size < 5 || source[1] != 0xfb ||
        (((unsigned int)source[2] << 16) | ((unsigned int)source[3] << 8) | source[4]) != decoded)
        return 0;
    while (cursor < size) {
        unsigned int control = source[cursor++];
        unsigned int literal;
        unsigned int copies = 0;
        unsigned int distance = 0;
        unsigned int index;
        if (control >= 0xfc) literal = control & 3;
        else if (control >= 0xe0) literal = ((control & 31) << 2) + 4;
        else if (control >= 0xc0) {
            if (size - cursor < 3) return 0;
            literal = control & 3;
            copies = ((control & 12) << 6) + source[cursor + 2] + 5;
            distance = ((control & 16) << 12) + ((unsigned int)source[cursor] << 8) + source[cursor + 1] + 1;
            cursor += 3;
        } else if (control >= 0x80) {
            if (size - cursor < 2) return 0;
            literal = source[cursor] >> 6;
            copies = (control & 63) + 4;
            distance = ((source[cursor] & 63) << 8) + source[cursor + 1] + 1;
            cursor += 2;
        } else {
            if (cursor == size) return 0;
            literal = control & 3;
            copies = ((control & 28) >> 2) + 3;
            distance = ((control & 96) << 3) + source[cursor++] + 1;
        }
        if (literal > size - cursor || literal > decoded - position) return 0;
        memcpy(output + position, source + cursor, literal);
        cursor += literal;
        position += literal;
        if (copies && (!distance || distance > position || copies > decoded - position)) return 0;
        for (index = 0; index < copies; ++index) {
            output[position] = output[position - distance];
            ++position;
        }
        if (control >= 0xfc) return position == decoded;
    }
    return position == decoded;
}

static BYTE* scene_material(const BYTE* source, unsigned int stored,
    unsigned int decoded, unsigned int compression, unsigned int selected, unsigned int expected) {
    BYTE* payload;
    unsigned int offset;
    unsigned int count;
    unsigned int index;
    unsigned int changed = 0;
    uint32_t renderer = 0x80000003u;
    if (decoded < 108 || decoded > 32 * 1024 * 1024) return NULL;
    payload = (BYTE*)HeapAlloc(GetProcessHeap(), 0, decoded);
    if (!payload) return NULL;
    if (compression == 0 && stored == decoded) memcpy(payload, source, decoded);
    else if (compression != 0xffff || !fang_mod_scene_expand(source, stored, payload, decoded)) goto failed;
    if (memcmp(payload, "\x89RW4w32\0", 8)) goto failed;
    count = scene_word(payload + 0x24);
    offset = scene_word(payload + 0x30);
    if (offset > decoded || count > (decoded - offset) / 24) goto failed;
    /* Keep source vertex declarations, textures, UVs and render states, but
     * use the sky/vegetation's working unlit textured renderer for ground.
     * The fallback swatch has two lit surfaces; each actual environment
     * model has one, checked against its inspected renderer identity. */
    for (index = 0; index < count; ++index) {
        const BYTE* section = payload + offset + index * 24;
        unsigned int start = scene_word(section);
        unsigned int size = scene_word(section + 8);
        unsigned int shader;
        if (scene_word(section + 20) != 0x2000b) continue;
        if (start > decoded || size < 32 || size > decoded - start) goto failed;
        shader = scene_word(payload + start + 24);
        if (shader != 0x80000002u && shader != 0x80000004u) continue;
        if (selected && shader != selected) continue;
        memcpy(payload + start + 24, &renderer, 4);
        ++changed;
    }
    if (changed != expected) goto failed;
    return payload;

failed:
    HeapFree(GetProcessHeap(), 0, payload);
    return NULL;
}

BYTE* fang_mod_scene_floor(const BYTE* source, unsigned int stored,
    unsigned int decoded, unsigned int compression) {
    BYTE* payload = scene_material(source, stored, decoded, compression, 0, 2);
    if (payload) trace_client_state("mod_editor_scene_floor_materials", 2);
    return payload;
}

BYTE* fang_mod_scene_ground(const BYTE* source, unsigned int stored,
    unsigned int decoded, unsigned int compression, unsigned int shader) {
    return scene_material(source, stored, decoded, compression, shader, 1);
}

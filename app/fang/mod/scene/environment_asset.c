#include "mod/mod.h"
#include "hook.h"

#include <stdint.h>
#include <string.h>

/* Derive a private directory from the installed source on each launch. Keep
 * component indices intact, but expose only the captured editor entry effect.
 * No source payload or authored layout is compiled into Fang. */
// Reads: decodes a big-endian 32-bit word from source bytes.
static uint32_t environment_be32(const BYTE* p) {
    return ((uint32_t)p[0] << 24) | ((uint32_t)p[1] << 16) |
        ((uint32_t)p[2] << 8) | p[3];
}

// Reads: decodes a big-endian 16-bit word from source bytes.
static unsigned int environment_be16(const BYTE* p) {
    return ((unsigned int)p[0] << 8) | p[1];
}

// Modifies: writes a big-endian 32-bit word into the destination buffer.
static void environment_put32(BYTE* p, uint32_t n) {
    p[0] = (BYTE)(n >> 24); p[1] = (BYTE)(n >> 16);
    p[2] = (BYTE)(n >> 8); p[3] = (BYTE)n;
}

// Reads/modifies: bounds-checks a section span and advances the caller's cursor.
static int environment_skip(unsigned int* cursor, unsigned int size,
    unsigned int count, unsigned int stride) {
    if (*cursor > size || count > (size - *cursor) / stride) return 0;
    *cursor += count * stride;
    return 1;
}

// Reads/modifies: validates the installed effect directory and writes its export offset and selected effect index.
static int environment_exports(const BYTE* source, unsigned int size,
    unsigned int* exports, unsigned int* selected) {
    unsigned int cursor = 2, section, count, index, effect_count;
    int is_found = 0;
    if (size < 4 || environment_be16(source) != 4) return 0;
    for (section = 0; section < 2; ++section) {
        unsigned int seen = 0;
        for (;;) {
            unsigned int type, bytes;
            if (size - cursor < 2) return 0;
            type = environment_be16(source + cursor);
            cursor += 2;
            if (type == 0xffff) break;
            if (++seen > 64 || size - cursor < 6) return 0;
            bytes = environment_be32(source + cursor + 2);
            cursor += 6;
            if (bytes < 4 || !environment_skip(&cursor, size, bytes, 1)) return 0;
        }
    }
    if (size - cursor < 6 || environment_be16(source + cursor) != 1) return 0;
    effect_count = environment_be32(source + cursor + 2);
    cursor += 6;
    if (effect_count > 65536) return 0;
    for (index = 0; index < effect_count; ++index) {
        unsigned int block;
        if (!environment_skip(&cursor, size, 25, 1) || size - cursor < 4) return 0;
        count = environment_be32(source + cursor);
        cursor += 4;
        if (!environment_skip(&cursor, size, count, 4) ||
            !environment_skip(&cursor, size, 16, 1) || size - cursor < 4) return 0;
        count = environment_be32(source + cursor);
        cursor += 4;
        if (count > 65536) return 0;
        for (block = 0; block < count; ++block) {
            unsigned int lod_count;
            if (!environment_skip(&cursor, size, 61, 1) || size - cursor < 4) return 0;
            lod_count = environment_be32(source + cursor);
            cursor += 4;
            if (!environment_skip(&cursor, size, lod_count, 12) ||
                !environment_skip(&cursor, size, 44, 1)) return 0;
        }
    }
    *exports = cursor;
    for (;;) {
        unsigned int effect;
        if (size - cursor < 4) return 0;
        effect = environment_be32(source + cursor);
        cursor += 4;
        if (effect == 0xffffffffu) break;
        if (size - cursor < 8 || effect >= effect_count) return 0;
        if (environment_be32(source + cursor + 4) == 0xfbecd426u) {
            if (is_found) return 0;
            *selected = effect;
            is_found = 1;
        }
        cursor += 8;
    }
    /* The inspected editor directory is self contained. Reject a changed
     * import contract rather than silently resolving it against stock effects. */
    return is_found && size - cursor == 8 &&
        (environment_be32(source + cursor) == 0 ||
         environment_be32(source + cursor) == 0xffffffffu) &&
        environment_be32(source + cursor + 4) == 0;
}

/* Only the private effect directory is rewritten. The original source model
 * identities remain available to every other editor and gameplay resource. */
// Reads/modifies: validates private directory model records and rewrites their two installed model references to private aliases.
static int environment_alias_models(BYTE* source, unsigned int size) {
    unsigned int cursor = 2;
    unsigned int found = 0;
    while (cursor <= size && size - cursor >= 2) {
        unsigned int type = environment_be16(source + cursor);
        unsigned int bytes;
        unsigned int end;
        cursor += 2;
        if (type == 0xffff) return found == 3;
        if (size - cursor < 6) return 0;
        bytes = environment_be32(source + cursor + 2);
        if (bytes < 4 || bytes > size - cursor - 6) return 0;
        if (type != 37) { cursor += 6 + bytes; continue; }
        if (environment_be16(source + cursor) != 8) return 0;
        cursor += 6;
        end = cursor + bytes;
        {
            unsigned int count = environment_be32(source + cursor);
            unsigned int index;
            cursor += 4;
            if (count > 65536) return 0;
            for (index = 0; index < count; ++index) {
                unsigned int start = cursor;
                unsigned int animations;
                unsigned int animation;
                unsigned int groups;
                if (!environment_skip(&cursor, end, 36, 1) || end - cursor < 4) return 0;
                animations = environment_be32(source + cursor); cursor += 4;
                if (animations > 65536) return 0;
                for (animation = 0; animation < animations; ++animation) {
                    unsigned int keys;
                    if (!environment_skip(&cursor, end, 8, 1) || end - cursor < 4) return 0;
                    keys = environment_be32(source + cursor); cursor += 4;
                    if (!environment_skip(&cursor, end, keys, 4) ||
                        !environment_skip(&cursor, end, 13, 1)) return 0;
                }
                if (end - cursor < 4) return 0;
                groups = environment_be32(source + cursor); cursor += 4;
                if (!environment_skip(&cursor, end, groups, 6) || end - cursor < 4) return 0;
                /* The captured directory has no model splits. */
                if (environment_be32(source + cursor)) return 0;
                cursor += 4;
                if (end - cursor < 4) return 0;
                groups = environment_be32(source + cursor); cursor += 4;
                if (!environment_skip(&cursor, end, groups, 4) ||
                    !environment_skip(&cursor, end, 10, 1)) return 0;
                if (environment_be32(source + start + 28) != 0x40606000u) continue;
                if (environment_be32(source + start + 24) == 0xdaf31958u) {
                    environment_put32(source + start + 24, 0xfad0ce05u); found |= 1;
                } else if (environment_be32(source + start + 24) == 0x6261312au) {
                    environment_put32(source + start + 24, 0xfad0ce06u); found |= 2;
                }
            }
        }
        if (cursor != end) return 0;
    }
    return 0;
}

// Modifies: writes a little-endian 32-bit word into the destination buffer.
static void environment_le32(BYTE* p, uint32_t n) { memcpy(p, &n, 4); }

// Reads/modifies: allocates and assembles a private environment package from source copies, writes its size, and logs validation results.
BYTE* fang_mod_environment_package(const fang_environment_source* sources, unsigned int* size) {
    BYTE* payloads[5] = {0};
    unsigned int sizes[5] = {0};
    BYTE* package = NULL;
    unsigned int exports, selected, total = 244, index, position;
    static const uint32_t types[5] = {0xea5118b0u, 0x00b1b104u, 0x2f4e681bu, 0x00b1b104u, 0x2f4e681bu};
    static const uint32_t groups[5] = {0x40450200u, 0x40606000u, 0, 0x40606000u, 0};
    static const uint32_t instances[5] = {FANG_EDITOR_ENV_DIRECTORY, 0xfad0ce05u, 0xfad0ce05u, 0xfad0ce06u, 0xfad0ce06u};
    *size = 0;
    for (index = 0; index < 5; ++index) {
        const fang_environment_source* source = sources + index;
        if (!source->payload || !source->decoded || source->decoded > 8 * 1024 * 1024) goto finished;
        if (index == 2 || index == 4) {
            payloads[index] = fang_mod_scene_ground(source->payload, source->stored,
                source->decoded, source->compression, index == 2 ? 0x80000002u : 0x80000004u);
        } else {
            payloads[index] = (BYTE*)HeapAlloc(GetProcessHeap(), 0, source->decoded);
            if (!payloads[index]) goto finished;
            if (!source->compression && source->stored == source->decoded)
                memcpy(payloads[index], source->payload, source->decoded);
            else if (source->compression != 0xffff || !fang_mod_scene_expand(source->payload,
                source->stored, payloads[index], source->decoded)) goto finished;
        }
        if (!payloads[index]) goto finished;
        sizes[index] = source->decoded;
        if (index == 1 || index == 3) {
            uint32_t key[3];
            /* These installed wrappers put modelMeshLOD0 after the parent
             * property. The backdrop wrapper has a different property order;
             * its offset 88 is a bounding-box coordinate in these models. */
            if (sizes[index] != 104 ||
                environment_be32(payloads[index] + 28) != 0x00f9efbbu ||
                environment_be32(payloads[index] + 32) != 0x00200000u) {
                trace_client_state("mod_editor_environment_wrapper_invalid", index);
                goto finished;
            }
            memcpy(key, payloads[index] + 36, sizeof(key));
            if (key[0] != (index == 1 ? 0xdaf31958u : 0x6261312au) ||
                key[1] != 0x2f4e681bu || key[2]) {
                trace_client_state("mod_editor_environment_model_key_invalid", index);
                goto finished;
            }
            environment_le32(payloads[index] + 36, instances[index]);
        }
    }
    if (!environment_exports(payloads[0], sizes[0], &exports, &selected) ||
        !environment_alias_models(payloads[0], sizes[0])) goto finished;
    sizes[0] = exports + 24;
    for (index = 0; index < 5; ++index) total += sizes[index];
    package = (BYTE*)HeapAlloc(GetProcessHeap(), HEAP_ZERO_MEMORY, total);
    if (!package) goto finished;
    memcpy(package, "DBPF", 4);
    environment_le32(package + 4, 3); environment_le32(package + 36, 5);
    environment_le32(package + 44, 148); environment_le32(package + 60, 3);
    environment_le32(package + 64, 96); environment_le32(package + 96, 4);
    position = 244;
    for (index = 0; index < 5; ++index) {
        BYTE* entry = package + 104 + index * 28;
        environment_le32(entry, types[index]); environment_le32(entry + 4, groups[index]);
        environment_le32(entry + 8, instances[index]); environment_le32(entry + 12, position);
        environment_le32(entry + 16, sizes[index] | 0x80000000u); environment_le32(entry + 20, sizes[index]);
        memcpy(package + position, payloads[index], index ? sizes[index] : exports);
        if (!index) {
            environment_put32(package + position + exports, selected);
            environment_put32(package + position + exports + 8, FANG_EDITOR_ENV_ENTRY);
            /* Both numeric and string export tables require FFFFFFFF. */
            environment_put32(package + position + exports + 12, 0xffffffffu);
            environment_put32(package + position + exports + 16, 0xffffffffu);
        }
        position += sizes[index];
    }
    *size = total;
    trace_client_state("mod_editor_environment_private_export", selected);
    trace_client_state("mod_editor_environment_source_materials", 2);
finished:
    for (index = 0; index < 5; ++index)
        if (payloads[index]) HeapFree(GetProcessHeap(), 0, payloads[index]);
    if (!package) trace_client_state("mod_editor_environment_directory_unsupported", sources[0].decoded);
    return package;
}

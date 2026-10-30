#include "mod/mod.h"
#include "hook.h"

#include <stdint.h>
#include <string.h>

/* Build 103 audio manager: event contexts use +64 for integer parameters and
 * +68 for strings. Replace only the editor's music event, keeping its native
 * playback handle and stop/fade messages. No extra playback or polling loop.
 */
typedef int (FANG_THISCALL* music_integer_fn)(void*, uint32_t, uint32_t);
typedef int (FANG_THISCALL* music_string_fn)(void*, uint32_t, const char*, int);

#define MOD_EDITOR_MUSIC_EVENT 0x8e94e64au

typedef unsigned char (FANG_THISCALL* music_load_fn)(void*, uint32_t, void**, uint32_t);
static music_load_fn native_load;
static volatile LONG music_load_trace_count;
static music_integer_fn native_integer;
static music_string_fn native_string;
static volatile LONG music_trace_count;

static void music_trace(void) {
    if (InterlockedIncrement(&music_trace_count) <= 32)
        trace_client_state("mod_editor_music_selected", MOD_EDITOR_MUSIC_EVENT);
}

static int FANG_THISCALL mod_music_integer(void* manager, uint32_t parameter, uint32_t event) {
    /* FNV1: music_editor_lp -> fang_mod_creature_editor. */
    if (parameter == 55006081 && event == 0xf24bff79) {
        event = MOD_EDITOR_MUSIC_EVENT;
        music_trace();
    }
    return native_integer(manager, parameter, event);
}

static int FANG_THISCALL mod_music_string(void* manager, uint32_t parameter,
    const char* event, int length) {
    static const char original[] = "music_editor_lp";
    /* Native length 0 means a NUL-terminated string; explicit lengths may
     * include or exclude NUL. Do not reinterpret non-string parameters. */
    if (parameter == 55006078 &&
        (length == 0 || length == sizeof(original) - 1 || length == sizeof(original)) &&
        readable_range(event, sizeof(original)) &&
        _stricmp(event, original) == 0) {
        event = "fang_mod_creature_editor";
        length = 0;
        music_trace();
    }
    return native_string(manager, parameter, event, length);
}

/* A private event inherits Darkspore's _music_base rather than Spore's
 * patch_editor_base -> patch_base chain. The latter adds non-music groups
 * controlled by editor snapshots. Sample bytes stay in the mounted Spore bank.
 * Property headers/scalars are big-endian; resource-key items are little-endian.
 * One uncompressed DBPF resource exists entirely in the supplied memory stream.
 */
static void music_be32(BYTE* output, uint32_t word) {
    output[0] = (BYTE)(word >> 24); output[1] = (BYTE)(word >> 16);
    output[2] = (BYTE)(word >> 8); output[3] = (BYTE)word;
}

static void music_le32(BYTE* output, uint32_t word) {
    memcpy(output, &word, 4);
}

static unsigned int music_scalar(BYTE* output, uint32_t id, unsigned int type, uint32_t word) {
    music_be32(output, id);
    output[4] = 0; output[5] = (BYTE)type;
    music_be32(output + 8, word);
    return 12;
}

unsigned int fang_mod_music_package(BYTE* output, unsigned int capacity) {
    BYTE* property;
    BYTE* index;
    unsigned int length = 4;
    unsigned int index_offset;
    if (capacity < 320) return 0;
    memset(output, 0, 320);
    property = output + 96;
    music_be32(property, 7);
    length += music_scalar(property + length, 0x51997db2, 0x0a, 0xa852cbd1); /* soundtype */
    length += music_scalar(property + length, 0x25df0108, 0x0d, 0x3e99999a); /* gain .3 */
    music_be32(property + length, 0x5f6317d5); /* parent: _music_base */
    property[length + 5] = 0x20;
    music_le32(property + length + 8, 0xe34bae7b);
    length += 20;
    music_be32(property + length, 0x6dd08218); /* islooped */
    property[length + 5] = 1; property[length + 8] = 1;
    length += 9;
    music_be32(property + length, 0x701ed91e); /* samples */
    property[length + 5] = 0x20; property[length + 7] = 0x9c;
    music_be32(property + length + 8, 1);
    music_be32(property + length + 12, 12);
    music_le32(property + length + 16, 0x8b3eca7a);
    length += 28;
    length += music_scalar(property + length, 0xbaf7f4f9, 0x0a, 1); /* polyphony */
    length += music_scalar(property + length, 0xe8ab99b9, 0x0d, 0x40466666); /* fadeout 3.1 */
    memcpy(output, "DBPF", 4);
    music_le32(output + 4, 3);
    music_le32(output + 0x24, 1);
    music_le32(output + 0x2c, 36);
    music_le32(output + 0x3c, 3);
    index_offset = 96 + length;
    music_le32(output + 0x40, index_offset);
    index = output + index_offset;
    music_le32(index, 4); /* Native requires shared instance-high = 0. */
    music_le32(index + 8, 0x02b9f662);
    music_le32(index + 12, 0x021407ee);
    music_le32(index + 16, MOD_EDITOR_MUSIC_EVENT);
    music_le32(index + 20, 96);
    music_le32(index + 24, length | 0x80000000u);
    music_le32(index + 28, length);
    return index_offset + 36;
}

static unsigned char FANG_THISCALL mod_music_load(void* manager, uint32_t event,
    void** property, uint32_t group) {
    unsigned char result = native_load(manager, event, property, group);
    if (event == MOD_EDITOR_MUSIC_EVENT && InterlockedIncrement(&music_load_trace_count) <= 32)
        trace_client_state("mod_editor_music_event_loaded", result && property && *property);
    return result;
}

void fang_mod_install_music(BYTE* base, const wchar_t* manifest) {
    static const BYTE integer_signature[] = {
        0x81, 0xc1, 0x14, 0x01, 0x00, 0x00, 0xe9, 0x75, 0xc9, 0x01, 0x00
    };
    static const BYTE load_signature[] = {0x6a, 0xff, 0x68};
    static const BYTE string_signature[] = {
        0x81, 0xc1, 0x14, 0x01, 0x00, 0x00, 0xe9, 0x75, 0xc9, 0x01, 0x00
    };
    /* Retail exposes both the base and concrete audio-manager tables. Their
     * parameter writers are inherited unchanged; cover either active manager. */
    static const unsigned int slots[] = {16, 17, 80};
    void* originals[3];
    void* replacements[3];
    void** tables[] = {(void**)(base + 0xc671a8), (void**)(base + 0xc67798)};
    unsigned int index;
    unsigned int installed = 0;
    if (GetPrivateProfileIntW(L"mod", L"editor_music", 0, manifest) != 1) return;
    if (!readable_range(base + 0x844380, sizeof(load_signature)) ||
        memcmp(base + 0x844380, load_signature, sizeof(load_signature)) != 0 ||
        !readable_range(base + 0x8418b0, sizeof(integer_signature)) ||
        !readable_range(base + 0x8418c0, sizeof(string_signature)) ||
        memcmp(base + 0x8418b0, integer_signature, sizeof(integer_signature)) != 0 ||
        memcmp(base + 0x8418c0, string_signature, sizeof(string_signature)) != 0) {
        trace_client_state("mod_editor_music_unsupported", 1);
        return;
    }
    for (index = 0; index < sizeof(tables) / sizeof(tables[0]); ++index) {
        void** methods = tables[index];
        if (!readable_range(methods, 324) || methods[0] != base + 0x8456c0 ||
            methods[16] != base + 0x8418b0 || methods[17] != base + 0x8418c0 || methods[80] != base + 0x844380) {
            trace_client_state("mod_editor_music_unsupported", index + 2);
            return;
        }
    }
    native_integer = (music_integer_fn)(base + 0x8418b0);
    native_string = (music_string_fn)(base + 0x8418c0);
    native_load = (music_load_fn)(base + 0x844380);
    originals[0] = native_integer; originals[1] = native_string; originals[2] = native_load;
    replacements[0] = mod_music_integer; replacements[1] = mod_music_string; replacements[2] = mod_music_load;
    for (index = 0; index < 6; ++index) {
        unsigned int slot = index % 3;
        if (!patch_pointer(&tables[index / 3][slots[slot]], originals[slot], replacements[slot])) break;
        ++installed;
    }
    if (installed != 6) {
        trace_client_state("mod_editor_music_hook_failed", installed + 1);
        while (installed) {
            unsigned int index = --installed;
            unsigned int slot = index % 3;
            if (!patch_pointer(&tables[index / 3][slots[slot]], replacements[slot], originals[slot]))
                trace_client_state("mod_editor_music_rollback_failed", index + 1);
        }
        return;
    }
    trace_client_state("mod_editor_music_enabled", 1);
}

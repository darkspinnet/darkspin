#include "mod/mod.h"
#include "hook.h"

#include <stdint.h>
#include <string.h>

/* Build-103 import observers with separately gated bound-creature material
 * compatibility. Preserve native results and PNG bytes; never suppress
 * resetCreature or bypass the model validator. ABIs and stack cleanup were
 * checked against the executable's actual calling conventions.
 */
typedef unsigned char (FANG_THISCALL* mod_png_fn)(void*, void*);
typedef unsigned char (FANG_THISCALL* mod_header_fn)(void*, uint32_t*);
typedef unsigned char (FANG_THISCALL* mod_crc_fn)(void*, unsigned char);
typedef unsigned char (__cdecl* mod_metadata_fn)(uint32_t*, void*);
typedef unsigned char (__cdecl* mod_import_fn)(void*, uint32_t, void**, uint32_t*, void*);
typedef unsigned char (__stdcall* mod_validate_fn)(uint32_t*);
typedef unsigned char (__cdecl* mod_mask_fn)(uint32_t, uint32_t, uint32_t, uint32_t,
    uint32_t, uint32_t, uint32_t, uint32_t);
typedef void* (__cdecl* mod_metadata_cast_fn)(void**);
typedef uint32_t* (__cdecl* mod_model_cast_fn)(void*);

static mod_png_fn png_decode;
static mod_header_fn png_header;
static mod_crc_fn png_crc;
static mod_metadata_fn png_metadata;
static mod_import_fn png_import;
static mod_validate_fn model_validate;
static mod_mask_fn model_mask;
static mod_metadata_cast_fn metadata_cast;
static mod_model_cast_fn model_cast;
static int is_material_enabled;
static BYTE* source_base;
static mod_import_fn source_import __attribute__((used));

/* The importer also carries an optional decode context in ESI. Source imports
 * are synchronous: provide NULL and preserve the caller's nonvolatile ESI. */
static unsigned char __attribute__((naked, noinline)) mod_source_import(void* stream __attribute__((unused)),
    uint32_t instance __attribute__((unused)), void** asset __attribute__((unused)),
    uint32_t* metadata __attribute__((unused)), void* decoder __attribute__((unused))) {
    __asm__(
        "pushl %ebp\nmovl %esp, %ebp\npushl %esi\n"
        "pushl 24(%ebp)\npushl 20(%ebp)\npushl 16(%ebp)\n"
        "pushl 12(%ebp)\npushl 8(%ebp)\nxorl %esi, %esi\n"
        "call *_source_import\naddl $20, %esp\npopl %esi\nleave\nret\n");
}

static void mod_import_material(uint32_t* model) {
    /* Build 103's CRT XML writer 0x6D8DD0 identifies material type at +92,
     * followed by two float4 parameters at +96/+112. 0x69BCE0 transfers them
     * into the native editor appearance. Legacy Spore XML omits all nine
     * words. Only fill a completely absent material on an allowlisted import;
     * preserve every paint/effect/color field preceding this range.
     * Defaults are the stock Blitz CRT's authored material parameters.
     */
    static const uint32_t absent[9] = {0};
    static const float parameters[8] = {0.35f, 0.35f, 0.35f, 0.12f, 1.f, 1.f, 1.f, 2.f};
    if (!readable_range(model, 128)) return;
    trace_client_state("mod_model_material_type", model[23]);
    trace_client_state("mod_model_material_param1", model[24]);
    trace_client_state("mod_model_material_param2", model[28]);
    if (!is_material_enabled || memcmp(model + 23, absent, sizeof(absent)) != 0) return;
    model[23] = 0x35daf187;
    memcpy(model + 24, parameters, sizeof(parameters));
    trace_client_state("mod_model_material_fallback", model[2]);
}

int fang_mod_import_source(const wchar_t* path, uint32_t instance, uint32_t* key, void** retained) {
    typedef void* (FANG_THISCALL* initialize_fn)(void*);
    typedef void (FANG_THISCALL* destroy_fn)(void*);
    typedef void (FANG_THISCALL* release_fn)(void*);
    typedef unsigned char (FANG_THISCALL* preference_get_fn)(void*, uint32_t);
    typedef void (FANG_THISCALL* preference_set_fn)(void*, uint32_t, uint32_t);
    uint32_t metadata[48] = {0};
    uint32_t decoder[24] = {0};
    void* asset = NULL;
    void* stream;
    uint32_t* model;
    unsigned char is_imported;
    unsigned char is_valid;
    unsigned char previous_import_mode;
    void* preference;
    if (!source_base || !instance || !key || !retained) return 0;
    stream = fang_mod_open_source(path);
    if (!stream) { trace_client_state("mod_character_png_open_failed", instance); return 0; }
    ((initialize_fn)(source_base + 0xa2650))(metadata);
    ((initialize_fn)(source_base + 0x2643d0))(metadata);
    ((initialize_fn)(source_base + 0x3e2650))(decoder);
    is_imported = mod_source_import(stream, instance, &asset, metadata, decoder);
    ((destroy_fn)(source_base + 0x604f0))(decoder);
    ((destroy_fn)(source_base + 0xa2690))(metadata);
    fang_mod_close_source(stream);
    model = asset ? model_cast(asset) : NULL;
    if (!is_imported || !readable_range(model, 128) || model[3] != 0x2b978c46) {
        if (asset && readable_range(*(void***)asset, 8))
            ((release_fn)(*(void***)asset)[1])(asset);
        trace_client_state("mod_character_png_import_failed", instance);
        return 0;
    }
    /* Source identities were allowlisted by the manifest before this call. */
    uint32_t identity[11] = {6};
    identity[10] = instance;
    if (!fang_mod_bind_baseline(identity, model + 2)) {
        ((release_fn)(*(void***)asset)[1])(asset);
        return 0;
    }
    mod_import_material(model);
    /* Mirror the native PNG completion path's validation context (0x4A2C30
     * through 0x4A2C94), restoring its prior preference after validation. */
    preference = *(void**)(source_base + 0x105db94);
    if (!readable_range(preference, 60)) {
        ((release_fn)(*(void***)asset)[1])(asset);
        return 0;
    }
    previous_import_mode = ((preference_get_fn)(source_base + 0x3b6600))(preference, 0x0b3979bf);
    ((preference_set_fn)(source_base + 0x3b58b0))(preference, 0x0b3979bf, 1);
    is_valid = model_validate(model);
    ((preference_set_fn)(source_base + 0x3b58b0))(preference, 0x0b3979bf, previous_import_mode);
    if (!is_valid) {
        ((release_fn)(*(void***)asset)[1])(asset);
        trace_client_state("mod_character_validation_failed", instance);
        return 0;
    }
    memcpy(key, model + 2, 12);
    *retained = asset;
    trace_client_state("mod_character_png_imported", instance);
    return 1;
}

static unsigned char __stdcall mod_observe_validation(uint32_t* model) {
    /* 0x69B070 never reads incoming ECX and returns with RET 4. The
     * decompiler's implicit floating-point argument is not a stack argument. */
    unsigned char is_valid = model_validate(model);
    trace_client_state("mod_model_validate", is_valid);
    if (readable_range(model, 160)) {
        trace_client_state("mod_model_type", model[6]);
        if (model[39] >= model[38])
            trace_client_state("mod_model_block_count", (model[39] - model[38]) / 480);
    }
    return is_valid;
}

static unsigned char __cdecl mod_observe_mask(uint32_t first, uint32_t second,
    uint32_t third, uint32_t fourth, uint32_t first_mask, uint32_t second_mask,
    uint32_t third_mask, uint32_t fourth_mask) {
    unsigned char is_valid = model_mask(first, second, third, fourth,
        first_mask, second_mask, third_mask, fourth_mask);
    trace_client_state("mod_model_error_bits0", first);
    trace_client_state("mod_model_error_bits1", second);
    trace_client_state("mod_model_error_bits2", third);
    trace_client_state("mod_model_error_bits3", fourth);
    trace_client_state("mod_model_rejected_bits0", first & first_mask);
    trace_client_state("mod_model_rejected_bits1", second & second_mask);
    trace_client_state("mod_model_rejected_bits2", third & third_mask);
    trace_client_state("mod_model_rejected_bits3", fourth & fourth_mask);
    return is_valid;
}

static void* __cdecl mod_observe_metadata_cast(void** resource) {
    void* metadata = metadata_cast(resource);
    trace_client_state("mod_model_metadata_resolved", metadata != NULL);
    return metadata;
}

static unsigned char FANG_THISCALL mod_observe_decode(void* decoder, void* stream) {
    unsigned char is_valid = png_decode(decoder, stream);
    trace_client_state("mod_png_decode", is_valid);
    return is_valid;
}

static unsigned char FANG_THISCALL mod_observe_header(void* decoder, uint32_t* size) {
    unsigned char is_valid = png_header(decoder, size);
    trace_client_state("mod_png_header", is_valid);
    if (is_valid && readable_range(size, sizeof(*size)))
        trace_client_state("mod_png_payload_size", *size);
    return is_valid;
}

static unsigned char FANG_THISCALL mod_observe_crc(void* decoder, unsigned char is_rewound) {
    unsigned char is_valid = png_crc(decoder, is_rewound);
    trace_client_state("mod_png_crc", is_valid);
    return is_valid;
}

static unsigned char __cdecl mod_observe_metadata(uint32_t* metadata, void* stream) {
    unsigned char is_valid = png_metadata(metadata, stream);
    trace_client_state("mod_png_metadata", is_valid);
    if (is_valid && readable_range(metadata, 12)) {
        trace_client_state("mod_png_metadata_version", metadata[0]);
        trace_client_state("mod_png_resource_type", metadata[1]);
        trace_client_state("mod_png_resource_group", metadata[2]);
    }
    return is_valid;
}

static unsigned char __cdecl mod_observe_import(void* stream, uint32_t instance,
    void** asset, uint32_t* metadata, void* decoder) {
    unsigned char is_valid;
    trace_client_state("mod_png_import_instance", instance);
    is_valid = png_import(stream, instance, asset, metadata, decoder);
    trace_client_state("mod_png_import", is_valid);
    if (is_valid && readable_range(metadata, 44) && readable_range(asset, sizeof(*asset)) && *asset) {
        uint32_t* model = model_cast(*asset);
        if (readable_range(model, 20) && model[2] == instance &&
            fang_mod_bind_baseline(metadata, model + 2))
            mod_import_material(model);
    }
    return is_valid;
}

static int mod_import_call(BYTE* call, BYTE* target) {
    int32_t displacement;
    if (!readable_range(call, 5) || *call != 0xe8) return 0;
    memcpy(&displacement, call + 1, sizeof(displacement));
    return call + 5 + displacement == target;
}

int fang_mod_observe_import(BYTE* base, const wchar_t* manifest) {
    unsigned int offsets[] = {0x26446f, 0x26448e, 0x264531, 0x266fe7, 0xa2b05,
        0xa2c7b, 0x29b104, 0x29b153, 0xa2bad};
    unsigned int targets[] = {0x3e2ae0, 0x3e29c0, 0x3e2880, 0x266a90, 0x266fa0,
        0x29b070, 0x2b7960, 0x2b7960, 0xa1d90};
    void* replacements[] = {mod_observe_decode, mod_observe_header, mod_observe_crc,
        mod_observe_metadata, mod_observe_import, mod_observe_validation,
        mod_observe_mask, mod_observe_mask, mod_observe_metadata_cast};
    unsigned int index, installed = 0;
    const unsigned int count = sizeof(offsets) / sizeof(offsets[0]);
    is_material_enabled = GetPrivateProfileIntW(L"mod", L"model_material", 0, manifest) == 1;
    for (index = 0; index < count; ++index) {
        if (!mod_import_call(base + offsets[index], base + targets[index])) return 0;
    }
    png_decode = (mod_png_fn)(base + targets[0]);
    png_header = (mod_header_fn)(base + targets[1]);
    png_crc = (mod_crc_fn)(base + targets[2]);
    png_metadata = (mod_metadata_fn)(base + targets[3]);
    png_import = (mod_import_fn)(base + targets[4]);
    model_validate = (mod_validate_fn)(base + targets[5]);
    model_mask = (mod_mask_fn)(base + targets[6]);
    metadata_cast = (mod_metadata_cast_fn)(base + targets[8]);
    model_cast = (mod_model_cast_fn)(base + 0xa1a90);
    for (index = 0; index < count; ++index) {
        if (!patch_call(base + offsets[index], base + targets[index], replacements[index])) goto rollback;
        ++installed;
    }
    trace_client_state("mod_model_material_enabled", is_material_enabled);
    source_base = base;
    source_import = png_import;
    return 1;
rollback:
    while (installed) {
        --installed;
        if (!patch_call(base + offsets[installed], replacements[installed], base + targets[installed]))
            trace_client_state("mod_png_observer_rollback_failed", installed);
    }
    return 0;
}

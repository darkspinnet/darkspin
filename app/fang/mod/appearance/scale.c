#include "mod/mod.h"
#include "hook.h"

#include <float.h>
#include <stdint.h>
#include <string.h>

/* Explicitly approved Mod compatibility: retain imported geometry and expand
 * only the editor's authored part-size limits for registered bound models.
 * Missing parts, non-finite/degenerate scales and every unrelated validation
 * rule remain errors. No part property list, model bytes or installation file
 * is modified. Build 103 calls this one scale check from 0x6BB578.
 */
typedef unsigned char (__cdecl* mod_scale_fn)(const uint32_t*, uint32_t*);
typedef void* (__cdecl* mod_editor_fn)(void);
typedef void* (FANG_THISCALL* mod_part_fn)(void*, uint32_t, uint32_t);

static mod_scale_fn native_scale;
static mod_editor_fn native_editor;
static volatile LONG scale_trace_count;

// Reads/modifies: invokes native scale validation, checks bound parts, adjusts the permitted error bit, and records traces.
static unsigned char __cdecl mod_bound_scale(const uint32_t* model, uint32_t* errors) {
    unsigned char is_valid = native_scale(model, errors);
    unsigned int index, count;
    const uint32_t* blocks;
    void* editor;
    void** methods;
    mod_part_fn get_part;
    if (!readable_range(model, 160) || !fang_mod_is_bound_key(model + 2)) return is_valid;
    if (model[39] < model[38] || (model[39] - model[38]) % 480 != 0) return is_valid;
    count = (model[39] - model[38]) / 480;
    if (count > 1024) return is_valid;
    blocks = (const uint32_t*)(uintptr_t)model[38];
    if (count && !readable_range(blocks, count * 480)) return is_valid;
    editor = native_editor();
    if (!readable_range(editor, sizeof(void*))) return is_valid;
    methods = *(void***)editor;
    if (!readable_range(methods, 92) || !readable_range(methods[22], 1)) return is_valid;
    get_part = (mod_part_fn)methods[22];
    for (index = 0; index < count; ++index) {
        const uint32_t* block = blocks + index * 120;
        float scale;
        memcpy(&scale, block + 4, sizeof(scale));
        /* Keep the native hard lower bound, independently of editor-specific
         * min/max properties. Ordered comparisons also reject NaN/infinity. */
        if (!(scale >= 0.0000152587890625f && scale <= FLT_MAX) ||
            !get_part(editor, block[1], block[0])) {
            if (readable_range(errors, sizeof(*errors))) *errors |= 0x80;
            trace_client_state("mod_model_scale_invalid_block", index);
            return 0;
        }
    }
    if (is_valid) return is_valid;
    /* Native 0x6B6920 owns only bit 7. Retain all other accumulated errors. */
    if (readable_range(errors, sizeof(*errors))) *errors &= ~UINT32_C(0x80);
    if (InterlockedIncrement(&scale_trace_count) <= 64)
        trace_client_state("mod_model_scale_expanded", model[2]);
    return 1;
}

// Reads/modifies: checks manifest policy and native signatures, stores validation targets, and patches the bound scale-check call.
void fang_mod_install_scale(BYTE* base, const wchar_t* manifest) {
    static const BYTE scale_signature[] = {0x83, 0xec, 0x18, 0x53, 0x8b, 0x5c, 0x24, 0x20};
    if (GetPrivateProfileIntW(L"mod", L"model_scale", 0, manifest) != 1) return;
    if (!readable_range(base + 0x2b6920, sizeof(scale_signature)) ||
        memcmp(base + 0x2b6920, scale_signature, sizeof(scale_signature)) != 0 ||
        !readable_range(base + 0x264270, 6) ||
        base[0x264270] != 0xa1 || base[0x264275] != 0xc3) {
        trace_client_state("mod_scale_build_unsupported", 1);
        return;
    }
    native_scale = (mod_scale_fn)(base + 0x2b6920);
    native_editor = (mod_editor_fn)(base + 0x264270);
    /* Verified cdecl: two stack arguments, caller ADD ESP,8; native RET. */
    if (!patch_call(base + 0x2bb578, native_scale, mod_bound_scale)) {
        trace_client_state("mod_scale_hook_failed", 1);
        return;
    }
    trace_client_state("mod_scale_enabled", 1);
}

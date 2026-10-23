#include "hook.h"

#include <float.h>
#include <math.h>
#include <stdint.h>
#include <string.h>
#include <stdio.h>
#include <stdlib.h>
#include <wchar.h>

typedef struct {
    uint32_t noun;
    uint32_t source;
    float nominal_scale;
    float target_extent;
    float maximum_ratio;
} mod_fit_target;

/* Populated from the active profile's full noun catalog. Size targets are
 * authored bounds when available, otherwise an explicit two-unit envelope. */
static mod_fit_target fit_targets[128];
static unsigned int fit_target_count;

typedef int (__cdecl* fit_present_fn)(BYTE*, unsigned char, unsigned char);

typedef struct {
    BYTE* rig;
    BYTE* recipe;
    uint32_t noun;
    uint32_t key[3];
    float ratio;
    int is_traced;
} mod_fit;

static fit_present_fn native_present;
static mod_fit fits[512];
static volatile LONG fit_trace_count;
static volatile LONG fit_failure_count;

static const mod_fit_target* fit_model_target(const uint32_t* key) {
    const mod_fit_target* target = NULL;
    uint32_t source = 0, noun = 0;
    unsigned int index;
    if (!fang_mod_bound_identity(key, &source, &noun)) return NULL;
    for (index = 0; index < fit_target_count; ++index) {
        const mod_fit_target* candidate = &fit_targets[index];
        if (noun) {
            if (candidate->noun == noun) return candidate;
            continue;
        }
        if (candidate->source != source) continue;
        /* Original PNGs have no parent noun. Several bindings may share one
         * source when their normalized size policy agrees (e.g. heroes).
         * Do not guess when that source has conflicting noun-size targets. */
        if (target && (fabsf(target->target_extent / target->nominal_scale -
            candidate->target_extent / candidate->nominal_scale) > .0001f ||
            target->maximum_ratio != candidate->maximum_ratio)) return NULL;
        target = candidate;
    }
    return target;
}

static float fit_extent(BYTE* rig) {
    uintptr_t begin = *(uintptr_t*)(rig + 752);
    uintptr_t end = *(uintptr_t*)(rig + 756);
    uintptr_t joint;
    float minimums[3] = {FLT_MAX, FLT_MAX, FLT_MAX};
    float maximums[3] = {-FLT_MAX, -FLT_MAX, -FLT_MAX};
    float extent = 0.f;
    unsigned int axis;
    if (end <= begin || (end - begin) % 708 || (end - begin) / 708 > 1024 ||
        !readable_range((void*)begin, end - begin)) return 0.f;
    /* Runtime part templates have rest-space mesh centers at +324 and mesh
     * half-extents at +312 (0x969700). Enclose each oriented part by its sphere
     * so long horns/limbs count. Read rest data, never the animated pose, to
     * avoid size changes when a creature dances, crouches or idles.
     */
    for (joint = begin; joint < end; joint += 708) {
        BYTE* part = *(BYTE**)joint;
        float radius_squared = 0.f;
        float radius;
        if (!readable_range(part, 336)) return 0.f;
        for (axis = 0; axis < 3; ++axis) {
            float half_extent = *(float*)(part + 312 + 4 * axis);
            if (!isfinite(half_extent) || half_extent < 0.f) return 0.f;
            radius_squared += half_extent * half_extent;
        }
        radius = sqrtf(radius_squared);
        if (!isfinite(radius)) return 0.f;
        for (axis = 0; axis < 3; ++axis) {
            float center = *(float*)(part + 324 + 4 * axis);
            float minimum = center - radius;
            float maximum = center + radius;
            if (!isfinite(minimum) || !isfinite(maximum)) return 0.f;
            if (minimum < minimums[axis]) minimums[axis] = minimum;
            if (maximum > maximums[axis]) maximums[axis] = maximum;
        }
    }
    for (axis = 0; axis < 3; ++axis)
        if (maximums[axis] - minimums[axis] > extent) extent = maximums[axis] - minimums[axis];
    return extent;
}

static float fit_number(const wchar_t* manifest, const wchar_t* prefix, unsigned int index, float fallback) {
    wchar_t key[40], text[64];
    wchar_t* end;
    double number;
    _snwprintf(key, 40, L"%ls%u", prefix, index);
    if (!GetPrivateProfileStringW(L"mod", key, L"", text, 64, manifest)) return fallback;
    number = wcstod(text, &end);
    if (end == text || *end || !isfinite(number) || number <= 0 || number > FLT_MAX) return fallback;
    return (float)number;
}

static mod_fit* fit_binding(BYTE* rig) {
    BYTE* recipe;
    const mod_fit_target* target;
    uint32_t key[3];
    unsigned int index;
    mod_fit* empty = NULL;
    float extent;
    if (!readable_range(rig, 760)) return NULL;
    recipe = *(BYTE**)rig;
    if (!readable_range(recipe, 804)) return NULL;
    key[0] = *(uint32_t*)(recipe + 792);
    key[1] = 0x2b978c46;
    key[2] = *(uint32_t*)(recipe + 796) & 0xe0ffffff;
    target = fit_model_target(key);
    if (!target) return NULL;
    for (index = 0; index < sizeof(fits) / sizeof(fits[0]); ++index) {
        mod_fit* fit = &fits[index];
        if (!fit->rig && !empty) empty = fit;
        if (fit->rig == rig) {
            if (fit->recipe == recipe && fit->noun == target->noun &&
                memcmp(fit->key, key, sizeof(key)) == 0)
                return fit;
            empty = fit;
            break;
        }
    }
    if (!empty) {
        if (InterlockedIncrement(&fit_failure_count) <= 32) trace_client_state("mod_fit_capacity", 1);
        return NULL;
    }
    extent = fit_extent(rig);
    if (!(extent > .0001f && isfinite(extent))) {
        if (InterlockedIncrement(&fit_failure_count) <= 32)
            trace_client_state("mod_fit_bounds_invalid", key[0]);
        return NULL;
    }
    memset(empty, 0, sizeof(*empty));
    empty->rig = rig;
    empty->recipe = recipe;
    empty->noun = target->noun;
    memcpy(empty->key, key, sizeof(key));
    empty->ratio = target->target_extent / (target->nominal_scale * extent);
    if (empty->ratio > target->maximum_ratio) empty->ratio = target->maximum_ratio;
    trace_client_state("mod_fit_hero", target->noun);
    trace_client_state("mod_fit_model", key[0]);
    {
        uint32_t bits;
        memcpy(&bits, &extent, sizeof(bits));
        trace_client_state("mod_fit_extent_bits", bits);
        memcpy(&bits, &empty->ratio, sizeof(bits));
        trace_client_state("mod_fit_ratio_bits", bits);
    }
    return empty;
}

/* Animated meshes normalize vertices by rig+112; cached/baked meshes do
 * not follow the same normalization. Fit the final graphics-model transform
 * after native presentation instead of changing the animation rig's scale.
 * The transform is rooted at the same world position, preserving foot origin.
 */
static int __cdecl mod_fit_present(BYTE* animator, unsigned char mode, unsigned char is_hidden) {
    int result = native_present(animator, mode, is_hidden);
    BYTE* rig;
    BYTE* node;
    mod_fit* fit;
    float native_scale;
    float fitted_scale;
    if (!readable_range(animator, 396)) return result;
    rig = *(BYTE**)(animator + 388);
    fit = fit_binding(rig);
    if (!fit) return result;
    node = *(BYTE**)(animator + 392);
    if (!readable_range(node, 52)) return result;
    /* Both native presentation branches source the root model scale from
     * rig+112. Read that untouched input, never our previous model output. */
    native_scale = *(float*)(rig + 112);
    fitted_scale = native_scale * fit->ratio;
    if (!(fitted_scale > .0000152587890625f && isfinite(fitted_scale))) return result;
    *(float*)(node + 24) = fitted_scale;
    ++*(WORD*)(node + 10); /* Native graphics-transform generation counter. */
    if (!fit->is_traced && InterlockedIncrement(&fit_trace_count) <= 128) {
        uint32_t bits;
        fit->is_traced = 1;
        memcpy(&bits, &native_scale, 4);
        trace_client_state("mod_fit_native_scale_bits", bits);
        memcpy(&bits, &fitted_scale, 4);
        trace_client_state("mod_fit_effective_scale_bits", bits);
        trace_client_state("mod_fit_render_path", *(void**)(rig + 720) != NULL);
    }
    return result;
}

void fang_mod_install_fit(BYTE* base, const wchar_t* manifest) {
    static const BYTE present_signature[] = {0x83, 0xec, 0x38, 0x53, 0x55, 0x8b, 0x6c, 0x24, 0x44};
    static const unsigned int calls[] = {0x54109f, 0x5410ba, 0x5410d6, 0x54121d, 0x542eae};
    unsigned int index;
    unsigned int installed = 0;
    if (GetPrivateProfileIntW(L"mod", L"model_fit", 0, manifest) != 1) return;
    if (!readable_range(base + 0x540e80, sizeof(present_signature)) ||
        memcmp(base + 0x540e80, present_signature, sizeof(present_signature)) != 0) {
        trace_client_state("mod_fit_unsupported", 1);
        return;
    }
    for (index = 1; index <= 128; ++index) {
        wchar_t key[32];
        mod_fit_target* target = &fit_targets[fit_target_count];
        _snwprintf(key, 32, L"hero%u", index);
        target->noun = GetPrivateProfileIntW(L"mod", key, 0, manifest);
        if (!target->noun) continue;
        _snwprintf(key, 32, L"source%u", index);
        target->source = GetPrivateProfileIntW(L"mod", key, 0, manifest);
        target->nominal_scale = fit_number(manifest, L"nominal", index, 1.f);
        target->target_extent = fit_number(manifest, L"extent", index, 2.f * target->nominal_scale);
        _snwprintf(key, 32, L"is_npc%u", index);
        target->maximum_ratio = GetPrivateProfileIntW(L"mod", key, 0, manifest) == 1 ? FLT_MAX : 1.5f;
        ++fit_target_count;
    }
    if (!fit_target_count) return;
    native_present = (fit_present_fn)(base + 0x540e80);
    for (index = 0; index < sizeof(calls) / sizeof(calls[0]); ++index) {
        if (!patch_call(base + calls[index], native_present, mod_fit_present)) break;
        ++installed;
    }
    if (installed != sizeof(calls) / sizeof(calls[0])) {
        trace_client_state("mod_fit_hook_failed", installed + 1);
        while (installed) {
            if (!patch_call(base + calls[--installed], mod_fit_present, native_present))
                trace_client_state("mod_fit_rollback_failed", installed + 1);
        }
        return;
    }
    trace_client_state("mod_fit_enabled", 3);
}

#include "mod/mod.h"
#include "hook.h"

#include <stdio.h>
#include <string.h>

/* Build 103 retains 128 validation bits but its dialog selector only scans
 * 0..28. These first 28 resource identities match SporeModder-FX reg_file.txt
 * by FNV hash against Darkspore's table at 1125C28. Do not assume later bits
 * share Spore's meaning. Bit 28 is Darkspore's 6BA870 template comparison.
 * Unknown bits always retain their numeric identity.
 */
static const char* validation_names[] = {
    "Complexity exceeds editor limits", "Creation exceeds its budget",
    "Invalid part count", "Textures could not be loaded", "Invalid creation name",
    "Invalid parts", "Invalid parent connections", "Part scales outside limits",
    "Creation outside allowed bounds", "Creation too small", "Missing mouth",
    "Creation has not been painted", "Stats outside allowed ranges",
    "Unattached or floating parts", "Parts could not be loaded", "Invalid symmetry",
    "Required content pack missing", "Creation contains no parts", "Invalid paint",
    "Intersecting end effectors", "Creation name is blank", "Invalid name characters",
    "Creation too small on X axis", "Creation too small on Y axis",
    "Creation too small on Z axis", "Creation below minimum absolute Z size",
    "Creation failed to load", "Invalid limb",
    "Core body differs from its original template"
};

typedef void (FANG_THISCALL* validation_accept_fn)(BYTE*);
typedef int (FANG_THISCALL* validation_status_fn)(BYTE*);
typedef uint32_t* (FANG_THISCALL* validation_mask_fn)(BYTE*, uint32_t*);

static BYTE* validation_base;
static BYTE* validation_owner;
static validation_accept_fn validation_accept;
static uint32_t validation_bits[4];
static uint32_t validation_masks[4];
static unsigned int validation_status;
static unsigned int validation_count;
static int is_validation_visible;
typedef void (FANG_THISCALL* validation_ref_fn)(BYTE*);
typedef void (FANG_THISCALL* validation_flag_fn)(BYTE*, unsigned int, unsigned char);
typedef void (FANG_THISCALL* validation_caption_fn)(BYTE*, const wchar_t*);

typedef struct {
    BYTE* window;
    uint32_t flags;
    uint32_t color;
    float area[4];
    wchar_t caption[2048];
} validation_hint_window;

/* Native 74B7F0 loads this hint layout; 74A300 fills its existing text and
 * frame. Use those installed controls, not an Accept tooltip or a new panel.
 * Keep their references and presentation snapshots until validation clears. */
static validation_hint_window validation_hints[5];
static unsigned int validation_page;
static DWORD validation_next_page;
static int is_validation_hint_ready;

// Reads: validates a native validation-hint window and its virtual interface.
static int validation_is_window(BYTE* window) {
    return readable_range(window, 4) && readable_range(*(void**)window, 132);
}

// Modifies: sets native validation-hint window visibility.
static void validation_flag(BYTE* window, int is_visible) {
    ((validation_flag_fn)(*(void***)window)[31])(window, 1, is_visible != 0);
}

// Modifies: restores retained hint presentation, releases native window references, and clears snapshot readiness.
static void validation_release_hint(void) {
    unsigned int index;
    for (index = 0; index < 5; ++index) {
        validation_hint_window* hint = validation_hints + index;
        BYTE* window = hint->window;
        if (!window) continue;
        ((validation_caption_fn)(*(void***)window)[32])(window, hint->caption);
        ((void (FANG_THISCALL*)(BYTE*, const float*))(*(void***)window)[27])(window, hint->area);
        ((void (FANG_THISCALL*)(BYTE*, uint32_t))(*(void***)window)[23])(window, hint->color);
        validation_flag(window, hint->flags & 1);
        ((validation_ref_fn)(*(void***)window)[1])(window);
        memset(hint, 0, sizeof(*hint));
    }
    is_validation_hint_ready = 0;
}

// Modifies: clears validation display ownership and paging and restores retained native hint presentation.
void fang_mod_editor_clear_validation(void) {
    is_validation_visible = 0;
    validation_owner = NULL;
    validation_page = 0;
    validation_next_page = 0;
    validation_release_hint();
}

// Reads/modifies: copies native hint presentation into the supplied snapshot and retains the window reference.
static int validation_snapshot_hint(validation_hint_window* hint, BYTE* window) {
    const wchar_t* caption;
    const float* area;
    unsigned int index = 0;
    if (!validation_is_window(window)) return 0;
    /* IWindow slots 17/32 are GetCaption/SetCaption; 14/27 preserve the
     * authored area. Slot 9 returns state flags, 12/23 get/set text color. */
    caption = ((const wchar_t* (FANG_THISCALL*)(BYTE*))(*(void***)window)[17])(window);
    if (!caption) return 0;
    while (index + 1 < sizeof(hint->caption) / sizeof(hint->caption[0])) {
        if (!readable_range(caption + index, sizeof(*caption))) return 0;
        if (!caption[index]) break;
        hint->caption[index] = caption[index];
        ++index;
    }
    if (!readable_range(caption + index, sizeof(*caption)) || caption[index]) return 0;
    hint->caption[index] = 0;
    area = ((const float* (FANG_THISCALL*)(BYTE*))(*(void***)window)[14])(window);
    if (!readable_range(area, sizeof(hint->area))) return 0;
    memcpy(hint->area, area, sizeof(hint->area));
    hint->flags = ((uint32_t (FANG_THISCALL*)(BYTE*))(*(void***)window)[9])(window);
    hint->color = ((uint32_t (FANG_THISCALL*)(BYTE*))(*(void***)window)[12])(window);
    ((validation_ref_fn)(*(void***)window)[0])(window);
    hint->window = window;
    return 1;
}

// Reads/modifies: resolves and snapshots installed native hint controls, records readiness, and logs success.
static int validation_open_hint(void) {
    static const uint32_t ids[] = {UINT32_MAX, 86270024, 86258400, 86264936, 86264840};
    BYTE* manager;
    BYTE* layout;
    unsigned int index;
    if (!readable_range(validation_base + 0x105edd4, 4)) return 0;
    manager = *(BYTE**)(validation_base + 0x105edd4);
    if (!readable_range(manager, 96)) return 0;
    layout = *(BYTE**)(manager + 52);
    if (!readable_range(layout, 4)) return 0;
    for (index = 0; index < 5; ++index) {
        BYTE* window = ((BYTE* (FANG_THISCALL*)(BYTE*, uint32_t, int))
            (validation_base + 0x50c0e0))(layout, ids[index], 1);
        if (!validation_snapshot_hint(validation_hints + index, window)) {
            validation_release_hint();
            return 0;
        }
    }
    is_validation_hint_ready = 1;
    trace_client_state("mod_editor_save_hint_area", 1);
    return 1;
}

// Reads/modifies: formats the selected validation error into static text and writes the caller's page count.
const char* fang_mod_editor_validation_text(unsigned int page, unsigned int* pages) {
    static char text[1024];
    unsigned int bit;
    unsigned int row = 0;
    *pages = 0;
    if (!is_validation_visible || validation_owner != fang_mod_editor_active()) return NULL;
    *pages = validation_count ? validation_count : 1;
    page %= *pages;
    for (bit = 0; bit < 128; ++bit) {
        uint32_t flag = UINT32_C(1) << (bit % 32);
        const char* name;
        if (!(validation_bits[bit / 32] & flag) || row++ != page) continue;
        name = bit < sizeof(validation_names) / sizeof(validation_names[0]) ?
            validation_names[bit] : "Unmapped native validation error";
        snprintf(text, sizeof(text), "Save blocked (%u/%u): [%u] %s%s\n%s%s",
            page + 1, *pages, bit, name, validation_masks[bit / 32] & flag ? " [blocking]" : " [flagged]",
            bit == 28 ? "Removing legs can change the foot count required by the original template. " : "",
            *pages > 1 ? "Scroll over this hint for more errors; pages also advance automatically." :
                "Correct the creation, then try Accept again.");
        return text;
    }
    snprintf(text, sizeof(text), "Save blocked: %s",
        validation_status == 5 ? "Native editor counter limit exceeded." :
        "Native editor validation failed without model error bits. Details recorded in the client trace.");
    return text;
}

// Reads/modifies: advances timed error paging and updates native hint captions, geometry, color, and visibility.
void fang_mod_editor_update_validation(BYTE* ui) {
    const char* text;
    wchar_t caption[1024];
    unsigned int pages;
    DWORD now = GetTickCount();
    float frame_area[4], text_area[4];
    float width;
    float center;
    unsigned int index;
    (void)ui;
    text = fang_mod_editor_validation_text(validation_page, &pages);
    if (!text) return;
    if (!is_validation_hint_ready && !validation_open_hint()) return;
    if (!validation_next_page) validation_next_page = now + 10000;
    if (pages > 1 && (LONG)(now - validation_next_page) >= 0) {
        validation_page = (validation_page + 1) % pages;
        validation_next_page = now + 10000;
        text = fang_mod_editor_validation_text(validation_page, &pages);
    }
    if (!MultiByteToWideChar(CP_UTF8, MB_ERR_INVALID_CHARS, text, -1,
        caption, sizeof(caption) / sizeof(caption[0]))) return;
    ((validation_caption_fn)(*(void***)validation_hints[2].window)[32])(
        validation_hints[2].window, caption);
    /* Retain the native hint position and font. Give its existing frame room
     * for the error and its explanation, extending upward from its bottom. */
    memcpy(frame_area, validation_hints[1].area, sizeof(frame_area));
    width = validation_hints[0].area[2] - validation_hints[0].area[0] - 32.f;
    if (width > 680.f) width = 680.f;
    if (width > 100.f) {
        center = (validation_hints[0].area[2] + validation_hints[0].area[0]) * .5f;
        frame_area[0] = center - width * .5f;
        frame_area[2] = center + width * .5f;
        frame_area[1] = frame_area[3] - 112.f;
        ((void (FANG_THISCALL*)(BYTE*, const float*))(*(void***)validation_hints[1].window)[27])(
            validation_hints[1].window, frame_area);
        memcpy(text_area, validation_hints[2].area, sizeof(text_area));
        text_area[2] = text_area[0] + width - 24.f;
        text_area[3] = text_area[1] + 96.f;
        ((void (FANG_THISCALL*)(BYTE*, const float*))(*(void***)validation_hints[2].window)[27])(
            validation_hints[2].window, text_area);
    }
    for (index = 0; index < 5; ++index) {
        validation_flag(validation_hints[index].window, index < 3);
        if (index == 2)
            ((void (FANG_THISCALL*)(BYTE*, uint32_t))(*(void***)validation_hints[index].window)[23])(
                validation_hints[index].window, UINT32_MAX);
    }
}

// Reads/modifies: hit-checks wheel input, updates validation paging and timing, and refreshes native hint presentation.
int fang_mod_editor_validation_scroll(HWND window, WPARAM wparam, LPARAM lparam) {
    POINT point = {(short)LOWORD(lparam), (short)HIWORD(lparam)};
    float area[4];
    unsigned int pages;
    int delta = (short)HIWORD(wparam);
    if (!is_validation_hint_ready || !delta ||
        !fang_mod_editor_validation_text(validation_page, &pages) || pages < 2 ||
        !ScreenToClient(window, &point)) return 0;
    ((float* (__cdecl*)(float*, BYTE*))(validation_base + 0x508d70))(
        area, validation_hints[1].window);
    if (point.x < area[0] || point.y < area[1] || point.x >= area[2] || point.y >= area[3]) return 0;
    validation_page = (validation_page + (delta < 0 ? 1 : pages - 1)) % pages;
    validation_next_page = GetTickCount() + 10000;
    fang_mod_editor_update_validation(NULL);
    return 1;
}

// Reads/modifies: snapshots and logs native validation status and masks, invokes native Accept, and updates or clears the hint display.
static void FANG_THISCALL validation_on_accept(BYTE* ui) {
    BYTE* editor = fang_mod_editor_active();
    unsigned int index;
    unsigned int bit;
    if (!editor || !readable_range(ui, 92) || *(BYTE**)(ui + 88) != editor ||
        !readable_range(editor, 1237)) {
        validation_accept(ui);
        return;
    }
    fang_mod_editor_clear_validation();
    /* Observe the exact virtual status and masks used by native Accept.
     * Leave every result, callback, native dialog and save decision intact. */
    validation_status = ((validation_status_fn)(*(void***)editor)[41])(editor);
    memcpy(validation_bits, editor + 80, sizeof(validation_bits));
    ((validation_mask_fn)(validation_base + 0x322f40))(editor, validation_masks);
    validation_count = 0;
    trace_client_state("mod_editor_save_status", validation_status);
    for (index = 0; index < 4; ++index) {
        static const char* bits[] = {"mod_editor_save_bits0", "mod_editor_save_bits1",
            "mod_editor_save_bits2", "mod_editor_save_bits3"};
        static const char* masks[] = {"mod_editor_save_mask0", "mod_editor_save_mask1",
            "mod_editor_save_mask2", "mod_editor_save_mask3"};
        trace_client_state(bits[index], validation_bits[index]);
        trace_client_state(masks[index], validation_masks[index]);
        for (bit = 0; bit < 32; ++bit)
            if (validation_bits[index] & (UINT32_C(1) << bit)) ++validation_count;
    }
    validation_owner = editor;
    is_validation_visible = validation_count != 0 || validation_status == 2 || validation_status >= 5;
    validation_accept(ui);
    if (fang_mod_editor_active() != editor) fang_mod_editor_clear_validation();
    else if (is_validation_visible) fang_mod_editor_update_validation(ui);
}

// Modifies: stores the native Accept target, patches its vtable callback, and logs installation results.
void fang_mod_install_validation(BYTE* base) {
    validation_base = base;
    validation_accept = (validation_accept_fn)(base + 0x33cb30);
    if (!readable_range(base + 0xbda8f0, 4) ||
        !patch_pointer((void**)(base + 0xbda8f0), validation_accept, validation_on_accept)) {
        trace_client_state("mod_editor_validation_hook_failed", 1);
        return;
    }
    trace_client_state("mod_editor_validation_enabled", 128);
}

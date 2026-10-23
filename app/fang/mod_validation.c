#include "hook.h"

#include <stdio.h>
#include <string.h>

/* Build 103 retains 128 validation bits but its dialog selector only scans
 * 0..28. These first 28 resource identities match SporeModder-FX reg_file.txt
 * by FNV hash against Darkspore's table at 1125C28. Do not assume later bits
 * share Spore's meaning. Unknown bits always retain their numeric identity.
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
    "Creation failed to load", "Invalid limb"
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

void fang_mod_editor_clear_validation(void) {
    is_validation_visible = 0;
    validation_owner = NULL;
}

const char* fang_mod_editor_validation_text(unsigned int page, unsigned int* pages) {
    static char text[2048];
    unsigned int bit;
    unsigned int row = 0;
    size_t length;
    *pages = 0;
    if (!is_validation_visible || validation_owner != fang_mod_editor_active()) return NULL;
    *pages = (validation_count + 5) / 6;
    if (!*pages) *pages = 1;
    page %= *pages;
    length = (size_t)snprintf(text, sizeof(text),
        "MOD EDITOR - LAST SAVE VALIDATION - %u/%u\n%s (status %u)\n",
        page + 1, *pages, validation_status == 2 ? "Save blocked by validation" :
        validation_status == 5 ? "Native editor counter limit exceeded" :
        "Native editor validation details", validation_status);
    for (bit = 0; bit < 128; ++bit) {
        uint32_t flag = UINT32_C(1) << (bit % 32);
        const char* name;
        if (!(validation_bits[bit / 32] & flag)) continue;
        if (row++ / 6 != page) continue;
        name = bit < sizeof(validation_names) / sizeof(validation_names[0]) ?
            validation_names[bit] : "Unmapped native validation error";
        length += (size_t)snprintf(text + length, sizeof(text) - length,
            "[%u] %s%s\n", bit, name,
            validation_masks[bit / 32] & flag ? " [blocking]" : " [flagged]");
    }
    if (!validation_count)
        length += (size_t)snprintf(text + length, sizeof(text) - length,
            "No model error bits reported; see status above.\n");
    snprintf(text + length, sizeof(text) - length,
        "Click for next page. Right-click to dismiss.\nDetails recorded in the client trace.");
    return text;
}

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
}

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

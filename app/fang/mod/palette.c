#include "mod/mod.h"
#include "hook.h"

#include <stdint.h>

/* PlayerEditor::InitPalettes (4BDFF0) installs an equipment catalogue in the
 * same slots used by the retained general creator. Keep both owners: Equip
 * keeps its catalogue, while explicit Mod Body mode uses definition 0C.
 * No packaged property list or shared resource is changed.
 */
typedef int (__cdecl* palette_property_fn)(void*, uint32_t, uint32_t*);
typedef void (FANG_THISCALL* palette_load_fn)(BYTE*);
typedef void (__stdcall* palette_show_fn)(BYTE*, int);
typedef void (FANG_THISCALL* palette_ref_fn)(BYTE*);
typedef void (FANG_THISCALL* palette_flag_fn)(BYTE*, unsigned int, unsigned char);
typedef unsigned char (FANG_THISCALL* palette_create_fn)(BYTE*, BYTE*, BYTE**, float, float, int);

static BYTE* palette_base;
static BYTE* palette_owner;
static BYTE* palette_loading;
static BYTE* body_catalog;
static BYTE* body_panel;
static BYTE* equipment_catalog;
static BYTE* equipment_panel;
static palette_property_fn palette_property;
static int is_palette_enabled;
static int is_palette_attempted;
static int is_palette_selected;
static int is_palette_ready;
static palette_create_fn palette_native_create;
static palette_property_fn palette_layout_property;
static int is_style_enabled;
static DWORD palette_entered;
static int is_warm_failed;

int fang_mod_palette_is_loading(void) {
    return palette_loading != NULL;
}

int fang_mod_palette_is_styled(void) {
    return is_style_enabled;
}

static int __cdecl palette_select_layout(void* property, uint32_t id, uint32_t* result) {
    /* Both callers are constructing data owned by the private catalogue.
     * Keep the original category IDs, page contents and native controllers. */
    if (is_style_enabled && palette_loading && result) {
        if (id == 0xc46ec042 || id == 0x9a6aaae5) {
            /* Retain the legacy root's dynamic category controller; the
             * installed equipment root has fixed, inventory-specific tabs.
             * mod_palette_tab replaces the strip artwork with native skins.
             * No copied SPUI data or private layout resource is shipped. */
            *result = id == 0xc46ec042 ? 0xf5cbe065 : 0x89bb1330;
            trace_client_state("mod_editor_parts_native_layout", *result);
            return 1;
        }
    }
    return palette_layout_property(property, id, result);
}

static void palette_install_style(BYTE* base) {
    palette_layout_property = (palette_property_fn)(base + 0x3b47c0);
    if (!patch_call(base + 0x337d10, palette_layout_property, palette_select_layout)) {
        trace_client_state("mod_editor_parts_style_hook_failed", 1);
        return;
    }
    if (!patch_call(base + 0x34bc4c, palette_layout_property, palette_select_layout)) {
        if (!patch_call(base + 0x337d10, palette_select_layout, palette_layout_property))
            trace_client_state("mod_editor_parts_style_rollback_failed", 1);
        trace_client_state("mod_editor_parts_style_hook_failed", 2);
        return;
    }
    is_style_enabled = 1;
    trace_client_state("mod_editor_parts_style_enabled", 1);
}

static int __cdecl palette_select_property(void* property, uint32_t id, uint32_t* key) {
    if (palette_loading && property == *(void**)(palette_loading + 40) &&
        id == 0xf5cbe065 && key) {
        /* editorPartsPalette: palette_definition~!0000000C.prop.
         * ResourceKey order is instance/type/group, not DBPF index order. */
        key[0] = 12;
        key[1] = 0x00b1b104;
        key[2] = 0x406b6a00;
        return 1;
    }
    return palette_property(property, id, key);
}

static void palette_root_visible(BYTE* panel, int is_visible) {
    BYTE* window;
    if (!readable_range(panel, 116)) return;
    /* 741580 stores the dynamically loaded layout root at controller+40.
     * Hiding pages alone leaves its category tabs and frame on screen. */
    window = *(BYTE**)(panel + 40);
    if (!readable_range(window, 4) || !readable_range(*(void**)window, 128)) return;
    ((palette_flag_fn)(*(void***)window)[31])(window, 1, is_visible != 0);
}

static void palette_visible(BYTE* panel, int is_visible) {
    if (!readable_range(panel, 116)) return;
    if (is_visible && *(uintptr_t*)(panel + 56) <= *(uintptr_t*)(panel + 52)) return;
    ((palette_show_fn)(palette_base + 0x339fe0))(panel, is_visible);
    palette_root_visible(panel, is_visible);
}

void fang_mod_palette_suspend(BYTE* editor) {
    if (!palette_owner || editor != palette_owner) return;
    fang_mod_palette_drag_leave();
    if (is_palette_selected) {
        if (*(BYTE**)(editor + 988) == body_catalog) {
            *(BYTE**)(editor + 988) = equipment_catalog;
            ((palette_ref_fn)(*(void***)body_catalog)[2])(body_catalog);
        } else if (equipment_catalog) {
            ((palette_ref_fn)(*(void***)equipment_catalog)[2])(equipment_catalog);
            equipment_catalog = NULL;
        }
        if (*(BYTE**)(editor + 992) == body_panel) {
            *(BYTE**)(editor + 992) = equipment_panel;
            ((palette_ref_fn)(*(void***)body_panel)[1])(body_panel);
            /* SetMode restores category pages, not the dynamically loaded
             * root hidden by our Body swap. Restore only that root here;
             * the native transition still decides which pages are visible. */
            if (*(uint32_t*)(editor + 856) == 0) palette_visible(equipment_panel, 1);
            else palette_root_visible(equipment_panel, 1);
        } else if (equipment_panel) {
            ((palette_ref_fn)(*(void***)equipment_panel)[1])(equipment_panel);
            equipment_panel = NULL;
        }
    }
    palette_visible(body_panel, 0);
    is_palette_selected = 0;
}

void fang_mod_palette_leave(BYTE* editor) {
    if (!palette_owner || editor != palette_owner) return;
    fang_mod_palette_suspend(editor);
    fang_mod_palette_tabs_leave();
    fang_mod_palette_cards_leave(body_panel);
    is_palette_ready = 0;
    is_warm_failed = 0;
    /* Match 71D3C0: Release alone does not unregister page windows or drag
     * sources. The native controller destructor only releases its fields;
     * slot 8 tears down those registrations while the editor is still alive.
     * Otherwise hidden creature cards survive into the following scene. */
    if (body_panel) {
        ((palette_ref_fn)(*(void***)body_panel)[8])(body_panel);
        ((palette_ref_fn)(*(void***)body_panel)[1])(body_panel);
    }
    if (body_catalog) {
        ((palette_ref_fn)(palette_base + 0x337b60))(body_catalog);
        ((palette_ref_fn)(*(void***)body_catalog)[2])(body_catalog);
    }
    trace_client_state("mod_editor_parts_unloaded", 1);
    palette_owner = NULL;
    body_catalog = NULL;
    body_panel = NULL;
    equipment_catalog = NULL;
    equipment_panel = NULL;
    is_palette_attempted = 0;
    is_palette_selected = 0;
    is_palette_ready = 0;
}

static int palette_load(BYTE* editor) {
    BYTE* ui = *(BYTE**)(editor + 128);
    BYTE* host;
    DWORD started;
    DWORD prepared;
    if (!readable_range(ui, 159) || !ui[157] || ui[158] ||
        !readable_range(*(void**)ui, 76)) return 0;
    host = ((BYTE* (FANG_THISCALL*)(BYTE*, uint32_t))(*(void***)ui)[18])(ui, 0xf006f308);
    if (!host) return 0;
    is_palette_attempted = 1;
    started = GetTickCount();
    equipment_catalog = *(BYTE**)(editor + 988);
    equipment_panel = *(BYTE**)(editor + 992);
    *(BYTE**)(editor + 988) = NULL;
    *(BYTE**)(editor + 992) = NULL;
    palette_loading = editor;
    ((palette_load_fn)(palette_base + 0x3210d0))(editor);
    palette_loading = NULL;
    prepared = GetTickCount();
    trace_client_state("mod_editor_parts_catalog_native_ms", prepared - started);
    body_catalog = *(BYTE**)(editor + 988);
    body_panel = *(BYTE**)(editor + 992);
    *(BYTE**)(editor + 988) = equipment_catalog;
    *(BYTE**)(editor + 992) = equipment_panel;
    palette_visible(body_panel, 0);
    if (body_catalog && body_panel)
        is_palette_ready = fang_mod_palette_cards(palette_base, body_panel);
    if (is_palette_ready && is_style_enabled) {
        if (!fang_mod_palette_tabs(palette_base, body_panel, equipment_panel))
            trace_client_state("mod_editor_parts_tabs_style_failed", 1);
        fang_mod_palette_view(palette_base, body_panel);
    }
    trace_client_state("mod_editor_parts_catalog", body_catalog != NULL);
    trace_client_state("mod_editor_parts_panel", body_panel != NULL);
    if (readable_range(body_panel, 116))
        trace_client_state("mod_editor_parts_categories",
            (*(uintptr_t*)(body_panel + 56) - *(uintptr_t*)(body_panel + 52)) / sizeof(void*));
    if (!is_palette_ready) trace_client_state("mod_editor_parts_cards_failed", 1);
    trace_client_state("mod_editor_parts_cards_prepare_ms", GetTickCount() - prepared);
    trace_client_state("mod_editor_parts_catalog_load_ms", GetTickCount() - started);
    return is_palette_ready;
}

void fang_mod_update_palette(void) {
    BYTE* editor = fang_mod_editor_active();
    int is_selected;
    if (!is_palette_enabled || !editor) return;
    if (palette_owner != editor) {
        if (palette_owner) fang_mod_palette_leave(palette_owner);
        palette_owner = editor;
        palette_entered = GetTickCount();
    }
    is_selected = fang_mod_editor_is_body(editor);
    if (is_palette_ready && is_style_enabled && is_selected)
        fang_mod_palette_view(palette_base, body_panel);
    if (is_palette_selected && (*(BYTE**)(editor + 988) != body_catalog ||
        *(BYTE**)(editor + 992) != body_panel)) fang_mod_palette_suspend(editor);
    if (!is_palette_attempted) {
        /* Let the editor's first frame settle, then prepare the private
         * catalogue before Body is selected. Cards are deferred by the native
         * build hook, so this no longer initializes every thumbnail twice. */
        if (!is_selected && ((DWORD)(GetTickCount() - palette_entered) < 500 ||
            fang_mod_editor_is_save_busy(editor))) return;
        palette_load(editor);
        return;
    }
    if (is_palette_ready && !is_warm_failed && !*(void**)(editor + 400) &&
        !fang_mod_editor_is_save_busy(editor)) {
        DWORD started = GetTickCount();
        int result = fang_mod_palette_warm(body_panel);
        DWORD elapsed = GetTickCount() - started;
        if (result < 0) {
            is_warm_failed = 1;
            trace_client_state("mod_editor_parts_warm_failed", 1);
        }
        if (elapsed > 16) trace_client_state("mod_editor_parts_warm_slow_ms", elapsed);
    }
    if (!is_palette_ready || is_palette_selected == is_selected) return;
    /* Keep private references as well as the editor's references. A native
     * hero/catalogue refresh can replace and release the editor slots. */
    if (is_selected) {
        equipment_catalog = *(BYTE**)(editor + 988);
        equipment_panel = *(BYTE**)(editor + 992);
        palette_visible(equipment_panel, 0);
        ((palette_ref_fn)(*(void***)body_catalog)[1])(body_catalog);
        ((palette_ref_fn)(*(void***)body_panel)[0])(body_panel);
        *(BYTE**)(editor + 988) = body_catalog;
        *(BYTE**)(editor + 992) = body_panel;
    } else {
        fang_mod_palette_suspend(editor);
        trace_client_state("mod_editor_parts_visible", 0);
        return;
    }
    palette_visible(body_panel, is_selected);
    is_palette_selected = is_selected;
    trace_client_state("mod_editor_parts_visible", is_selected);
}

static unsigned char FANG_THISCALL palette_create(BYTE* editor, BYTE* entry,
    BYTE** output, float x, float y, int is_assembly) {
    if (editor == palette_owner && editor == fang_mod_editor_active() &&
        is_palette_selected && is_palette_ready && fang_mod_editor_is_body(editor) &&
        fang_mod_palette_contains(body_panel, entry)) {
        /* User-approved core-part hook: PlayerEditor::CreatePart calls the
         * base creator, then rejects parts without owned equipment records.
         * Retain the base result for this private catalogue's core parts. */
        unsigned char result = ((palette_create_fn)(palette_base + 0x325af0))(
            editor, entry, output, x, y, is_assembly);
        trace_client_state(result ? "mod_editor_parts_created" : "mod_editor_parts_create_failed",
            *(uint32_t*)(entry + 12));
        return result;
    }
    return palette_native_create(editor, entry, output, x, y, is_assembly);
}

void fang_mod_install_palette(BYTE* base) {
    if (sizeof(void*) != 4 || !readable_range(base + 0xbd997c, 4) ||
        *(void**)(base + 0xbd997c) != base + 0xbaaf0) {
        trace_client_state("mod_editor_parts_unsupported", 1);
        return;
    }
    palette_base = base;
    palette_property = (palette_property_fn)(base + 0x3b4770);
    palette_native_create = (palette_create_fn)(base + 0xbaaf0);
    if (!patch_call(base + 0x321101, palette_property, palette_select_property)) {
        trace_client_state("mod_editor_parts_hook_failed", 1);
        return;
    }
    if (!patch_pointer((void**)(base + 0xbd997c), palette_native_create, palette_create)) {
        if (!patch_call(base + 0x321101, palette_select_property, palette_property))
            trace_client_state("mod_editor_parts_hook_rollback_failed", 1);
        trace_client_state("mod_editor_parts_create_hook_failed", 1);
        return;
    }
    if (!fang_mod_install_palette_cards(base)) {
        if (!patch_pointer((void**)(base + 0xbd997c), palette_create, palette_native_create))
            trace_client_state("mod_editor_parts_create_rollback_failed", 1);
        if (!patch_call(base + 0x321101, palette_select_property, palette_property))
            trace_client_state("mod_editor_parts_hook_rollback_failed", 1);
        trace_client_state("mod_editor_parts_defer_hook_failed", 1);
        return;
    }
    is_palette_enabled = 1;
    palette_install_style(base);
    trace_client_state("mod_editor_parts_enabled", 1);
}

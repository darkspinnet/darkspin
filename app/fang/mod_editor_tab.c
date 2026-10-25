#include "hook.h"

#include <stdint.h>
#include <string.h>

typedef BYTE* (FANG_THISCALL* tab_get_fn)(BYTE*);
typedef void (FANG_THISCALL* tab_ref_fn)(BYTE*);
typedef unsigned char (FANG_THISCALL* tab_child_fn)(BYTE*, BYTE*);
typedef void (FANG_THISCALL* tab_area_fn)(BYTE*, const float*);
typedef void (FANG_THISCALL* tab_flag_fn)(BYTE*, unsigned int, unsigned char);
typedef void (FANG_THISCALL* tab_drawable_fn)(BYTE*, BYTE*);
typedef BYTE* (FANG_THISCALL* tab_object_fn)(BYTE*, uint32_t);
typedef void (FANG_THISCALL* tab_button_flag_fn)(BYTE*, unsigned int, int);

static BYTE* tab_window;
static BYTE* tab_icon;
static BYTE* tab_parent;
static BYTE* tab_ui;

void fang_mod_editor_tab_leave(void) {
    if (!tab_window) return;
    ((tab_child_fn)(*(void***)tab_parent)[55])(tab_parent, tab_window);
    ((tab_ref_fn)(*(void***)tab_window)[1])(tab_window);
    tab_window = NULL;
    tab_icon = NULL;
    tab_parent = NULL;
    tab_ui = NULL;
}

static int tab_create(BYTE* base, BYTE* ui, BYTE* source) {
    BYTE* parent;
    BYTE* allocator;
    BYTE* allocation;
    BYTE* instance;
    BYTE* window;
    BYTE* tooltip;
    BYTE* icon;
    unsigned int state;
    uint32_t icon_key[3] = {0x4bc8a7f0u, 0x2f7d0004u, 0x100d977cu};
    parent = ((tab_get_fn)(*(void***)source)[4])(source);
    if (!readable_range(parent, 4) || !readable_range(*(void**)parent, 224)) return 0;
    /* Same native WinButton construction as 90A200. The IWindow interface
     * is at +4; the IButton interface is at +488. The private drawable retains
     * installation-owned background images for its interaction states. */
    allocator = ((BYTE* (__cdecl*)(void))(base + 0x7de7d0))();
    allocation = ((BYTE* (__cdecl*)(unsigned int, unsigned int, const char*, BYTE*))
        (base + 0x7de7e0))(2148, 4, "Darkspin/BodyTab", allocator);
    if (!allocation) return 0;
    instance = ((BYTE* (FANG_THISCALL*)(BYTE*))(base + 0x7ea860))(allocation);
    if (!instance) return 0;
    window = instance + 4;
    ((tab_ref_fn)(*(void***)window)[0])(window);
    ((void (FANG_THISCALL*)(BYTE*, uint32_t))(*(void***)window)[20])(window, 0xfad0b001u);
    ((void (FANG_THISCALL*)(BYTE*, uint32_t))(*(void***)window)[21])(window, 0xfad0b001u);
    if (!fang_mod_editor_button_skin(base, window, source)) {
        ((tab_ref_fn)(*(void***)window)[1])(window);
        return 0;
    }
    {
        BYTE* button = ((tab_object_fn)(*(void***)window)[3])(window, 0x8ed27e7au);
        /* WinButton defaults to push type 1, which ignores persistent checked
         * artwork. Native editor tabs use radio type 3 (IButton::SetType). */
        if (!readable_range(button, 4) || !readable_range(*(void**)button, 44)) {
            ((tab_ref_fn)(*(void***)window)[1])(window);
            return 0;
        }
        ((void (FANG_THISCALL*)(BYTE*, unsigned int))(*(void***)button)[7])(button, 3);
    }
    allocation = ((BYTE* (__cdecl*)(unsigned int, unsigned int, const char*, BYTE*))
        (base + 0x7de7e0))(104, 4, "Darkspin/BodyTooltip", allocator);
    if (!allocation) {
        ((tab_ref_fn)(*(void***)window)[1])(window);
        return 0;
    }
    tooltip = ((BYTE* (FANG_THISCALL*)(BYTE*))(base + 0x51ce20))(allocation);
    ((tab_ref_fn)(*(void***)tooltip)[0])(tooltip);
    ((void (FANG_THISCALL*)(BYTE*, const wchar_t*, int, unsigned char))
        (base + 0x51d030))(tooltip, L"BODY", -1, 0);
    ((tab_drawable_fn)(*(void***)window)[65])(window, tooltip);
    ((tab_ref_fn)(*(void***)tooltip)[1])(tooltip);
    if (!((tab_child_fn)(*(void***)parent)[54])(parent, window)) {
        /* AddWindow may attach before rejecting validation. Remove only if
         * it actually retained this parent, then release our own reference. */
        if (((tab_get_fn)(*(void***)window)[4])(window) == parent)
            ((tab_child_fn)(*(void***)parent)[55])(parent, window);
        ((tab_ref_fn)(*(void***)window)[1])(window);
        return 0;
    }
    allocation = ((BYTE* (__cdecl*)(unsigned int, unsigned int, const char*, BYTE*))
        (base + 0x7de7e0))(2148, 4, "Darkspin/BodyIcon", allocator);
    if (!allocation) goto icon_failed;
    instance = ((BYTE* (FANG_THISCALL*)(BYTE*))(base + 0x7ea860))(allocation);
    if (!instance) goto icon_failed;
    icon = instance + 4;
    {
        const float icon_area[4] = {0, 0, 32, 32};
        ((tab_area_fn)(*(void***)icon)[27])(icon, icon_area);
    }
    ((tab_ref_fn)(*(void***)icon)[0])(icon);
    for (state = 0; state < 8; ++state) {
        if (!((unsigned char (__cdecl*)(BYTE*, uint32_t*, unsigned int))
            (base + 0x50a360))(icon, icon_key, state)) break;
    }
    if (state != 8 || !((tab_child_fn)(*(void***)window)[54])(window, icon)) {
        if (((tab_get_fn)(*(void***)icon)[4])(icon) == window)
            ((tab_child_fn)(*(void***)window)[55])(window, icon);
        ((tab_ref_fn)(*(void***)icon)[1])(icon);
        goto icon_failed;
    }
    /* The parent handles clicks and owns the child's retained reference. */
    ((tab_flag_fn)(*(void***)icon)[31])(icon, 2, 0);
    ((tab_ref_fn)(*(void***)icon)[1])(icon);
    tab_icon = icon;
    tab_window = window;
    tab_parent = parent;
    tab_ui = ui;
    trace_client_state("mod_editor_body_native_tab", 1);
    return 1;
icon_failed:
    ((tab_child_fn)(*(void***)parent)[55])(parent, window);
    ((tab_ref_fn)(*(void***)window)[1])(window);
    trace_client_state("mod_editor_body_icon_failed", 1);
    return 0;
}

int fang_mod_editor_tab_update(BYTE* base, BYTE* ui, int is_selected, int is_enabled) {
    BYTE* source;
    BYTE* button;
    const float* source_area;
    const float* current_area;
    float area[4];
    if (tab_ui && tab_ui != ui) fang_mod_editor_tab_leave();
    source = ((BYTE* (FANG_THISCALL*)(BYTE*, uint32_t))(*(void***)ui)[18])(ui, 0xf019c2e7);
    if (!readable_range(source, 4) || !readable_range(*(void**)source, 264)) return 0;
    if (!tab_window && !tab_create(base, ui, source)) return 0;
    source_area = ((const float* (FANG_THISCALL*)(BYTE*))(*(void***)source)[14])(source);
    if (!readable_range(source_area, sizeof(area))) return 0;
    area[2] = source_area[0] - 2;
    area[0] = area[2] - (source_area[2] - source_area[0]);
    area[1] = source_area[1];
    area[3] = source_area[3];
    current_area = ((const float* (FANG_THISCALL*)(BYTE*))(*(void***)tab_window)[14])(tab_window);
    if (!readable_range(current_area, sizeof(area)) || memcmp(current_area, area, sizeof(area)))
        ((tab_area_fn)(*(void***)tab_window)[27])(tab_window, area);
    {
        float icon_area[4];
        float width = area[2] - area[0];
        float height = area[3] - area[1];
        float size = height < 32.f ? height : 32.f;
        icon_area[0] = (width - size) * .5f;
        icon_area[1] = (height - size) * .5f;
        icon_area[2] = icon_area[0] + size;
        icon_area[3] = icon_area[1] + size;
        current_area = ((const float* (FANG_THISCALL*)(BYTE*))(*(void***)tab_icon)[14])(tab_icon);
        if (!readable_range(current_area, sizeof(icon_area)) || memcmp(current_area, icon_area, sizeof(icon_area)))
            ((tab_area_fn)(*(void***)tab_icon)[27])(tab_icon, icon_area);
        ((tab_flag_fn)(*(void***)tab_icon)[31])(tab_icon, 1, 1);
    }
    ((tab_flag_fn)(*(void***)tab_window)[31])(tab_window, 1, 1);
    ((tab_flag_fn)(*(void***)tab_window)[31])(tab_window, 2, is_enabled != 0);
    button = ((tab_object_fn)(*(void***)tab_window)[3])(tab_window, 0x8ed27e7a);
    if (readable_range(button, 4) && readable_range(*(void**)button, 44)) {
        ((tab_button_flag_fn)(*(void***)button)[10])(button, 4, is_selected);
        ((tab_ref_fn)(*(void***)tab_window)[37])(tab_window);
    }
    return 1;
}

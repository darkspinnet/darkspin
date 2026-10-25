#include "hook.h"

#include <string.h>

typedef int (FANG_THISCALL* drag_update_fn)(BYTE*, unsigned int);
typedef BYTE* (FANG_THISCALL* drag_drawable_fn)(BYTE*);
typedef void (FANG_THISCALL* drag_ref_fn)(BYTE*);
typedef void (FANG_THISCALL* drag_set_fn)(BYTE*, BYTE*);

static BYTE* drag_base;
static drag_update_fn drag_native_update;
static void* drag_table[42];
static struct {
    BYTE* window;
    BYTE* drawable;
} drag_thumbnail;

void fang_mod_palette_drag_leave(void) {
    BYTE* window = drag_thumbnail.window;
    BYTE* drawable = drag_thumbnail.drawable;
    if (!window) return;
    drag_thumbnail.window = NULL;
    drag_thumbnail.drawable = NULL;
    /* A native replacement takes precedence over our retained thumbnail. */
    if (!((drag_drawable_fn)(*(void***)window)[42])(window))
        ((drag_set_fn)(*(void***)window)[44])(window, drawable);
    ((drag_ref_fn)(*(void***)drawable)[1])(drawable);
    ((drag_ref_fn)(*(void***)window)[1])(window);
    trace_client_state("mod_editor_parts_drag_thumbnail", 0);
}

static void drag_hide_thumbnail(BYTE* window) {
    BYTE* drawable;
    if (!readable_range(window, 4) || !readable_range(*(void**)window, 180)) return;
    drawable = ((drag_drawable_fn)(*(void***)window)[42])(window);
    if (!drawable || !readable_range(drawable, 4) || !readable_range(*(void**)drawable, 8)) return;
    if (drag_thumbnail.window != window) {
        fang_mod_palette_drag_leave();
        ((drag_ref_fn)(*(void***)window)[0])(window);
        drag_thumbnail.window = window;
        trace_client_state("mod_editor_parts_drag_thumbnail", 1);
    }
    ((drag_ref_fn)(*(void***)drawable)[0])(drawable);
    if (drag_thumbnail.drawable)
        ((drag_ref_fn)(*(void***)drag_thumbnail.drawable)[1])(drag_thumbnail.drawable);
    drag_thumbnail.drawable = drawable;
    /* Keep the native window, capture, position and drag callbacks alive.
     * Only its 2D artwork is absent while the editor places the real part. */
    ((drag_set_fn)(*(void***)window)[44])(window, NULL);
}

static int FANG_THISCALL drag_update(BYTE* drag, unsigned int elapsed) {
    int result = drag_native_update(drag, elapsed);
    BYTE* editor = fang_mod_editor_active();
    BYTE* window = *(BYTE**)(drag + 88);
    /* 743650 sets +388 only after native drag admission. Mouse-down alone
     * must not hide the palette thumbnail or change input eligibility. */
    if (drag[388] && editor && fang_mod_editor_is_body(editor)) {
        drag_hide_thumbnail(window);
    } else if (drag_thumbnail.window == window) {
        fang_mod_palette_drag_leave();
    }
    return result;
}

void fang_mod_palette_drag_card(BYTE* base, BYTE* card) {
    BYTE* drag;
    if (!readable_range(card, 28)) return;
    drag = *(BYTE**)(card + 20);
    if (!readable_range(drag, 424) || *(void**)drag != base + 0xbfb160 ||
        *(BYTE**)(drag + 88) != *(BYTE**)(card + 24)) return;
    if (!drag_base) {
        /* Preserve RTTI and every native virtual. Only private Body-card
         * instances use this table; the equipment factory stays untouched. */
        if (!readable_range(base + 0xbfb15c, sizeof(drag_table)) ||
            *(void**)(base + 0xbfb180) != base + 0x346f20) return;
        drag_base = base;
        drag_native_update = (drag_update_fn)(base + 0x346f20);
        memcpy(drag_table, base + 0xbfb15c, sizeof(drag_table));
        drag_table[9] = drag_update;
    }
    if (drag_base != base) return;
    *(void***)drag = drag_table + 1;
}

#include "hook.h"

#include <stdint.h>
#include <string.h>

/* Retain installed background images in a private drawable. Composite editor
 * skins keep their glyph in a separate layer, which must not be copied. */
int fang_mod_editor_button_skin(BYTE* base, BYTE* window, BYTE* donor) {
    BYTE* drawable = ((BYTE* (FANG_THISCALL*)(BYTE*))(*(void***)donor)[42])(donor);
    BYTE* composite;
    BYTE* own;
    unsigned int state;
    if (!readable_range(drawable, 4) || !readable_range(*(void**)drawable, 16)) return 0;
    composite = ((BYTE* (FANG_THISCALL*)(BYTE*, uint32_t))
        (*(void***)drawable)[3])(drawable, 87995686u);
    if (!readable_range(composite, 352)) return 0;
    for (state = 0; state < 8; ++state) {
        BYTE* detail = *(BYTE**)(composite + 276 + state * 4);
        BYTE* image;
        if (!detail) detail = composite + 124;
        if (!readable_range(detail, 20)) return 0;
        image = *(BYTE**)(detail + 12);
        if (!image) image = *(BYTE**)(composite + 136);
        if (!readable_range(image, 4)) return 0;
        /* 9093D0 creates a private StdDrawable and retains each UIImage. */
        if (!((unsigned char (__cdecl*)(BYTE*, BYTE*, unsigned int))
            (base + 0x5093d0))(window, image, state)) return 0;
    }
    own = ((BYTE* (FANG_THISCALL*)(BYTE*))(*(void***)window)[42])(window);
    if (!readable_range(own, 76)) return 0;
    memcpy(own + 48, composite + 48, 28);
    return 1;
}

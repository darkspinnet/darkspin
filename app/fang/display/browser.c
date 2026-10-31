#include "display/display.h"

#include <string.h>

/* Build 103, EAWebKit 1.21.00.darkspore:
 * WebLayer+24 owns cWebView; cWebView+224 owns EA::WebKit::View.
 * EA View::GetWebView (+300) is 21AAC0, returning the pointer at +8.
 * WebView::setPageSizeMultiplier (+264) is 2D3B70. It calls
 * Frame::setZoomFactor(multiplier, false), including child frames.
 * Resize the backing surface and use native page zoom instead of stretching
 * its finished texture. Stock browser rendering and mouse conversion then
 * share physical pixels. Installed assets and Flash stay intact. */
typedef void (FANG_THISCALL* browser_bounds_fn)(BYTE*, int, int, int, int);
typedef void (FANG_THISCALL* browser_zoom_fn)(BYTE*, float);
typedef unsigned char (FANG_THISCALL* browser_script_fn)(BYTE*, const char*);

static BYTE* browser_image;
static BYTE* webkit_image;
static BYTE* reported_view;
static int reported_percent = 100;
static int is_webkit_rejected;
static DWORD next_background_update;

/* Build 103's profile border and Social search strip use repeating backgrounds.
 * This WebKit's full-page zoom tiles them at their old intrinsic size, leaving
 * repeated borders and a horizontal search-field seam. Size these live
 * backgrounds explicitly; restore their inline styles at 100%. */
static void update_html_backgrounds(BYTE* browser, int percent) {
    static const char scaled_script[] =
        "(function visit(w){try{var d=w.document,ids=['Img_Frame',"
        "'Search_Container_Left','Search_Container_Middle','Search_Container_Right'];"
        "for(var j=0;j<ids.length;j++){var e=d.getElementById(ids[j]);"
        "if(e){var s=e.style;if(!e.__fangZoomBackground)e.__fangZoomBackground="
        "{size:s.webkitBackgroundSize,repeat:s.backgroundRepeat};"
        "if(s.webkitBackgroundSize!='100% 100%')s.webkitBackgroundSize='100% 100%';"
        "if(s.backgroundRepeat!='no-repeat')s.backgroundRepeat='no-repeat';}}"
        "for(var i=0;i<w.frames.length;i++)visit(w.frames[i]);}catch(e){}})(window)";
    static const char original_script[] =
        "(function visit(w){try{var d=w.document,ids=['Img_Frame',"
        "'Search_Container_Left','Search_Container_Middle','Search_Container_Right'];"
        "for(var j=0;j<ids.length;j++){var e=d.getElementById(ids[j]);"
        "if(e&&e.__fangZoomBackground){var p=e.__fangZoomBackground;"
        "e.style.webkitBackgroundSize=p.size;e.style.backgroundRepeat=p.repeat;"
        "delete e.__fangZoomBackground;}}"
        "for(var i=0;i<w.frames.length;i++)visit(w.frames[i]);}catch(e){}})(window)";
    DWORD now = GetTickCount();
    if (next_background_update && (LONG)(now - next_background_update) < 0) return;
    next_background_update = now + 500;
    ((browser_script_fn)(browser_image + 0x9ED90))(browser,
        percent == 100 ? original_script : scaled_script);
}

// Reads: verifies the installed build-103 DLL's page zoom entry and ABI.
static int browser_zoom_available(void) {
    static const BYTE zoom_signature[] = {
        0x8B, 0x41, 0x08, 0xD9, 0x44, 0x24, 0x04, 0x50,
        0xD9, 0x59, 0x48, 0xC6, 0x41, 0x4C, 0x00, 0xE8,
        0xBC, 0x8D, 0xFF, 0xFF, 0x83, 0xC4, 0x04,
    };
    static const BYTE getter_signature[] = {0x8B, 0x41, 0x08, 0xC3};
    BYTE* module;
    if (webkit_image) return 1;
    if (is_webkit_rejected) return 0;
    module = (BYTE*)GetModuleHandleW(L"EAWebkit.dll");
    if (!module) return 0;
    if (!readable_range(module + 0x2D3B70, sizeof(zoom_signature)) ||
        memcmp(module + 0x2D3B70, zoom_signature, sizeof(zoom_signature)) ||
        !readable_range(module + 0x21AAC0, sizeof(getter_signature)) ||
        memcmp(module + 0x21AAC0, getter_signature, sizeof(getter_signature))) {
        is_webkit_rejected = 1;
        trace_client_state("display_html_zoom_binding_failed", 1);
        return 0;
    }
    webkit_image = module;
    return 1;
}

// Modifies: renders the main browser at its selected physical size and page zoom.
void fang_update_html_ui_scale(void) {
    BYTE* client;
    BYTE* browser;
    BYTE* view;
    BYTE* engine_view;
    BYTE* page;
    void** methods;
    HWND window;
    RECT area;
    int percent = fang_native_ui_scale();
    int width = 870 * percent / 100;
    int height = 620 * percent / 100;
    float factor = percent / 100.0f;
    int x, y;
    if (!browser_image || !browser_zoom_available()) return;
    client = *(BYTE**)(browser_image + 0x1038EF0);
    if (!readable_range(client, 0x19180)) return;
    browser = *(BYTE**)(client + 0x1917C);
    if (!readable_range(browser, 28) || *(BYTE**)browser != browser_image + 0xBD76C8) return;
    view = *(BYTE**)(browser + 24);
    if (!readable_range(view, 228) || *(BYTE**)view != browser_image + 0xC734A0 || view[96]) return;
    engine_view = *(BYTE**)(view + 224);
    if (!readable_range(engine_view, 12) || *(BYTE**)engine_view != webkit_image + 0x331390) return;
    methods = *(void***)engine_view;
    if (!readable_range(methods, 304) || methods[75] != webkit_image + 0x21AAC0) return;
    page = *(BYTE**)(engine_view + 8);
    if (!readable_range(page, 77) || *(BYTE**)page != webkit_image + 0x39FE08) return;
    methods = *(void***)page;
    if (!readable_range(methods, 268) || methods[66] != webkit_image + 0x2D3B70) return;
    if (percent == 100 && reported_percent == 100 && *(float*)(page + 72) == factor && !page[76]) {
        update_html_backgrounds(browser, percent);
        return;
    }
    window = GetActiveWindow();
    if (!window || !GetClientRect(window, &area)) return;
    x = (area.right - area.left - width) / 2;
    y = (area.bottom - area.top - height) / 2;
    if (x < 0) x = 0;
    if (y < 0) y = 0;
    if (*(int*)(view + 64) != width || *(int*)(view + 68) != height ||
        *(float*)(view + 88) != x || *(float*)(view + 92) != y) {
        ((browser_bounds_fn)(browser_image + 0x9EF20))(browser, x, y, width, height);
    }
    if (*(float*)(page + 72) != factor || page[76]) {
        ((browser_zoom_fn)methods[66])(page, factor);
    }
    update_html_backgrounds(browser, percent);
    if (reported_view != page || reported_percent != percent) {
        reported_view = page;
        reported_percent = percent;
        trace_client_state("display_html_ui_page_zoom", percent);
        trace_client_state("display_html_ui_width", width);
        trace_client_state("display_html_ui_height", height);
    }
}

// Modifies: binds the byte-confirmed build-103 browser owner and bounds setter.
int fang_install_html_ui_scale(BYTE* base) {
    static const BYTE getter_tail[] = {0x8B, 0x80, 0x7C, 0x91, 0x01, 0x00, 0xC3};
    if (!readable_range(base + 0xEFD90, 12) || base[0xEFD90] != 0xA1 ||
        *(BYTE**)(base + 0xEFD91) != base + 0x1038EF0 ||
        memcmp(base + 0xEFD95, getter_tail, sizeof(getter_tail)) ||
        !readable_range(base + 0xBD76EC, 4) ||
        *(BYTE**)(base + 0xBD76EC) != base + 0x9EF20 ||
        !readable_range(base + 0xBD770C, 4) ||
        *(BYTE**)(base + 0xBD770C) != base + 0x9ED90) return 0;
    browser_image = base;
    return 1;
}

#include "hook.h"
#include "scenario/control.h"
#if FANG_SCENARIO
#include <string.h>

/* Build 103 ordinary overlay dispatcher, not ship room IDs. Evidence:
 * scenario-screens-20261024/{overlay-dispatch,inventory-lifecycle}.asm.
 * Factory4307A0 is cdecl() -> inventory; GameUI stores it at +1720.
 * Reset426427 destroys that inventory before GameUI/browser teardown. */
typedef BYTE* (__cdecl* screen_create_fn)(void);
typedef void (__cdecl* screen_destroy_fn)(BYTE*);
typedef void (__cdecl* screen_set_fn)(int, uint32_t, uint32_t);
typedef void (__cdecl* screen_toggle_fn)(void);
typedef int (__cdecl* screen_query_fn)(void);
typedef unsigned char (__cdecl* screen_visible_fn)(BYTE*);
typedef BYTE* (__cdecl* screen_browser_fn)(void);
typedef unsigned char (FANG_THISCALL* screen_browser_visible_fn)(BYTE*);
/* Native4290D0 initializes/receives this 36-byte browser variant, checks
 * type+28==0x4003, then uses flags+30 bit0x10 for pointer/inline UTF16 at+16.
 * Its425330(this) destructor owns any returned allocation. */
typedef struct screen_variant {
    uint32_t headers[4];
    union { wchar_t chars[6]; const wchar_t* string; } payload;
    uint16_t type;
    uint8_t flags, reserved;
    uint32_t tail;
} screen_variant;
typedef char screen_variant_size[sizeof(screen_variant) == 36 ? 1 : -1];
typedef unsigned char (FANG_THISCALL* screen_browser_query_fn)(BYTE*, const char*, screen_variant*);
typedef void (FANG_THISCALL* screen_variant_release_fn)(screen_variant*);

static CRITICAL_SECTION screen_lock;
static volatile LONG is_screen_lock_initialized;
static int is_screen_installed;
static BYTE* screen_image;
static BYTE* observed_gameui;
static BYTE* observed_inventory;
static DWORD observed_thread;
static uint64_t screen_generation;
static screen_create_fn native_screen_create;
static screen_destroy_fn native_screen_destroy;
static screen_set_fn native_screen_set;
static screen_toggle_fn native_inventory_toggle;
static screen_query_fn native_screen_query, native_modal_query;
static screen_visible_fn native_inventory_visible;
static screen_browser_fn native_screen_browser;
static screen_variant_release_fn native_screen_variant_release;

static BYTE* __cdecl observe_screen_create(void) {
    BYTE* inventory = native_screen_create();
    DWORD last_error = GetLastError();
    EnterCriticalSection(&screen_lock);
    observed_gameui = *(BYTE**)(screen_image + 0xDBEC40);
    observed_inventory = inventory;
    observed_thread = GetCurrentThreadId();
    ++screen_generation;
    LeaveCriticalSection(&screen_lock);
    SetLastError(last_error);
    return inventory;
}

static void __cdecl observe_screen_destroy(BYTE* inventory) {
    DWORD last_error = GetLastError();
    EnterCriticalSection(&screen_lock);
    if (inventory == observed_inventory) {
        observed_gameui = NULL;
        observed_inventory = NULL;
        observed_thread = 0;
        ++screen_generation;
    }
    /* Foreign teardown waits until a read/invocation leaves this lock. The
     * observation is already invalid before native destruction can reenter. */
    SetLastError(last_error);
    native_screen_destroy(inventory);
    last_error = GetLastError();
    LeaveCriticalSection(&screen_lock);
    SetLastError(last_error);
}

int fang_control_screen_install(HMODULE executable) {
    BYTE* base = (BYTE*)executable;
    static const BYTE create_call[] = {0xE8,0xAC,0x80,0x00,0x00};
    static const BYTE destroy_call[] = {0xE8,0xD4,0xF1,0x00,0x00};
    static const BYTE dispatch_entry[] = {0x6A,0xFF,0x68,0x98,0x9F,0xE9,0x00};
    static const BYTE query_entry[] = {0x6A,0xFF,0x68,0xE8,0x0D,0xE9,0x00};
    static const BYTE toggle_entry[] = {0xA1,0x40,0xEC,0x1B,0x01,0x85,0xC0};
    static const BYTE modal_entry[] = {0x53,0x56,0x32,0xDB,0xE8,0xF7,0xA8,0x0C,0x00};
    static const BYTE browser_entry[] = {0xA1,0xF0,0x8E,0x43,0x01,0x8B,0x80,0x7C,0x91,0x01,0x00,0xC3};
    static const BYTE inventory_entry[] = {0x8B,0x4C,0x24,0x04};
    static const BYTE variant_release_entry[] = {0x6A,0xFF,0x68,0x08,0x0B,0xE9,0x00};
    if (!base || sizeof(void*) != 4 || memcmp(base + 0x286EF, create_call, sizeof(create_call)) ||
        memcmp(base + 0x26427, destroy_call, sizeof(destroy_call)) ||
        memcmp(base + 0x29620, dispatch_entry, sizeof(dispatch_entry)) ||
        memcmp(base + 0x290D0, query_entry, sizeof(query_entry)) ||
        memcmp(base + 0x2AE90, toggle_entry, sizeof(toggle_entry)) ||
        memcmp(base + 0x25490, modal_entry, sizeof(modal_entry)) ||
        memcmp(base + 0xEFD90, browser_entry, sizeof(browser_entry)) ||
        memcmp(base + 0x2F270, inventory_entry, sizeof(inventory_entry)) ||
        memcmp(base + 0x25330, variant_release_entry, sizeof(variant_release_entry)) ||
        *(void**)(base + 0xBD0B18) != base + 0x2F1C0) return 0;
    if (InterlockedCompareExchange(&is_screen_lock_initialized, 0, 0)) return 0;
    if (!InitializeCriticalSectionAndSpinCount(&screen_lock, 4000)) return 0;
    InterlockedExchange(&is_screen_lock_initialized, 1);
    screen_image = base;
    native_screen_create = (screen_create_fn)(base + 0x307A0);
    native_screen_destroy = (screen_destroy_fn)(base + 0x35600);
    native_screen_set = (screen_set_fn)(base + 0x29620);
    native_inventory_toggle = (screen_toggle_fn)(base + 0x2AE90);
    native_screen_query = (screen_query_fn)(base + 0x290D0);
    native_modal_query = (screen_query_fn)(base + 0x25490);
    native_inventory_visible = (screen_visible_fn)(base + 0x2F270);
    native_screen_browser = (screen_browser_fn)(base + 0xEFD90);
    native_screen_variant_release = (screen_variant_release_fn)(base + 0x25330);
    if (!patch_call(base + 0x26427, native_screen_destroy, observe_screen_destroy)) return 0;
    if (!patch_call(base + 0x286EF, native_screen_create, observe_screen_create)) {
        if (!patch_call(base + 0x26427, observe_screen_destroy, native_screen_destroy))
            trace_client_state("scenario_screen_rollback_failed", 1);
        return 0;
    }
    EnterCriticalSection(&screen_lock);
    is_screen_installed = 1;
    LeaveCriticalSection(&screen_lock);
    return 1;
}

static int is_screen_current(uint64_t generation) {
    return is_screen_installed && observed_gameui && observed_inventory &&
        generation == screen_generation && GetCurrentThreadId() == observed_thread &&
        *(BYTE**)(screen_image + 0xDBEC40) == observed_gameui &&
        readable_range(observed_gameui, 1832) &&
        *(BYTE**)(observed_gameui + 1720) == observed_inventory &&
        readable_range(observed_inventory, 1404) &&
        *(void**)observed_inventory == screen_image + 0xBD0B18;
}

static int is_other_screen_open(void) {
    /* The other ordinary dispatcher-owned overlays must not become 'none'.
     * These exact constructors/vtables pair with their pure bool getters:
     * Results43E650, Settings/Options/Rewards4E48F0, Arena4045C0. */
    static const uint32_t offsets[] = {1736, 1740, 1744, 1748, 1752};
    static const uint32_t tables[] = {0xBD16FC, 0xBD14A4, 0xBD1A10, 0xBD1DE0, 0xBCD684};
    static const uint32_t flags[] = {304, 40, 40, 40, 1320};
    uint32_t index;
    for (index = 0; index < 5; ++index) {
        BYTE* overlay = *(BYTE**)(observed_gameui + offsets[index]);
        if (!overlay) continue;
        if (!readable_range(overlay, flags[index] + 1) ||
            *(void**)overlay != screen_image + tables[index] || overlay[flags[index]]) return 1;
    }
    return 0;
}

static int is_screen_modal_blocked(void) {
    BYTE* dialog = *(BYTE**)(observed_gameui + 1724);
    /* The same explicit modal counter/dialog-active byte read by425490.
     * Do not dismiss a save/confirmation dialog through the screen setter. */
    return *(int32_t*)(observed_gameui + 1828) > 0 ||
        (dialog && (!readable_range(dialog, 42) || dialog[41]));
}

static int is_screen_request_current(uint32_t request_id, uint64_t generation, uint32_t scene, uint64_t deadline) {
    fang_control_snapshot boundary;
    if (GetTickCount64() >= deadline || !is_screen_current(generation) ||
        !fang_control_window_screen_current(request_id)) return 0;
    fang_control_read(&boundary);
    return boundary.scene_load_return_count == scene;
}

static uint32_t read_browser_screen(BYTE* browser, uint32_t request_id, uint64_t generation, uint32_t scene, uint64_t deadline) {
    /* The authored root showPlayerProfile/showLeaderBoards/showWiki functions
     * display FRAME3 and select its tab but never update currentactive. Read
     * those actual DOM producers; do not modify the page or trust a request.
     * Labels are one UTF16 character, and unknown/absent state stays unknown. */
    static const char expression[] =
        "(function(){try{var d=document,f=d.getElementById('FRAME2');"
        "if(f&&f.style.display=='block')return'F';"
        "f=d.getElementById('FRAME3');if(!f||f.style.display!='block')return'?';"
        "var w=window.frames['FRAME3'],h=w&&w.document,"
        "p=h&&h.getElementById('Tab_1'),l=h&&h.getElementById('Tab_3'),m=h&&h.getElementById('Tab_5');"
        "if(l&&l.className=='tab_bar_selected_middle')return'L';"
        "if(m&&m.className=='tab_bar_selected_middle')return'M';"
        "if(p&&p.className=='tab_bar_selected_middle')return'P';"
        "}catch(e){}return'?';})()";
    screen_variant result;
    const wchar_t* label;
    uint32_t screen = 6;
    void** table = *(void***)browser;
    memset(&result, 0, sizeof(result));
    if (!readable_range(table, 80) || !table[19] ||
        !is_screen_request_current(request_id, generation, scene, deadline)) return 6;
    if (((screen_browser_query_fn)table[19])(browser, expression, &result) && result.type == 0x4003 &&
        is_screen_request_current(request_id, generation, scene, deadline)) {
        label = (result.flags & 0x10) ? result.payload.string : result.payload.chars;
        if (readable_range(label, 2 * sizeof(wchar_t)) && label[1] == 0) {
            switch (label[0]) {
            case L'P': screen = 1; break;
            case L'F': screen = 3; break;
            case L'L': screen = 4; break;
            case L'M': screen = 5; break;
            }
        }
    }
    native_screen_variant_release(&result);
    return screen;
}

static int read_screen_locked(fang_control_screen_state* snapshot, uint32_t request_id, uint32_t scene, uint64_t deadline) {
    BYTE* browser;
    void** table;
    int native_id;
    uint64_t generation = screen_generation;
    BYTE* galaxy;
    memset(snapshot, 0, sizeof(*snapshot));
    snapshot->screen = 6;
    if (!is_screen_current(generation)) return 0;
    snapshot->is_inventory_open = native_inventory_visible(observed_inventory) != 0;
    browser = native_screen_browser();
    if (!is_screen_current(generation) || !readable_range(browser, 4)) return 0;
    table = *(void***)browser;
    if (!readable_range(table, 52) || !table[12]) return 0;
    snapshot->is_browser_open = ((screen_browser_visible_fn)table[12])(browser) != 0;
    if (!is_screen_current(generation)) return 0;
    galaxy = *(BYTE**)(observed_gameui + 1756);
    snapshot->screen = 0; /* none; unknown is deliberately distinct. */
    if ((galaxy && (!readable_range(galaxy, 41) ||
            *(void**)galaxy != screen_image + 0xBDBB7C)) || is_screen_modal_blocked()) {
        snapshot->screen = 6;
    } else if (snapshot->is_inventory_open) {
        snapshot->screen = 2;
        snapshot->native_screen_id = 2;
        snapshot->fields = 1;
    } else if (snapshot->is_browser_open) {
        BYTE* host = *(BYTE**)(observed_gameui + 1784);
        if (!readable_range(host, 13)) return 0;
        if (!is_screen_request_current(request_id, generation, scene, deadline)) return 0;
        native_id = native_screen_query();
        if (!is_screen_request_current(request_id, generation, scene, deadline)) return 0;
        snapshot->native_screen_id = native_id;
        snapshot->fields = 1;
        snapshot->screen = read_browser_screen(browser, request_id, generation, scene, deadline);
        if (!is_screen_request_current(request_id, generation, scene, deadline)) return 0;
        if (snapshot->screen != 6) snapshot->fields |= 2;
    } else if (galaxy && galaxy[40]) {
        /* cGalaxyMapUI exact vtable/IsVisible producer4E48F0. The visible
         * movie can still be an empty backdrop; this says nothing of data. */
        snapshot->screen = 7;
        snapshot->native_screen_id = 19;
        snapshot->fields = 1;
    } else if (native_modal_query() || is_other_screen_open()) {
        snapshot->screen = 6; /* a different modal/overlay is active */
    }
    if (!is_screen_current(generation)) return 0;
    snapshot->is_available = 1;
    snapshot->owner_generation = generation;
    return 1;
}

uint32_t fang_control_screen(uint32_t request_id, uint32_t screen, int is_action, uint64_t expected_owner_generation, uint32_t scene, uint64_t deadline,
    fang_control_screen_state* snapshot) {
    uint32_t status = 5;
    uint64_t generation;
    int native_id;
    int is_attempted = 0, is_returned = 0;
    fang_control_snapshot boundary;
    if (!snapshot) return 5;
    memset(snapshot, 0, sizeof(*snapshot));
    snapshot->screen = 6;
    if (!InterlockedCompareExchange(&is_screen_lock_initialized, 0, 0)) return is_action ? 5 : 1;
    EnterCriticalSection(&screen_lock);
    if (!fang_control_window_screen_current(request_id)) { status = 3; goto done; }
    if (GetTickCount64() >= deadline) { status = 4; goto done; }
    fang_control_read(&boundary);
    if (boundary.scene_load_return_count != scene) goto done;
    if (!read_screen_locked(snapshot, request_id, scene, deadline)) { status = is_action ? 5 : 1; goto done; }
    if (!is_action) { status = 1; goto done; }
    if (!expected_owner_generation || screen_generation != expected_owner_generation) goto done;
    if ((screen > 5 && screen != 7) || snapshot->screen == 6 || snapshot->screen == screen) goto done;
    /* Opening another screen must never discard inventory close restrictions.
     * Explicit Close uses its ordinary toggle, including all native gates. */
    if (snapshot->is_inventory_open && screen != 0) goto done;
    generation = screen_generation;
    if (GetTickCount64() >= deadline) { status = 4; goto done; }
    fang_control_read(&boundary);
    if (!is_screen_current(generation) || boundary.scene_load_return_count != scene) goto done;
    if (is_screen_modal_blocked()) goto done;
    if (!fang_control_window_screen_current(request_id)) { status = 3; goto done; }
    is_attempted = 1;
    if (snapshot->is_inventory_open || screen == 2) {
        native_inventory_toggle();
    } else {
        native_id = screen == 0 ? -1 : screen == 1 ? 6 : screen == 3 ? 0 : screen == 4 ? 10 : screen == 7 ? 19 : 11;
        native_screen_set(native_id, 0, 0);
    }
    is_returned = 1;
    /* The dispatcher returns void. This receipt proves invocation return,
     * never rendering/network completion. A fresh query establishes selection. */
    if (!is_screen_current(generation) || !read_screen_locked(snapshot, request_id, scene, deadline)) {
        snapshot->is_available = 0;
        status = 6;
        goto done;
    }
    status = 1;
done:
    snapshot->is_invocation_attempted = is_attempted;
    snapshot->is_invocation_returned = is_returned;
    LeaveCriticalSection(&screen_lock);
    return status;
}
#endif

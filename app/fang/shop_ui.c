#include "hook.h"

#include <stdint.h>
#include <string.h>

/* Build 103 Inventory.swf has only three vendor buttons. Reuse its native
 * buyback list for Parts, with a separate market-status filter. All changes
 * below are to the running UI; packaged movies remain untouched. */
typedef struct shop_value {
    void* object_interface;
    unsigned int type;
    union { double number; const char* string; void* object; } data;
} shop_value;
typedef char shop_value_size_must_be_16[sizeof(shop_value) == 16 ? 1 : -1];

typedef unsigned char (FANG_THISCALL *shop_member_fn)(void*, void*,
    const char*, shop_value*, int);
typedef unsigned char (FANG_THISCALL *shop_invoke_fn)(shop_value*,
    const char*, shop_value*, const shop_value*, unsigned int);
typedef void (FANG_THISCALL *shop_release_fn)(shop_value*);
typedef void (FANG_THISCALL *shop_refresh_fn)(BYTE*);
typedef void (__cdecl *shop_event_fn)(BYTE*, const char*, shop_value*);

static shop_member_fn shop_get_member;
static shop_member_fn shop_set_member;
static shop_invoke_fn shop_invoke;
static shop_release_fn shop_release;
static shop_refresh_fn shop_refresh;
static shop_event_fn shop_event;
static BYTE* shop_controller;
static unsigned int shop_market __attribute__((used)) = 2;
static unsigned int shop_ui_stage;
enum { shop_tab_index = 1000000 };

static void shop_trace_stage(unsigned int stage) {
    if (shop_ui_stage != stage) {
        shop_ui_stage = stage;
        trace_client_state("shop_parts_ui_stage", stage);
    }
}

static shop_value shop_number(double number) {
    shop_value result = {0};
    result.type = 3;
    result.data.number = number;
    return result;
}

static shop_value shop_string(const char* string) {
    shop_value result = {0};
    result.type = 4;
    result.data.string = string;
    return result;
}

static shop_value shop_boolean(int is_enabled) {
    shop_value result = {0};
    result.type = 2;
    *(unsigned char*)&result.data = is_enabled != 0;
    return result;
}

static int shop_get(shop_value* parent, const char* name, shop_value* result) {
    unsigned int type = parent->type & 0x8F;
    if (type < 6 || type > 8 || parent->object_interface == NULL) {
        return 0;
    }
    return shop_get_member(parent->object_interface, parent->data.object, name,
        result, type == 8);
}

static void shop_set(shop_value* parent, const char* name, shop_value property) {
    unsigned int type = parent->type & 0x8F;
    if (type >= 6 && type <= 8 && parent->object_interface != NULL) {
        shop_set_member(parent->object_interface, parent->data.object, name,
            &property, type == 8);
    }
}

static void shop_label(shop_value* button, const char* label) {
    shop_value text = {0};
    shop_set(button, "label", shop_string(label));
    if (shop_get(button, "textField", &text)) {
        shop_set(&text, "text", shop_string(label));
    }
    shop_release(&text);
}

static void shop_layout(shop_value* vendor, shop_value* clone) {
    static const char* names[] = {"btnTab1", "btnTab2", "fangPartsTab", "btnTab3"};
    shop_value start = {0}, stride = {0}, height = {0};
    unsigned int index;
    if (!shop_get(clone, "fangTabStart", &start) ||
        !shop_get(clone, "fangTabStride", &stride) ||
        !shop_get(clone, "fangTabHeight", &height) ||
        (start.type & 0x8F) != 3 || (stride.type & 0x8F) != 3 ||
        (height.type & 0x8F) != 3) {
        goto done;
    }
    for (index = 0; index < 4; ++index) {
        shop_value tab = {0};
        shop_value arguments[2] = {shop_number(stride.data.number - 2), height};
        if (shop_get(vendor, names[index], &tab)) {
            shop_set(&tab, "_autoSize", shop_boolean(0));
            shop_invoke(&tab, "setSize", NULL, arguments, 2);
            shop_invoke(&tab, "validateNow", NULL, NULL, 0);
            shop_set(&tab, "_width", arguments[0]);
            shop_set(&tab, "_x", shop_number(start.data.number + index * stride.data.number));
        }
        shop_release(&tab);
    }
done:
    shop_release(&height);
    shop_release(&stride);
    shop_release(&start);
}

static void shop_play_transition(BYTE* controller) {
    shop_value content = {0}, vendor = {0}, transition = {0};
    shop_value source = {0}, flash = {0}, button = {0};
    shop_value position = {0}, width = {0};
    shop_value arguments[2];
    shop_value* root = (shop_value*)(controller + 1368);
    if (!shop_get(root, "inventoryContent", &content) ||
        !shop_get(&content, "vendorMC", &vendor)) {
        goto done;
    }
    /* Inventory.vendorTabClicked plays the panel transition at frame 2
     * and its button flash at frame 1. Give Parts its own flash instance
     * so the existing Buyback animation stays attached to Buyback. */
    if (shop_get(&vendor, "tabTransitionMC", &transition)) {
        arguments[0] = shop_number(2);
        shop_invoke(&transition, "gotoAndPlay", NULL, arguments, 1);
    }
    if (!shop_get(&vendor, "fangPartsFlash", &flash) ||
        (flash.type & 0x8F) < 6) {
        if (!shop_get(&vendor, "btnTab_FlashMC6", &source)) {
            goto done;
        }
        arguments[0] = shop_string("fangPartsFlash");
        arguments[1] = shop_number(18001);
        shop_invoke(&source, "duplicateMovieClip", NULL, arguments, 2);
        shop_release(&flash);
        memset(&flash, 0, sizeof(flash));
        if (!shop_get(&vendor, "fangPartsFlash", &flash)) {
            goto done;
        }
    }
    if (!shop_get(&vendor, "fangPartsTab", &button) ||
        !shop_get(&button, "_x", &position) ||
        !shop_get(&button, "_width", &width)) {
        goto done;
    }
    shop_set(&flash, "_x", position);
    shop_set(&flash, "_width", width);
    shop_set(&flash, "_visible", shop_boolean(1));
    arguments[0] = shop_number(1);
    shop_invoke(&flash, "gotoAndPlay", NULL, arguments, 1);
done:
    shop_release(&width);
    shop_release(&position);
    shop_release(&button);
    shop_release(&flash);
    shop_release(&source);
    shop_release(&transition);
    shop_release(&vendor);
    shop_release(&content);
}

static void shop_update_ui(BYTE* controller) {
    shop_value content = {0}, vendor = {0}, button = {0}, clone = {0};
    shop_value first = {0}, last = {0}, width = {0}, height = {0}, icon = {0};
    shop_value header = {0}, initialized = {0};
    shop_value arguments[3];
    shop_value* root = (shop_value*)(controller + 1368);
    double start, stride;
    unsigned int stage = 0;
    stage = 1;
    if (!controller[1400]) {
        goto done;
    }
    stage = 2;
    if (!shop_get(root, "inventoryContent", &content)) {
        goto done;
    }
    stage = 3;
    if (!shop_get(&content, "vendorMC", &vendor)) {
        goto done;
    }
    stage = 4;
    if (shop_get(&vendor, "fangPartsTab", &clone) && (clone.type & 0x8F) >= 6) {
        if (shop_get(&clone, "slotIndex", &initialized) &&
            (initialized.type & 0x8F) == 3 &&
            initialized.data.number == shop_tab_index) {
            goto header;
        }
    }
    stage = 5;
    if (!shop_get(&vendor, "btnTab1", &button) ||
        !shop_get(&button, "_x", &first) ||
        !shop_get(&button, "_width", &width) ||
        !shop_get(&button, "_height", &height)) {
        goto done;
    }
    shop_release(&button);
    memset(&button, 0, sizeof(button));
    if (!shop_get(&vendor, "btnTab3", &button) ||
        !shop_get(&button, "_x", &last) ||
        (first.type & 0x8F) != 3 || (last.type & 0x8F) != 3 ||
        (width.type & 0x8F) != 3 || (height.type & 0x8F) != 3) {
        goto done;
    }
    arguments[0] = shop_string("fangPartsTab");
    arguments[1] = shop_number(18000);
    stage = 6;
    if ((clone.type & 0x8F) < 6) {
        /* Resolve the child from its parent after duplication. A movie can
         * create the child without supplying a native Invoke return value. */
        shop_invoke(&button, "duplicateMovieClip", NULL, arguments, 2);
        shop_release(&clone);
        memset(&clone, 0, sizeof(clone));
        if (!shop_get(&vendor, "fangPartsTab", &clone) ||
            (clone.type & 0x8F) < 6) {
            goto done;
        }
    }
    stage = 7;
    start = first.data.number;
    stride = (last.data.number - start + width.data.number) / 4;
    shop_set(&clone, "fangTabStart", shop_number(start));
    shop_set(&clone, "fangTabStride", shop_number(stride));
    shop_set(&clone, "fangTabHeight", height);
    /* duplicateMovieClip starts the symbol on its initial timeline frame.
     * Unlike the authored buttons, this dynamic instance has no placement
     * initialization from Inventory.swf. Run UIComponent.onLoadImpl and the
     * Button state transition explicitly before sizing or binding it.
     * These are the stock movie's lifecycle methods, not injected script. */
    shop_set(&clone, "__width", shop_number(stride - 2));
    shop_set(&clone, "__height", height);
    shop_set(&clone, "_label", shop_string("PARTS"));
    shop_set(&clone, "_autoSize", shop_boolean(0));
    arguments[0] = shop_string("up");
    shop_invoke(&clone, "gotoAndStop", NULL, arguments, 1);
    shop_invoke(&clone, "onLoadImpl", NULL, NULL, 0);
    shop_invoke(&clone, "setState", NULL, arguments, 1);
    arguments[0] = shop_number(stride - 2);
    arguments[1] = height;
    shop_invoke(&clone, "setSize", NULL, arguments, 2);
    shop_invoke(&clone, "validateNow", NULL, NULL, 0);
    shop_set(&clone, "_visible", shop_boolean(1));
    shop_set(&clone, "_alpha", shop_number(100));
    shop_set(&clone, "disabled", shop_boolean(0));
    shop_label(&clone, "PARTS");
    shop_set(&clone, "tooltipText", shop_string("PARTS"));
    shop_set(&clone, "_tooltipText", shop_string("PARTS"));
    shop_set(&clone, "slotType", shop_number(0));
    if (shop_get(&clone, "btnTabIconMC", &icon)) {
        arguments[0] = shop_string("parts");
        shop_invoke(&icon, "gotoAndStop", NULL, arguments, 1);
    }
    /* Bind the stock movie's existing dispatcher, without injecting script.
     * It forwards slotIndex through OnItemDoubleClick; consume our sentinel
     * before the native handler can interpret it as an inventory index. */
    arguments[0] = shop_string("click");
    arguments[1] = *(shop_value*)(controller + 1384);
    arguments[2] = shop_string("doubleClickItem");
    if (!shop_invoke(&clone, "addEventListener", NULL, arguments, 3)) {
        goto done;
    }
    shop_set(&clone, "slotIndex", shop_number(shop_tab_index));
    trace_client_state("shop_parts_tab_created", 1);
header:
    stage = 8;
    shop_layout(&vendor, &clone);
    {
        shop_value selected = shop_boolean(shop_controller == controller && shop_market == 1);
        shop_set(&clone, "selected", selected);
    }
    if (shop_controller == controller && shop_market == 1 &&
        *(unsigned int*)(controller + 1404) == 0 &&
        shop_get(&vendor, "vendorHeaderMC", &header)) {
        shop_set(&header, "text", shop_string("PARTS"));
    }
done:
    shop_trace_stage(stage);
    shop_release(&initialized);
    shop_release(&header);
    shop_release(&icon);
    shop_release(&width);
    shop_release(&height);
    shop_release(&last);
    shop_release(&first);
    shop_release(&clone);
    shop_release(&button);
    shop_release(&vendor);
    shop_release(&content);
}

static void shop_select_offer(BYTE* controller, shop_value* arguments) {
    const BYTE* account;
    const BYTE* entries;
    const BYTE* end;
    unsigned int index;
    double slot;
    if (shop_controller != controller || shop_market != 1 ||
        *(unsigned int*)(controller + 1404) != 0 ||
        !readable_range(arguments, 3 * sizeof(*arguments)) ||
        (arguments[0].type & 0x8F) != 3 ||
        (arguments[1].type & 0x8F) != 2 ||
        *(unsigned char*)&arguments[1].data == 0 ||
        (arguments[2].type & 0x8F) != 3 || arguments[2].data.number != 0) {
        return;
    }
    account = *(const BYTE**)(controller + 64);
    if (!readable_range(account, 26936)) {
        return;
    }
    entries = *(const BYTE**)(account + 26928);
    end = *(const BYTE**)(account + 26932);
    slot = arguments[0].data.number;
    if ((uintptr_t)end < (uintptr_t)entries ||
        ((uintptr_t)end - (uintptr_t)entries) % 80 != 0 ||
        !(slot >= 0 && slot < ((uintptr_t)end - (uintptr_t)entries) / 80)) {
        return;
    }
    index = (unsigned int)slot;
    if (slot != index || !readable_range(entries + index * 80, 80) ||
        *(const unsigned int*)(entries + index * 80 + 32) != 1) {
        return;
    }
    /* Stock clearVendorSelection keeps this slot and publishes deselection
     * for the others. Reset the native selection and total before it adds
     * the newly selected offer, including any selections on another page. */
    shop_invoke((shop_value*)(controller + 1384), "clearVendorSelection",
        NULL, arguments, 1);
    *(unsigned int*)(controller + 1252) = *(unsigned int*)(controller + 1248);
    *(unsigned int*)(controller + 40) = 0;
    controller[12] = 1;
}

static void __cdecl shop_on_event(BYTE* controller, const char* event,
    shop_value* arguments) {
    int is_tab_event = strcmp(event, "OnVendorTabClicked") == 0 ||
        strcmp(event, "OnWeaponTabClicked") == 0 ||
        strcmp(event, "OnUpgradeTabClicked") == 0;
    if (strcmp(event, "OnItemDoubleClick") == 0 && arguments != NULL &&
        (arguments[0].type & 0x8F) == 3 &&
        arguments[0].data.number == shop_tab_index) {
        shop_controller = controller;
        shop_market = 1;
        trace_client_state("shop_parts_tab_selected", 1);
        /* Native buyback mode handles paging, tooltip, selection and b<ID>.
         * Force its tab transition even when Buyback was already selected. */
        *(unsigned int*)(controller + 1404) = 2;
        shop_event(controller, "OnVendorTabClicked", arguments);
        *(unsigned int*)(controller + 24) = 0;
        *(unsigned int*)(controller + 1252) = *(unsigned int*)(controller + 1248);
        *(unsigned int*)(controller + 40) = 0;
        controller[11] = 1;
        controller[12] = 1;
        shop_update_ui(controller);
        shop_play_transition(controller);
        return;
    }
    if (is_tab_event || strcmp(event, "OnCloseInventoryClicked") == 0) {
        if (shop_market == 1) {
            /* Leaving Parts changes the native buyback filter even when the
             * movie merely closes. Do not retain a Parts page against the
             * smaller buyback index list on the next inventory refresh.
             * Build 103 clamps an invalid page to pageCount, then indexes it
             * as a real page in 4326A0, beyond the filtered entry vector. */
            if (strcmp(event, "OnVendorTabClicked") == 0) {
                *(unsigned int*)(controller + 1404) = 2;
            }
            *(unsigned int*)(controller + 24) = 0;
            controller[11] = 1;
        }
        shop_market = 2;
    }
    if (strcmp(event, "OnItemSelect") == 0) {
        shop_select_offer(controller, arguments);
    }
    shop_event(controller, event, arguments);
    /* GetLootImage, BuildLootTooltip and the other query callbacks publish
     * their return through the movie's shared ExternalInterface state.
     * Running component validation/state changes after those callbacks can
     * reenter Flash and replace that return before the caller receives it.
     * Refresh owns general UI maintenance; tab actions have no return data. */
    if (is_tab_event) {
        shop_update_ui(controller);
    }
}

static void FANG_THISCALL shop_on_refresh(BYTE* controller) {
    if (shop_controller != controller) {
        shop_controller = controller;
        shop_market = 2;
    }
    shop_refresh(controller);
    shop_update_ui(controller);
}

/* Replace only cmp eax,2 / jne skip. No shared account records are mutated. */
static void __attribute__((naked)) shop_filter(void) {
    __asm__(
        "cmpl _shop_market, %eax\n"
        "je 1f\n"
        "addl $55, (%esp)\n"
        "1: ret\n");
}

int fang_install_shop_ui(HMODULE executable) {
    BYTE* base = (BYTE*)executable;
    void** callbacks = (void**)(base + 0xD0FAC0);
    BYTE* filter = base + 0x32794;
    static const BYTE signature[] = {0x83, 0xF8, 0x02, 0x75, 0x37};
    BYTE replacement[5] = {0xE8, 0, 0, 0, 0};
    int32_t relative;
    unsigned int index, installed = 0;
    DWORD protection, ignored;
    if (sizeof(void*) != 4 || !readable_range(callbacks, 26 * 8) ||
        !readable_range(filter, 5) || memcmp(filter, signature, 5) != 0) {
        return 0;
    }
    for (index = 0; index < 26; ++index) {
        if (callbacks[index * 2] != base + 0x35D90) {
            return 0;
        }
    }
    shop_get_member = (shop_member_fn)(base + 0x14DAE0);
    shop_set_member = (shop_member_fn)(base + 0x14B510);
    shop_invoke = (shop_invoke_fn)(base + 0x15A0);
    shop_release = (shop_release_fn)(base + 0x17C0);
    shop_refresh = (shop_refresh_fn)(base + 0x326A0);
    shop_event = (shop_event_fn)(base + 0x35D90);
    if (!patch_call(base + 0x357D2, shop_refresh, shop_on_refresh)) {
        return 0;
    }
    for (index = 0; index < 26; ++index) {
        if (!patch_pointer(&callbacks[index * 2], shop_event, shop_on_event)) {
            goto rollback;
        }
        ++installed;
    }
    if (!VirtualProtect(filter, 5, PAGE_EXECUTE_READWRITE, &protection)) {
        goto rollback;
    }
    relative = (int32_t)((BYTE*)shop_filter - (filter + 5));
    memcpy(replacement + 1, &relative, 4);
    memcpy(filter, replacement, 5);
    FlushInstructionCache(GetCurrentProcess(), filter, 5);
    VirtualProtect(filter, 5, protection, &ignored);
    return 1;
rollback:
    while (installed != 0) {
        --installed;
        if (!patch_pointer(&callbacks[installed * 2], shop_on_event, shop_event)) {
            trace_client_state("shop_ui_rollback_failed", installed);
        }
    }
    if (!patch_call(base + 0x357D2, shop_on_refresh, shop_refresh)) {
        trace_client_state("shop_ui_refresh_rollback_failed", 1);
    }
    return 0;
}


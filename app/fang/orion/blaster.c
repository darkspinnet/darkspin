#include "orion/orion.h"
#include "client103/image.h"
#include "hook.h"

#include <stdint.h>
#include <string.h>

typedef unsigned char (FANG_THISCALL* orion_resource_read_fn)(BYTE*,
    const uint32_t*, BYTE**, unsigned int, unsigned int, void*, const uint32_t*);

static orion_resource_read_fn native_resource_read;
static fang_hook_set orion_hooks;
static volatile LONG redirect_count;

/* Build 103's right weapon_lightspeedTempest_04 rigblock (41B27833)
 * references mesh 41B27832, absent from the installed package indexes.
 * Arenas_RDX9.package contains its right mesh as 41B27833 instead. The saved
 * creature already has both linked weapon parts; do not change that model,
 * the item ID, symmetry, or any packaged property/resource bytes.
 *
 * User-approved compatibility scope: this exact Orion blaster mesh only.
 * Native ResourceKey order is instance/type/group. */
// Reads: recognizes only the broken Orion right-blaster mesh reference.
static int is_orion_blaster_key(const uint32_t* key) {
    return readable_range(key, 3 * sizeof(*key)) &&
        key[0] == UINT32_C(0x41b27832) &&
        key[1] == UINT32_C(0x2f4e681b) &&
        key[2] == UINT32_C(0x40606000);
}

// Reads/modifies: redirects Orion's bad mesh key in a private request copy.
static unsigned char FANG_THISCALL read_orion_resource(BYTE* manager,
    const uint32_t* key, BYTE** resource, unsigned int flags,
    unsigned int request_flags, void* callback, const uint32_t* cache_key) {
    uint32_t model_key[3];
    uint32_t model_cache_key[3];
    unsigned char result;
    LONG count;
    if (!is_orion_blaster_key(key)) {
        return native_resource_read(manager, key, resource, flags,
            request_flags, callback, cache_key);
    }
    memcpy(model_key, key, sizeof(model_key));
    model_key[0] = UINT32_C(0x41b27833);
    if (is_orion_blaster_key(cache_key)) {
        memcpy(model_cache_key, cache_key, sizeof(model_cache_key));
        model_cache_key[0] = UINT32_C(0x41b27833);
        cache_key = model_cache_key;
    }
    /* B076D0 copies the primary key before provider dispatch. Native callers
     * also use stack keys, including the optional cache-lookup key. Preserve
     * the original resource ownership, callback, flags and return status. */
    result = native_resource_read(manager, model_key, resource, flags,
        request_flags, callback, cache_key);
    count = InterlockedIncrement(&redirect_count);
    if (count > 0 && count <= 16) {
        trace_client_state("orion_blaster_mesh_redirect", model_key[0]);
        trace_client_state("orion_blaster_mesh_read", (unsigned int)result);
    }
    return result;
}

// Modifies: installs a checked build-103 resource-reader pointer substitution.
int fang_orion_install(HMODULE executable) {
    static const BYTE reader_signature[] = {
        0x8b, 0x44, 0x24, 0x18, 0x8b, 0x54, 0x24, 0x14, 0x50
    };
    fang_client103_image image;
    void** reader;
    if (!fang_client103_image_open(executable, 0xc40d70, &image)) return 0;
    /* B0AA90 installs vtable 1040D60; slot 3 is B07A50. Its six stack
     * arguments and RET 18h are verified in the installed build-103 EXE. */
    if (!fang_client103_expect(&image, "orionBlaster", "resourceReadCode",
        0x707a50, reader_signature, sizeof(reader_signature),
        "build103 B07A50: thiscall full-key read, six stack arguments")) return 0;
    reader = (void**)fang_client103_binding(&image, "orionBlaster",
        "resourceReadSlot", 0xc40d6c, sizeof(*reader),
        "build103 B0AA90 vtable 1040D60 slot 3 -> B07A50");
    if (reader == NULL) return 0;
    native_resource_read = (orion_resource_read_fn)(image.base + 0x707a50);
    fang_hook_begin(&orion_hooks, "orionBlaster");
    if (!fang_hook_pointer(&orion_hooks, "resourceRead", reader,
        native_resource_read, read_orion_resource,
        "user-approved Orion-only alias 41B27832:2F4E681B:40606000 -> 41B27833")) return 0;
    fang_hook_commit(&orion_hooks);
    return 1;
}

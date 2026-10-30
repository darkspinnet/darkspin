#include "client103/camera.h"
#include "hook/patch.h"

static fang_client103_camera_bindings camera_bindings;

_Static_assert(sizeof(float) == 4, "build103 float fields require 32 bits");
_Static_assert(sizeof(uint32_t) == 4, "build103 handles require 32 bits");
_Static_assert(camera_native_max + sizeof(float) == camera_read_size, "checked zoom span");
_Static_assert(camera_hold_timer + sizeof(float) == camera_snapshot_size, "checked cache span");

int fang_client103_camera_bind(HMODULE executable) {
    static const BYTE getter_signature[] = {
        0xE8, 0x2B, 0x11, 0x28, 0x00, 0x8B, 0x10, 0x8B, 0xC8,
        0x8B, 0x42, 0x6C, 0xFF, 0xD0, 0x8B, 0x10, 0x8B, 0xC8,
        0x8B, 0x42, 0x38, 0xFF, 0xD0,
    };
    static const BYTE object_signature[] = {
        0x8B, 0x44, 0x24, 0x04, 0x50,
        0xE8, 0x46, 0x32, 0xFE, 0xFF, 0x8B, 0xC8,
        0xE8, 0x2F, 0x82, 0xFF, 0xFF, 0xC3,
    };
    fang_client103_camera_bindings candidate = {0};
    if (camera_bindings.image.base == (BYTE*)executable && camera_bindings.camera != NULL) return 1;
    if (!fang_client103_image_open(executable, 0xBE1970, &candidate.image)) return 0;
    if (!fang_client103_expect(&candidate.image, "camera", "getter", 0x12DA80,
        getter_signature, sizeof(getter_signature), "build103 sub_52DA80 existing getter signature")) return 0;
    candidate.camera = (camera_getter_fn)fang_client103_at(&candidate.image, 0x12DA80, sizeof(getter_signature));
    candidate.vtable = fang_client103_at(&candidate.image, 0xBE1968, 8);
    if (candidate.vtable == NULL) {
        return fang_client103_expect(&candidate.image, "camera", "followVtable", 0xBE1968,
            NULL, 8, "build103 follow-camera runtime vtable identity");
    }
    /* Optional as in the existing observer: lookup failure is reported without
     * disabling cached camera snapshots or zoom. */
    if (fang_client103_expect(&candidate.image, "camera", "objectLookup", 0x5D94F0,
        object_signature, sizeof(object_signature), "build103 sub_9D94F0 existing object lookup signature")) {
        candidate.object = (camera_object_fn)fang_client103_at(&candidate.image, 0x5D94F0, sizeof(object_signature));
    }
    camera_bindings = candidate;
    return 1;
}

int fang_client103_camera_zoom_bind(HMODULE executable) {
    static const BYTE refresh_call[] = {
        0xE8, 0xF0, 0x3E, 0x09, 0x00, 0x50,
        0xE8, 0x3A, 0x92, 0xFF, 0xFF,
        0x6A, 0x00, 0xE8, 0x53, 0x4B, 0x09, 0x00,
    };
    static const BYTE distance_signature[] = {
        0xF3, 0x0F, 0x10, 0x54, 0x24, 0x04,
        0xF3, 0x0F, 0x10, 0x44, 0x24, 0x08,
        0x0F, 0x2F, 0xC2, 0xF3, 0x0F, 0x11, 0x91, 0xC0, 0x00, 0x00, 0x00,
    };
    static const BYTE level_signature[] = {
        0x8B, 0x44, 0x24, 0x04, 0x85, 0xC0, 0x74, 0x07,
        0x8B, 0x80, 0x8C, 0x74, 0x00, 0x00, 0xC3,
    };
    static const BYTE root_opcode[] = {0xA1};
    static const BYTE root_suffix[] = {
        0x64, 0x8B, 0x0D, 0x2C, 0x00, 0x00, 0x00,
        0x8B, 0x14, 0x81, 0x8B, 0x82, 0x08, 0x00, 0x00, 0x00, 0xC3,
    };
    static const BYTE refresh_prefix[] = {0x64, 0xA1, 0, 0, 0, 0, 0x6A, 0xFF};
    fang_client103_camera_bindings candidate;
    if (!fang_client103_camera_bind(executable)) return 0;
    if (camera_bindings.refresh != NULL) return 1;
    candidate = camera_bindings;
    if (!fang_client103_expect(&candidate.image, "zoom", "missionRefreshCall", 0x5196B,
        refresh_call, sizeof(refresh_call), "build103 local gameplay update call to sub_44ABB0") ||
        !fang_client103_expect(&candidate.image, "zoom", "distanceFields", 0x12E1B0,
        distance_signature, sizeof(distance_signature), "build103 sub_52E1B0 interval field writes") ||
        !fang_client103_expect(&candidate.image, "zoom", "levelGetter", 0x5BCC30,
        level_signature, sizeof(level_signature), "build103 sub_9BCC30") ||
        !fang_client103_expect(&candidate.image, "zoom", "rootGetterOpcode", 0x5BCBE0,
        root_opcode, sizeof(root_opcode), "build103 sub_9BCBE0; relocated TLS operand excluded") ||
        !fang_client103_expect(&candidate.image, "zoom", "rootGetterBody", 0x5BCBE5,
        root_suffix, sizeof(root_suffix), "build103 sub_9BCBE0 verified installed executable") ||
        !fang_client103_expect(&candidate.image, "zoom", "missionRefresh", 0x4ABB0,
        refresh_prefix, sizeof(refresh_prefix), "build103 sub_44ABB0 verified installed executable")) return 0;
    candidate.root = (camera_getter_fn)fang_client103_at(&candidate.image, 0x5BCBE0, 22);
    candidate.level = (camera_level_fn)fang_client103_at(&candidate.image, 0x5BCC30, sizeof(level_signature));
    candidate.refresh = (camera_refresh_fn)fang_client103_at(&candidate.image, 0x4ABB0, sizeof(refresh_prefix));
    camera_bindings = candidate;
    return 1;
}

const fang_client103_camera_bindings* fang_client103_camera_get_bindings(void) {
    return camera_bindings.camera == NULL ? NULL : &camera_bindings;
}

int fang_client103_camera_is_valid(const fang_client103_camera_bindings* bindings,
    const BYTE* camera, size_t accessed_size) {
    return bindings != NULL && accessed_size >= sizeof(void*) &&
        accessed_size <= camera_snapshot_size && readable_range(camera, accessed_size) &&
        *(const void* const*)camera == bindings->vtable;
}

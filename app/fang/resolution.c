#include "resolution.h"

typedef void* (__cdecl* manager_getter_fn)(void);
typedef unsigned int (FANG_THISCALL* option_read_fn)(void*, unsigned int);
typedef void (FANG_THISCALL* option_write_fn)(void*, unsigned int, unsigned int);
typedef int (FANG_THISCALL* resolution_count_fn)(void*);
typedef unsigned char (FANG_THISCALL* resolution_read_fn)(void*, unsigned int, int*, int*, int*);
typedef void (FANG_THISCALL* render_resize_fn)(void*, unsigned char, int, int, int);
typedef BYTE* (FANG_THISCALL* render_read_fn)(void*);
typedef void (FANG_THISCALL* camera_aspect_fn)(void*, float);

static void* resolution_settings(BYTE* base) {
    void* settings = ((manager_getter_fn)(base + 0x3AEBD0))();
    if (!readable_range(settings, sizeof(void*)) ||
        *(void***)settings != (void**)(base + 0xC0D498)) {
        trace_client_state("display_resolution_manager_missing", 1);
        return NULL;
    }
    return settings;
}

int fang_read_resolution(BYTE* base, struct fang_resolution* resolution) {
    void* settings = resolution_settings(base);
    option_read_fn read = (option_read_fn)(base + 0x4542D0);
    resolution_read_fn resolve = (resolution_read_fn)(base + 0x454650);
    if (settings == NULL) {
        return 0;
    }
    if (!resolve(settings, read(settings, 0x046170A1),
        &resolution->width, &resolution->height, &resolution->refresh) ||
        resolution->width <= 0 || resolution->height <= 0 ||
        resolution->width > 32767 || resolution->height > 32767) {
        trace_client_state("display_resolution_invalid", 1);
        return 0;
    }
    return 1;
}

int fang_select_resolution(BYTE* base, const struct fang_resolution* resolution) {
    void* settings = resolution_settings(base);
    resolution_count_fn count = (resolution_count_fn)(base + 0x454620);
    resolution_read_fn resolve = (resolution_read_fn)(base + 0x454650);
    option_read_fn read = (option_read_fn)(base + 0x4542D0);
    option_write_fn write = (option_write_fn)(base + 0x4562D0);
    int selection = -1;
    int resolution_count;
    if (settings == NULL) {
        return 0;
    }
    resolution_count = count(settings);
    for (int index = 0; index < resolution_count; index++) {
        struct fang_resolution candidate = {0};
        if (!resolve(settings, index, &candidate.width, &candidate.height, &candidate.refresh) ||
            candidate.width != resolution->width || candidate.height != resolution->height) {
            continue;
        }
        if (selection < 0) {
            selection = index;
        }
        if (candidate.refresh == resolution->refresh) {
            selection = index;
            break;
        }
    }
    if (selection < 0) {
        trace_client_state("display_resolution_unavailable",
            ((unsigned int)resolution->width << 16) | (unsigned int)resolution->height);
        return 0;
    }
    /* Use the original setter: these mode changes must not queue a second
     * Graphics Apply or overwrite the remembered windowed resolution. */
    write(settings, 0x046170A1, (unsigned int)selection);
    if (read(settings, 0x046170A1) != (unsigned int)selection) {
        trace_client_state("display_resolution_option_rejected", selection);
        return 0;
    }
    return 1;
}

int fang_render_resolution(BYTE* base, void* app, const struct fang_resolution* resolution) {
    void* renderer = ((manager_getter_fn)(base + 0x3AEBF0))();
    render_resize_fn resize = (render_resize_fn)(base + 0x48E8C0);
    render_read_fn read = (render_read_fn)(base + 0x48DC90);
    camera_aspect_fn set_aspect = (camera_aspect_fn)(base + 0x3B3720);
    BYTE* mode;
    void* camera;
    if (resolution->width <= 0 || resolution->height <= 0 ||
        resolution->width > 32767 || resolution->height > 32767 ||
        !readable_range(renderer, sizeof(void*)) ||
        *(void***)renderer != (void**)(base + 0xC0F558) ||
        !readable_range(app, 0x54)) {
        trace_client_state("display_renderer_missing", 1);
        return 0;
    }
    camera = *(void**)((BYTE*)app + 0x50);
    if (!readable_range(camera, 0x1B4) ||
        !readable_range(*(void**)((BYTE*)camera + 0x1B0), 0x70)) {
        trace_client_state("display_camera_missing", 1);
        return 0;
    }
    mode = read(renderer);
    if (!readable_range(mode, 0x20) || mode[0x10] != 0) {
        trace_client_state("display_renderer_not_windowed", 1);
        return 0;
    }
    /* Same device-reset operation and camera update as App::Update at
     * 0x7EA2CD. The final argument is the adapter index, not a resolution.
     * Keep the renderer windowed; display.c restores the intended window bounds. */
    if (*(int*)mode != resolution->width || *(int*)(mode + 4) != resolution->height) {
        resize(renderer, 0, resolution->width, resolution->height, -1);
        mode = read(renderer);
    }
    if (!readable_range(mode, 0x20) || mode[0x10] != 0 ||
        *(int*)mode != resolution->width || *(int*)(mode + 4) != resolution->height) {
        trace_client_state("display_render_resize_failed", 1);
        return 0;
    }
    set_aspect(camera, ((float)resolution->width / resolution->height) * *(float*)(mode + 0x1C));
    trace_client_state("display_render_resolution",
        ((unsigned int)resolution->width << 16) | (unsigned int)resolution->height);
    return 1;
}

#include "resolution.h"

typedef void* (__cdecl* manager_getter_fn)(void);
typedef unsigned int (FANG_THISCALL* option_read_fn)(void*, unsigned int);
typedef void (FANG_THISCALL* option_write_fn)(void*, unsigned int, unsigned int);
typedef int (FANG_THISCALL* resolution_count_fn)(void*);
typedef unsigned char (FANG_THISCALL* resolution_read_fn)(void*, unsigned int, int*, int*, int*);
typedef void (FANG_THISCALL* render_resize_fn)(void*, unsigned char, int, int, int);
typedef BYTE* (FANG_THISCALL* render_read_fn)(void*);
typedef void (FANG_THISCALL* camera_aspect_fn)(void*, float);
typedef void (FANG_THISCALL* ui_resize_fn)(void*, float, float);

/* GFxViewport in the shipped Scaleform runtime (13 four-byte fields). */
struct flash_viewport {
    int buffer_width;
    int buffer_height;
    int left;
    int top;
    int width;
    int height;
    int scissor_left;
    int scissor_top;
    int scissor_width;
    int scissor_height;
    float scale;
    float aspect_ratio;
    unsigned int flags;
};

typedef void (FANG_THISCALL* viewport_read_fn)(void*, struct flash_viewport*);
typedef void (FANG_THISCALL* viewport_write_fn)(void*, const struct flash_viewport*);

static int resize_flash_resolution(BYTE* base, const struct fang_resolution* resolution) {
    void* flash = ((manager_getter_fn)(base + 0x25920))();
    void* movie;
    void* view;
    void** methods;
    struct flash_viewport viewport = {0};
    viewport_read_fn read;
    viewport_write_fn write;
    /* Before Flash initialization, cMovie::Load (0xCF7B20) takes its viewport
     * from the current render buffer. A later resize must update it explicitly. */
    if (flash == NULL) {
        return 1;
    }
    if (!readable_range(flash, 0x80) ||
        *(void***)flash != (void**)(base + 0xC77630)) {
        trace_client_state("display_flash_manager_invalid", 1);
        return 0;
    }
    movie = *(void**)((BYTE*)flash + 0x64);
    if (movie == NULL) {
        return 1;
    }
    if (!readable_range(movie, 0x1C)) {
        trace_client_state("display_flash_movie_invalid", 1);
        return 0;
    }
    view = *(void**)((BYTE*)movie + 4);
    if (!readable_range(view, sizeof(void*))) {
        trace_client_state("display_flash_view_missing", 1);
        return 0;
    }
    methods = *(void***)view;
    if (methods != (void**)(base + 0xBE9D68) ||
        methods[0x64 / 4] != base + 0x1A8930 ||
        methods[0x68 / 4] != base + 0x19D500) {
        trace_client_state("display_flash_view_invalid", 1);
        return 0;
    }
    read = (viewport_read_fn)methods[0x68 / 4];
    write = (viewport_write_fn)methods[0x64 / 4];
    read(view, &viewport);
    if (viewport.buffer_width == resolution->width && viewport.buffer_height == resolution->height &&
        viewport.left == 0 && viewport.top == 0 &&
        viewport.width == resolution->width && viewport.height == resolution->height) {
        return 1;
    }
    /* Match cMovie::Load's viewport setup while retaining its scale, aspect,
     * clipping and flags. SetViewport also queues the ActionScript stage resize
     * (0x5A8930); copying dimensions alone would leave HUD anchors and input stale. */
    viewport.buffer_width = viewport.width = resolution->width;
    viewport.buffer_height = viewport.height = resolution->height;
    viewport.left = viewport.top = 0;
    write(view, &viewport);
    read(view, &viewport);
    if (viewport.buffer_width != resolution->width || viewport.buffer_height != resolution->height ||
        viewport.left != 0 || viewport.top != 0 ||
        viewport.width != resolution->width || viewport.height != resolution->height) {
        trace_client_state("display_flash_resize_failed", 1);
        return 0;
    }
    trace_client_state("display_flash_resolution_applied",
        ((unsigned int)viewport.width << 16) | (unsigned int)viewport.height);
    return 1;
}

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
    void* ui = ((manager_getter_fn)(base + 0x3B6F70))();
    render_resize_fn resize = (render_resize_fn)(base + 0x48E8C0);
    render_read_fn read = (render_read_fn)(base + 0x48DC90);
    camera_aspect_fn set_aspect = (camera_aspect_fn)(base + 0x3B3720);
    ui_resize_fn resize_ui = (ui_resize_fn)(base + 0x7E4020);
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
    if (ui != NULL && (!readable_range(ui, 0x838) ||
        *(void***)ui != (void**)(base + 0xC5E798))) {
        trace_client_state("display_ui_manager_invalid", 1);
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
    /* The game HUD lives in Flash's Container movie, separate from MainWin.
     * Resize it before App::Update advances the movie and handles stage resize. */
    if (!resize_flash_resolution(base, resolution)) {
        return 0;
    }
    /* WM_SIZE only updates MainWin's mouse-coordinate scale (0x9193A0).
     * WindowManager::SetScreenSize (0xBE4020) queues the separate UI resize.
     * Its consumer at 0xBE2D90 updates the UI projection and MainWin's area,
     * relayouts its children, and recalculates input scaling. Queue this even
     * when the render size already matches, including startup and rollback.
     * Before UI initialization, MainWin reads the renderer size at 0x91BADE. */
    if (ui != NULL) {
        resize_ui(ui, (float)resolution->width, (float)resolution->height);
        trace_client_state("display_ui_resolution_queued",
            ((unsigned int)resolution->width << 16) | (unsigned int)resolution->height);
    }
    trace_client_state("display_render_resolution",
        ((unsigned int)resolution->width << 16) | (unsigned int)resolution->height);
    return 1;
}

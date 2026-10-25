#include "hook.h"
#include "control.h"
#if FANG_SCENARIO
#include <math.h>
#include <string.h>
#include <stdio.h>

typedef int (FANG_THISCALL* control_editor_config_fn)(BYTE*);
typedef void (FANG_THISCALL* control_editor_exit_fn)(BYTE*);
static CRITICAL_SECTION editor_lock;
static volatile LONG is_editor_lock_initialized;
static int is_editor_observed;
static BYTE* editor_image;
static BYTE* observed_editor;
static DWORD observed_thread;
static control_editor_config_fn native_editor_config;
static control_editor_exit_fn native_editor_exit;
static uint32_t editor_generation;
typedef BYTE* (FANG_THISCALL* control_editor_find_fn)(BYTE*, uint32_t);
typedef uint32_t (FANG_THISCALL* control_editor_scalar_fn)(BYTE*);
typedef const float* (FANG_THISCALL* control_editor_area_fn)(BYTE*);
typedef float* (FANG_THISCALL* control_editor_point_fn)(BYTE*, float*, float, float);

#if defined(__GNUC__)
#define CONTROL_EDITOR_FASTCALL __attribute__((fastcall))
#else
#define CONTROL_EDITOR_FASTCALL __fastcall
#endif

int fang_control_editor_exit_begin(BYTE* editor) {
    if (!InterlockedCompareExchange(&is_editor_lock_initialized, 0, 0)) return 0;
    EnterCriticalSection(&editor_lock);
    if (observed_editor == editor) {
        editor_generation++;
        observed_editor = NULL;
        observed_thread = 0;
    }
    return 1;
}

void fang_control_editor_exit_end(int is_entered) {
    if (is_entered) LeaveCriticalSection(&editor_lock);
}

static int CONTROL_EDITOR_FASTCALL observe_editor_config(BYTE* editor, void* ignored) {
    int result;
    DWORD last_error = GetLastError();
    (void)ignored;
    EnterCriticalSection(&editor_lock);
    observed_editor = editor;
    editor_generation++;
    observed_thread = GetCurrentThreadId();
    SetLastError(last_error);
    result = native_editor_config(editor);
    last_error = GetLastError();
    /* A reentrant native exit clears the observation and is not overwritten
     * after configure returns. Foreign teardown waits for this call to finish. */
    LeaveCriticalSection(&editor_lock);
    SetLastError(last_error);
    return result;
}

static void CONTROL_EDITOR_FASTCALL observe_editor_exit(BYTE* editor, void* ignored) {
    DWORD last_error = GetLastError();
    int is_entered = fang_control_editor_exit_begin(editor);
    (void)ignored;
    SetLastError(last_error);
    native_editor_exit(editor);
    last_error = GetLastError();
    fang_control_editor_exit_end(is_entered);
    SetLastError(last_error);
}

/* This exact known wrapper can be composed by the later Mod installer. Never
 * accept arbitrary patched code as an original native lifecycle function. */
void* fang_control_editor_exit_target(BYTE* base) {
    void* target = NULL;
    if (!InterlockedCompareExchange(&is_editor_lock_initialized, 0, 0)) return NULL;
    EnterCriticalSection(&editor_lock);
    if (is_editor_observed && base == editor_image &&
        *(void**)(base + 0xBD98FC) == (void*)observe_editor_exit) target = (void*)observe_editor_exit;
    LeaveCriticalSection(&editor_lock);
    return target;
}

int fang_control_editor_install(HMODULE executable) {
    BYTE* base = (BYTE*)executable;
    void* exit_target;
    if (!base || !readable_range(base + 0xBD9984, 4) ||
        *(void**)(base + 0xBD9984) != base + 0xB9100 ||
        !readable_range(base + 0xBD98FC, 4)) return 0;
    exit_target = *(void**)(base + 0xBD98FC);
    if (exit_target != base + 0xB9470 && exit_target != fang_mod_editor_exit_hook()) return 0;
    if (InterlockedCompareExchange(&is_editor_lock_initialized, 0, 0)) return 0;
    if (!InitializeCriticalSectionAndSpinCount(&editor_lock, 4000)) return 0;
    InterlockedExchange(&is_editor_lock_initialized, 1);
    editor_image = base;
    native_editor_config = (control_editor_config_fn)(base + 0xB9100);
    native_editor_exit = (control_editor_exit_fn)exit_target;
    if (!patch_pointer((void**)(base + 0xBD9984), (void*)native_editor_config, (void*)observe_editor_config)) return 0;
    if (!patch_pointer((void**)(base + 0xBD98FC), exit_target, (void*)observe_editor_exit)) {
        if (!patch_pointer((void**)(base + 0xBD9984), (void*)observe_editor_config, (void*)native_editor_config))
            trace_client_state("scenario_editor_config_rollback_failed", 1);
        return 0;
    }
    EnterCriticalSection(&editor_lock);
    is_editor_observed = 1;
    LeaveCriticalSection(&editor_lock);
    return 1;
}

uint32_t fang_control_editor_ready(void) {
    BYTE* editor;
    BYTE* ui;
    uint32_t status = 5;
    if (!InterlockedCompareExchange(&is_editor_lock_initialized, 0, 0)) return 5;
    EnterCriticalSection(&editor_lock);
    editor = observed_editor;
    /* PlayerEditor constructor installs FD98D0. Existing EditorUI consumers
     * identify editor+128, ui+88 owner, enabled+157/block+158 and model+176.
     * This observes normal editors independently of explicit Mod/Build mode. */
    if (is_editor_observed && observed_thread == GetCurrentThreadId() &&
        readable_range(editor, 180) && *(void**)editor == editor_image + 0xBD98D0) {
        ui = *(BYTE**)(editor + 128);
        if (readable_range(ui, 159) && *(BYTE**)(ui + 88) == editor &&
            ui[157] && !ui[158] && readable_range(*(void**)(editor + 176), 4)) status = 1;
    }
    LeaveCriticalSection(&editor_lock);
    return status;
}

typedef BYTE* (FANG_THISCALL* control_editor_iterator_fn)(BYTE*, BYTE**);

static void observe_editor_render_groups(BYTE* editor) {
    static const unsigned int offsets[] = {612, 628, 644, 692};
    static const char* names[] = {"build", "paint", "skin", "overlay"};
    BYTE* world = *(BYTE**)(editor + 136);
    unsigned int index, word;
    char name[80];
    trace_client_state("scenario_editor_use_skin", editor[813]);
    trace_client_state("scenario_editor_skin_display", editor[940]);
    trace_client_state("scenario_editor_animation_display", editor[941]);
    for (index = 0; index < 4; ++index) {
        for (word = 0; word < (index == 3 ? 2u : 4u); ++word) {
            _snprintf(name, sizeof(name), "scenario_editor_group_%s_%u", names[index], word);
            trace_client_state(name, *(uint32_t*)(editor + offsets[index] + word * 4));
        }
    }
    if (!readable_range(world, 480) || !readable_range(*(void**)world, 304) ||
        (*(void***)world)[75] != editor_image + 0x4AC0B0) return;
    for (word = 0; word < 4; ++word) {
        _snprintf(name, sizeof(name), "scenario_editor_group_active_%u", word);
        trace_client_state(name, *(uint32_t*)(world + 432 + word * 4));
    }
}

/* Build-103 IWindow children are an intrusive list: Begin/End return an
 * iterator through an out slot; each link belongs to IWindow at link-4.
 * This ABI was observed in rea-editor-tree.js. Never retain these pointers
 * beyond this owner-thread observation, and never export them as handles. */
uint32_t fang_control_editor_hierarchy(uint32_t request_id, uint32_t scene, uint64_t deadline,
    fang_control_editor_tree* snapshot) {
    BYTE* windows[FANG_EDITOR_NODE_LIMIT];
    BYTE* editor;
    BYTE* ui;
    uint32_t queued = 1, cursor = 0, status = 5;
    fang_control_snapshot boundary;
    if (!snapshot || !InterlockedCompareExchange(&is_editor_lock_initialized, 0, 0)) return 5;
    memset(snapshot, 0, sizeof(*snapshot));
    EnterCriticalSection(&editor_lock);
    if (fang_control_editor_ready() != 1) goto done;
    editor = observed_editor;
    if (!readable_range(editor, 860)) goto done;
    ui = *(BYTE**)(editor + 128);
    if (!readable_range(ui, 4) || !readable_range(*(void**)ui, 76)) goto done;
    windows[0] = ((control_editor_find_fn)(*(void***)ui)[18])(ui, 0xffffffffu);
    snapshot->nodes[0].parent_index = 0; /* Indices are one-based, zero means root. */
    snapshot->owner_generation = editor_generation;
    snapshot->is_body_mode = fang_mod_editor_is_body(editor) != 0;
    snapshot->is_mod_editor = fang_mod_editor_active() == editor;
    snapshot->is_import_editor = fang_mod_editor_is_scene(editor) != 0;
    snapshot->is_environment_registered = fang_mod_environment_active(editor) != 0;
    observe_editor_render_groups(editor);
    snapshot->editor_mode = *(uint32_t*)(editor + 856);
    snapshot->is_complete = 1;
    while (cursor < queued) {
        BYTE* window = windows[cursor];
        BYTE** methods;
        BYTE* link = NULL;
        BYTE* finish = NULL;
        const float* area;
        float origin[2], extent[2], width, height;
        uint32_t sibling_count = 0;
        fang_control_editor_node* node = &snapshot->nodes[cursor];
        if (!fang_control_window_editor_current(request_id)) { status = 3; goto done; }
        if (GetTickCount64() >= deadline) { status = 4; goto done; }
        fang_control_read(&boundary);
        if (boundary.scene_load_return_count != scene || observed_editor != editor ||
            snapshot->owner_generation != editor_generation || fang_control_editor_ready() != 1 ||
            !readable_range(window, 4) || !readable_range(*(void**)window, 212)) goto done;
        methods = *(BYTE***)window;
        node->id = ((control_editor_scalar_fn)methods[7])(window);
        node->command = ((control_editor_scalar_fn)methods[8])(window);
        node->flags = ((control_editor_scalar_fn)methods[10])(window);
        area = ((control_editor_area_fn)methods[14])(window);
        if (!readable_range(area, 16)) goto done;
        width = area[2] - area[0]; height = area[3] - area[1];
        ((control_editor_point_fn)methods[48])(window, origin, 0, 0);
        ((control_editor_point_fn)methods[48])(window, extent, width, height);
        if (!isfinite(origin[0]) || !isfinite(origin[1]) || !isfinite(extent[0]) || !isfinite(extent[1])) goto done;
        node->left = origin[0]; node->top = origin[1]; node->right = extent[0]; node->bottom = extent[1];
        snapshot->node_count = cursor + 1;
        ((control_editor_iterator_fn)methods[51])(window, &link);
        ((control_editor_iterator_fn)methods[52])(window, &finish);
        while (link != finish) {
            BYTE* child;
            uint32_t index;
            if (!readable_range(link, 4) || ++sibling_count > FANG_EDITOR_NODE_LIMIT) goto done;
            child = link - 4;
            for (index = 0; index < queued; index++) if (windows[index] == child) goto done;
            if (queued == FANG_EDITOR_NODE_LIMIT || node->depth >= 64) {
                snapshot->is_complete = 0;
                break;
            }
            windows[queued] = child;
            snapshot->nodes[queued].parent_index = cursor + 1;
            snapshot->nodes[queued].depth = node->depth + 1;
            queued++;
            link = *(BYTE**)link;
        }
        cursor++;
    }
    if (observed_editor != editor || snapshot->owner_generation != editor_generation ||
        !fang_control_window_editor_current(request_id)) goto done;
    snapshot->is_available = 1;
    status = GetTickCount64() >= deadline ? 4 : 1;
done:
    if (status != 1) snapshot->is_complete = 0;
    LeaveCriticalSection(&editor_lock);
    return status;
}

/* Cancel is a native SPUI button, not a Flash callback. The retained owned
 * editor tree identifies 90439D7C with command 0x103; normal 73C080 dispatches
 * 0x103 to 73AED0, including its usual unsaved-change/prompt path. Deliver a
 * real finite button click at its current transformed center instead of
 * inventing 73AED0's inherited register/FPU context or calling native teardown. */
uint32_t fang_control_editor_cancel(HWND window, WNDPROC original) {
    BYTE* editor;
    BYTE* ui;
    BYTE* button;
    BYTE* root;
    const float* area;
    float width, height, center[2];
    uint32_t status = 5;
    DWORD process_id = 0;
    RECT client;
    LPARAM point;
    if (!original || !window || !InterlockedCompareExchange(&is_editor_lock_initialized, 0, 0)) return 5;
    EnterCriticalSection(&editor_lock);
    if (fang_control_editor_ready() != 1 || GetWindowThreadProcessId(window, &process_id) != GetCurrentThreadId() ||
        process_id != GetCurrentProcessId() || !IsWindowVisible(window) || !IsWindowEnabled(window) || IsIconic(window) ||
        !GetClientRect(window, &client) || client.left != 0 || client.top != 0 ||
        client.right <= 0 || client.bottom <= 0 || client.right > 32767 || client.bottom > 32767 || GetCapture() ||
        (GetAsyncKeyState(VK_LBUTTON) & 0x8000) || (GetAsyncKeyState(VK_SHIFT) & 0x8000) ||
        (GetAsyncKeyState(VK_CONTROL) & 0x8000) || (GetAsyncKeyState(VK_MENU) & 0x8000) ||
        (GetAsyncKeyState(VK_LWIN) & 0x8000) || (GetAsyncKeyState(VK_RWIN) & 0x8000)) goto done;
    editor = observed_editor;
    ui = *(BYTE**)(editor + 128);
    if (!readable_range(ui, 296) || *(void**)ui != editor_image + 0xBDA8B8) goto done;
    root = ((control_editor_find_fn)(*(void***)ui)[18])(ui, 0xFFFFFFFFu);
    button = ((control_editor_find_fn)(*(void***)ui)[18])(ui, 0x90439D7Cu);
    if (observed_editor != editor || !readable_range(root, 4) || !readable_range(*(void**)root, 196) ||
        !readable_range(button, 4) || !readable_range(*(void**)button, 196)) goto done;
    if ((((control_editor_scalar_fn)(*(void***)root)[10])(root) & 3) != 3 ||
        ((control_editor_scalar_fn)(*(void***)button)[7])(button) != 0x90439D7Cu ||
        ((control_editor_scalar_fn)(*(void***)button)[8])(button) != 0x103 ||
        (((control_editor_scalar_fn)(*(void***)button)[10])(button) & 3) != 3) goto done;
    area = ((control_editor_area_fn)(*(void***)button)[14])(button);
    if (!readable_range(area, 4 * sizeof(float))) goto done;
    width = area[2] - area[0];
    height = area[3] - area[1];
    /* Range comparisons also reject NaN/Inf before conversion to LPARAM. */
    if (!(width >= 4 && width <= 1024 && height >= 4 && height <= 512)) goto done;
    if (!((control_editor_point_fn)(*(void***)button)[48])(button, center, width * 0.5f, height * 0.5f) ||
        !(center[0] >= 0 && center[0] < client.right && center[1] >= 0 && center[1] < client.bottom) ||
        observed_editor != editor || fang_control_editor_ready() != 1) goto done;
    point = MAKELPARAM((WORD)(uint32_t)center[0], (WORD)(uint32_t)center[1]);
    trace_client_state("scenario_editor_cancel_click", 1);
    /* This lock excludes foreign editor teardown. Normal same-thread exit is
     * reentrant; do not touch editor/UI/button pointers after input begins. */
    CallWindowProcA(original, window, WM_MOUSEMOVE, 0, point);
    CallWindowProcA(original, window, WM_LBUTTONDOWN, MK_LBUTTON, point);
    CallWindowProcA(original, window, WM_LBUTTONUP, 0, point);
    status = 1;
done:
    LeaveCriticalSection(&editor_lock);
    return status;
}
#endif

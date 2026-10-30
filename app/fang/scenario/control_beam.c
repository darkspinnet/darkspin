#include "hook.h"
#include "scenario/control.h"
#if FANG_SCENARIO
#include <string.h>

typedef unsigned char (__cdecl* beam_ready_fn)(void);
typedef void (__cdecl* beam_action_fn)(void);
typedef void* (FANG_THISCALL* beam_root_fn)(void*);
typedef void* (FANG_THISCALL* beam_modal_fn)(void*, int);

/* Build103 HUD41E8F0 registers MaxisBeamOut.OnBeamOutClicked ->41B9F0.
 * That stateless, argument-free thunk jumps4E4F80, which sends the ordinary
 * status request via538C30 and latches root+15784. No packet is constructed
 * here. HUD41B9A0 uses4E0430&&!4E4FB0 after the native death presentation. */
// Reads: verifies installed build-103 beam-out code and callback registration signatures.
static int is_beam_supported(BYTE* base) {
    static const BYTE thunk[] = {0xE9,0x8B,0x95,0x0C,0x00};
    static const BYTE ready[] = {0xA1,0x20,0x8B,0x43,0x01,0x8A,0x80,0xAB,0x00,0x00,0x00,0xC3};
    static const BYTE requested[] = {0xA1,0xB0,0x8C,0x43,0x01,0x8A,0x80,0xA8,0x3D,0x00,0x00,0xC3};
    static const BYTE action[] = {
        0xA1,0xB0,0x8C,0x43,0x01,0x80,0xB8,0xA8,0x3D,0x00,0x00,0x00,0x75,0x1D,
        0xD9,0xE8,0x51,0xD9,0x1C,0x24,0x6A,0x20,0xE8,0x95,0x3C,0x05,0x00,
        0x8B,0x0D,0xB0,0x8C,0x43,0x01,0x83,0xC4,0x08,0xC6,0x81,0xA8,0x3D,0x00,0x00,0x01,0xC3};
    static const BYTE registration[] = {0x8B,0xC8,0x8B,0x42,0x1C,0x56,0xFF,0xD0};
    return base && readable_range(base + 0x1B9F0, sizeof(thunk)) &&
        readable_range(base + 0xE0430, sizeof(ready)) && readable_range(base + 0xE4FB0, sizeof(requested)) &&
        readable_range(base + 0xE4F80, sizeof(action)) && readable_range(base + 0x1E8FC, 18) &&
        readable_range(base + 0xBCED24, sizeof("MaxisBeamOut.OnBeamOutClicked")) &&
        !memcmp(base + 0x1B9F0, thunk, sizeof(thunk)) &&
        !memcmp(base + 0xE0430, ready, sizeof(ready)) &&
        !memcmp(base + 0xE4FB0, requested, sizeof(requested)) &&
        !memcmp(base + 0xE4F80, action, sizeof(action)) &&
        base[0x1E8FC] == 0x68 && *(void**)(base + 0x1E8FD) == base + 0x1B9F0 &&
        base[0x1E901] == 0x68 && *(void**)(base + 0x1E902) == base + 0xBCED24 &&
        !memcmp(base + 0x1E906, registration, sizeof(registration)) &&
        !strcmp((char*)base + 0xBCED24, "MaxisBeamOut.OnBeamOutClicked");
}

// Reads: checks the native scene, window, root/director, input, and modal context for beam-out.
static int is_beam_context_current(BYTE* base, uint32_t scene) {
    fang_control_snapshot boundary;
    fang_control_window_snapshot window;
    BYTE* root = *(BYTE**)(base + 0x1038CB0);
    BYTE* director = *(BYTE**)(base + 0x1038B20);
    BYTE* manager = *(BYTE**)(base + 0x105EDCC);
    void* modal;
    void* ui_root;
    fang_control_read(&boundary);
    if (boundary.scene_load_return_count != scene || boundary.frame_thread_id != GetCurrentThreadId() ||
        !boundary.frame_tick_ms || GetTickCount64() - boundary.frame_tick_ms > 1000 ||
        !fang_control_window_inspect(&window) || window.thread_id != GetCurrentThreadId() ||
        !window.is_enabled || window.is_minimized || !readable_range(root, 15785) ||
        !readable_range(*(void**)(root + 4), 33500) || !readable_range(director, 172) ||
        !readable_range(manager, 4) || !readable_range(*(void**)manager, 76) || GetCapture() ||
        (GetAsyncKeyState(VK_LBUTTON) & 0x8000) || (GetAsyncKeyState(VK_RBUTTON) & 0x8000)) return 0;
    modal = ((beam_modal_fn)(*(void***)manager)[18])(manager, 0);
    ui_root = ((beam_root_fn)(*(void***)manager)[1])(manager);
    return ui_root && modal == ui_root;
}

// Reads/modifies: fills beam-out readiness and optionally invokes the native request/latch path, logging the result.
uint32_t fang_control_beam_out(uint32_t scene, uint64_t deadline, int is_action, fang_control_beam_state* snapshot) {
    BYTE* base = (BYTE*)GetModuleHandleW(NULL);
    fang_control_snapshot boundary;
    if (!snapshot) return 5;
    memset(snapshot, 0, sizeof(*snapshot));
    if (!is_beam_supported(base) || !is_beam_context_current(base, scene)) return 5;
    snapshot->is_available = 1;
    snapshot->is_requested = ((beam_ready_fn)(base + 0xE4FB0))() != 0;
    snapshot->is_ready = ((beam_ready_fn)(base + 0xE0430))() != 0 && !snapshot->is_requested;
    if (!is_action) return 1;
    if (!snapshot->is_ready) return 5;
    fang_control_read(&boundary);
    if (GetTickCount64() >= deadline) return 4;
    if (boundary.scene_load_return_count != scene || !is_beam_context_current(base, scene) ||
        !((beam_ready_fn)(base + 0xE0430))() || ((beam_ready_fn)(base + 0xE4FB0))()) return 5;
    ((beam_action_fn)(base + 0x1B9F0))();
    /* The native request latch confirms only the local request was issued.
     * Personal victory, rewards and result commitment remain server facts. */
    fang_control_read(&boundary);
    if (boundary.scene_load_return_count != scene || !is_beam_context_current(base, scene)) {
        snapshot->is_available = 0;
        snapshot->is_ready = 0;
        return 6;
    }
    snapshot->is_requested = ((beam_ready_fn)(base + 0xE4FB0))() != 0;
    snapshot->is_ready = ((beam_ready_fn)(base + 0xE0430))() != 0 && !snapshot->is_requested;
    trace_client_state("scenario_beam_out_request", snapshot->is_requested);
    return snapshot->is_requested ? 1 : 6;
}
#endif

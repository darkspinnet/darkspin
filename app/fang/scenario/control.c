#include "scenario/control.h"
#if FANG_SCENARIO
#include <windows.h>
static SRWLOCK control_lock = SRWLOCK_INIT;
static fang_control_snapshot control_snapshot;

/* Copy owned scalars only. No native object is retained or read here. */
// Modifies: records the Fang frame boundary and advances pending gameplay requests, which may invoke native actions.
void fang_control_frame(uint32_t sequence) {
    AcquireSRWLockExclusive(&control_lock);
    control_snapshot.frame_sequence = sequence;
    control_snapshot.frame_thread_id = GetCurrentThreadId();
    control_snapshot.frame_tick_ms = GetTickCount64();
    ReleaseSRWLockExclusive(&control_lock);
    fang_control_gameplay_frame(sequence);
}

// Modifies: records the scene asset, load count, frame, and timestamp in the Fang snapshot.
void fang_control_scene(uint32_t asset_id, uint32_t frame_sequence) {
    AcquireSRWLockExclusive(&control_lock);
    control_snapshot.scene_asset_id = asset_id;
    control_snapshot.scene_load_return_count++;
    control_snapshot.scene_frame_sequence = frame_sequence;
    control_snapshot.scene_tick_ms = GetTickCount64();
    ReleaseSRWLockExclusive(&control_lock);
}

// Reads/modifies: copies the locked Fang control snapshot into the caller's output.
void fang_control_read(fang_control_snapshot* snapshot) {
    AcquireSRWLockShared(&control_lock);
    *snapshot = control_snapshot;
    ReleaseSRWLockShared(&control_lock);
}
#endif

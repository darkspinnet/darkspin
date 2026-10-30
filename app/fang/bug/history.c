#include "bug/history.h"

#include <stdio.h>
#include <string.h>

/* Reuse the build-103 snapshot readers on the game thread. One bounded JSON
 * record per sample avoids multiplying the scalar trace's per-field I/O.
 * These samples observe native state; they never refresh or reset it. */
static struct {
    volatile LONG owner;
    DWORD last_time;
    char line[8192];
    unsigned int length;
    unsigned int fields;
    int is_truncated;
} history;

// Modifies: reserves a rate-limited history sample and initializes its JSON buffer.
int fang_history_begin(unsigned int frame, unsigned int delta_bits, unsigned int frame_time) {
    DWORD now = GetTickCount();
    int length;
    if (InterlockedCompareExchange(&history.owner, (LONG)GetCurrentThreadId(), 0) != 0) return 0;
    if (now - history.last_time < 100u) {
        InterlockedExchange(&history.owner, 0);
        return 0;
    }
    history.last_time = now;
    history.fields = 0;
    history.is_truncated = 0;
    length = snprintf(history.line, sizeof(history.line),
        "{\"time_ms\":%lu,\"protocol\":\"client_state\",\"kind\":\"control_history\","
        "\"thread\":%lu,\"frame_sequence\":%u,\"frame_delta_bits\":%u,"
        "\"frame_time_ms\":%u,\"states\":{",
        (unsigned long)now, (unsigned long)GetCurrentThreadId(), frame, delta_bits, frame_time);
    if (length <= 0 || (size_t)length >= sizeof(history.line) - 64) {
        InterlockedExchange(&history.owner, 0);
        return 0;
    }
    history.length = (unsigned int)length;
    return 1;
}

// Modifies: appends a field to the owned history sample or marks it truncated.
int fang_history_collect(const char* kind, unsigned int value) {
    char field[160];
    int length;
    if ((DWORD)InterlockedCompareExchange(&history.owner, 0, 0) != GetCurrentThreadId()) return 0;
    if (history.is_truncated) return 1;
    length = snprintf(field, sizeof(field), "%s\"%s\":%u",
        history.fields == 0 ? "" : ",", kind, value);
    if (length <= 0 || (size_t)length >= sizeof(field) ||
        history.length + (unsigned int)length >= sizeof(history.line) - 64) {
        history.is_truncated = 1;
        return 1;
    }
    memcpy(history.line + history.length, field, (size_t)length);
    history.length += (unsigned int)length;
    history.fields++;
    return 1;
}

// Modifies: finishes and writes the owned history sample, then releases sample ownership.
void fang_history_end(void) {
    int length;
    if ((DWORD)InterlockedCompareExchange(&history.owner, 0, 0) != GetCurrentThreadId()) return;
    length = snprintf(history.line + history.length, sizeof(history.line) - history.length,
        "},\"is_truncated\":%s}\r\n", history.is_truncated ? "true" : "false");
    if (length > 0 && (size_t)length < sizeof(history.line) - history.length) {
        fang_history_write(history.line, history.length + (unsigned int)length);
    }
    InterlockedExchange(&history.owner, 0);
}

// Modifies: records selected mouse and focus window messages in control history.
void fang_history_input(UINT message, WPARAM wparam) {
    char line[384];
    unsigned int buttons = 0;
    int length;
    switch (message) {
    case WM_LBUTTONDOWN: case WM_LBUTTONUP: case WM_LBUTTONDBLCLK:
    case WM_RBUTTONDOWN: case WM_RBUTTONUP: case WM_RBUTTONDBLCLK:
    case WM_MBUTTONDOWN: case WM_MBUTTONUP: case WM_MBUTTONDBLCLK:
    case WM_XBUTTONDOWN: case WM_XBUTTONUP: case WM_XBUTTONDBLCLK:
        buttons = LOWORD(wparam);
        break;
    case WM_SETFOCUS: case WM_KILLFOCUS: case WM_CAPTURECHANGED: case WM_CANCELMODE:
        break;
    default:
        return;
    }
    /* Observe delivery to the game window, not global keyboard activity or
     * proof that gameplay consumed the input. Never record text/chat keys. */
    length = snprintf(line, sizeof(line),
        "{\"time_ms\":%lu,\"protocol\":\"client_state\",\"kind\":\"control_input\","
        "\"thread\":%lu,\"states\":{\"window_message\":%u,\"mouse_buttons\":%u}}\r\n",
        (unsigned long)GetTickCount(), (unsigned long)GetCurrentThreadId(), message, buttons);
    if (length > 0 && (size_t)length < sizeof(line)) fang_history_write(line, (unsigned int)length);
}

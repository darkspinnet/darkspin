#include "hook.h"

#include <stdint.h>
#include <stdio.h>

/* Snapshot-only observations of build 103. Do not call collection methods
 * from the control thread: the Arsenal does not tick the gameplay clock. */
static int arsenal_read(uintptr_t address, void* output, size_t size) {
    SIZE_T copied = 0;
    if (address == 0 || address > UINTPTR_MAX - size) {
        return 0;
    }
    return ReadProcessMemory(GetCurrentProcess(), (const void*)address,
        output, size, &copied) && copied == size;
}

static void arsenal_vector_count(const char* kind, unsigned int begin,
    unsigned int end, unsigned int stride) {
    if (end < begin || (end - begin) % stride != 0 ||
        (end - begin) / stride > 1024) {
        trace_client_state("arsenal_invalid_vector", 1);
        return;
    }
    trace_client_state(kind, (end - begin) / stride);
}

void fang_trace_arsenal_snapshot(HMODULE executable) {
    uintptr_t base = (uintptr_t)executable;
    unsigned int controller = 0;
    unsigned int current_controller = 0;
    unsigned int states[0xD10 / 4];
    unsigned int root = 0;
    unsigned int catalog_count = 0;
    unsigned int catalog_bounds[2];
    unsigned int nouns[256];
    unsigned int count;
    unsigned int index;
    char kind[64];

    trace_client_state("arsenal_snapshot_boundary", 1);
    if (executable == NULL || sizeof(uintptr_t) != 4) {
        trace_client_state("arsenal_layout_unsupported", 1);
        return;
    }
    /* sub_41ACA0 publishes this pointer; sub_419140 and the arrow handlers
     * use its filtered noun vector and scroll index. Exit clears the pointer. */
    if (!arsenal_read(base + 0x7BEAA0, &controller, sizeof(controller))) {
        trace_client_state("arsenal_controller_unreadable", 1);
        return;
    }
    trace_client_state("arsenal_is_open", controller != 0);
    if (controller == 0) {
        return;
    }
    if (!arsenal_read(controller, states, sizeof(states)) ||
        states[0] != base + 0xBCE048 ||
        !arsenal_read(base + 0x7BEAA0, &current_controller,
            sizeof(current_controller)) || current_controller != controller) {
        trace_client_state("arsenal_controller_unstable", 1);
        return;
    }

    /* These are sampled observations, not an atomic game-thread snapshot.
     * ReadProcessMemory tolerates the UI closing while the worker reads it. */
    arsenal_vector_count("arsenal_filtered_count", states[1], states[2], 4);
    arsenal_vector_count("arsenal_placard_count", states[232], states[233], 72);
    trace_client_state("arsenal_scroll_index", states[155]);
    trace_client_state("arsenal_filter", states[151]);
    trace_client_state("arsenal_show_locked", (states[152] & 0xFF) != 0);
    trace_client_state("arsenal_arrow_direction", states[192]);
    trace_client_state("arsenal_arrow_held", (states[194] & 0xFF) != 0);

    /* sub_4E4E20 returns root + 1808. Its catalogue hash count is at +580;
     * the ordered catalogue used by sub_41ACA0 is at +10324/+10328. */
    if (arsenal_read(base + 0x1038CB0, &root, sizeof(root)) && root != 0) {
        if (arsenal_read((uintptr_t)root + 2388, &catalog_count,
                sizeof(catalog_count))) {
            trace_client_state("arsenal_catalog_count", catalog_count);
        }
        if (arsenal_read((uintptr_t)root + 12132, catalog_bounds,
                sizeof(catalog_bounds))) {
            arsenal_vector_count("arsenal_ordered_count",
                catalog_bounds[0], catalog_bounds[1], 4);
        }
    }

    if (states[2] < states[1] || (states[2] - states[1]) % 4 != 0) {
        return;
    }
    count = (states[2] - states[1]) / 4;
    if (count == 0 || count > 256 ||
        !arsenal_read(states[1], nouns, count * sizeof(nouns[0]))) {
        return;
    }
    for (index = 0; index < count; ++index) {
        int length = snprintf(kind, sizeof(kind), "arsenal_filtered_noun_%u", index);
        if (length > 0 && (size_t)length < sizeof(kind)) {
            trace_client_state(kind, nouns[index]);
        }
    }
}

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

static void arsenal_slot_field(unsigned int index, const char* field,
    unsigned int number) {
    char kind[96];
    int length = snprintf(kind, sizeof(kind), "arsenal_slot_%u_%s", index, field);
    if (length > 0 && (size_t)length < sizeof(kind)) {
        trace_client_state(kind, number);
    }
}

static void arsenal_slots(const unsigned int* states) {
    /* sub_414C10 builds an EASTL deque of 112-byte records, four per block.
     * sub_410BA0 starts the loader; sub_410C90 replaces it with a model ID.
     * Never call either function here or dereference live container pointers. */
    uintptr_t slot = states[129];
    uintptr_t first = states[130];
    uintptr_t last = states[131];
    uintptr_t node = states[132];
    uintptr_t finish = states[133];
    unsigned int records[28];
    unsigned int loaders[10];
    unsigned int next_block;
    unsigned int index;
    for (index = 0; index < 32 && slot != finish; ++index) {
        if (last < first || last - first != 448 || slot < first ||
            slot >= last || (slot - first) % 112 != 0 ||
            !arsenal_read(slot, records, sizeof(records))) {
            trace_client_state("arsenal_slots_unreadable", index);
            return;
        }
        arsenal_slot_field(index, "noun_resource", records[1]);
        arsenal_slot_field(index, "creature_id_low", records[2]);
        arsenal_slot_field(index, "creature_id_high", records[3]);
        arsenal_slot_field(index, "model_id", records[12]);
        arsenal_slot_field(index, "position_index", records[14]);
        arsenal_slot_field(index, "is_suppressed", (records[26] & 0xFF) != 0);
        arsenal_slot_field(index, "has_loader", records[0] != 0);
        if (records[0] != 0) {
            if (arsenal_read(records[0], loaders, sizeof(loaders))) {
                /* Async request and resolved-resource presence, matching
                 * sub_4576A0/0x457450. No readiness virtual calls off-thread. */
                arsenal_slot_field(index, "has_request_1", loaders[1] != 0);
                arsenal_slot_field(index, "has_resource_1", loaders[2] != 0);
                arsenal_slot_field(index, "has_request_2", loaders[3] != 0);
                arsenal_slot_field(index, "has_resource_2", loaders[4] != 0);
                arsenal_slot_field(index, "has_request_3", loaders[7] != 0);
                arsenal_slot_field(index, "has_resource_3", loaders[8] != 0);
                arsenal_slot_field(index, "has_resource_4", loaders[9] != 0);
            } else {
                arsenal_slot_field(index, "loader_unreadable", 1);
            }
        }
        slot += 112;
        if (slot == last) {
            if (node > UINTPTR_MAX - 4 ||
                !arsenal_read(node + 4, &next_block, sizeof(next_block)) ||
                next_block == 0 || (uintptr_t)next_block > UINTPTR_MAX - 448) {
                trace_client_state("arsenal_slots_next_block_unreadable", index);
                return;
            }
            node += 4;
            first = next_block;
            last = first + 448;
            slot = first;
        }
    }
    trace_client_state("arsenal_slot_count", index);
    trace_client_state("arsenal_slots_truncated", slot != finish);
}

void fang_trace_arsenal_snapshot(HMODULE executable) {
    uintptr_t base = (uintptr_t)executable;
    /* Keep the decompiler's virtual addresses explicit to avoid confusing
     * 0x011BEAA0 with 0x00BBEAA0 when converting to an image-relative offset. */
    uintptr_t controller_address = base + (0x011BEAA0u - 0x00400000u);
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
    /* Capture the catalogue even if the collection UI is absent or changing.
     * sub_4E4E20 returns root + 1808. Its catalogue hash count is at +580;
     * the ordered catalogue used by sub_41ACA0 is at +10324/+10328. */
    if (arsenal_read(base + (0x01438CB0u - 0x00400000u),
            &root, sizeof(root)) && root != 0) {
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
    /* sub_41ACA0 publishes this pointer; sub_419140 and the arrow handlers
     * use its filtered noun vector and scroll index. Exit clears the pointer. */
    if (!arsenal_read(controller_address, &controller, sizeof(controller))) {
        trace_client_state("arsenal_controller_unreadable", 1);
        return;
    }
    trace_client_state("arsenal_is_open", controller != 0);
    if (controller == 0) {
        return;
    }
    if (!arsenal_read(controller, states, sizeof(states))) {
        trace_client_state("arsenal_state_unreadable", 1);
        return;
    }
    if (states[0] != base + (0x00FCE048u - 0x00400000u)) {
        trace_client_state("arsenal_vtable_mismatch", 1);
        return;
    }
    if (!arsenal_read(controller_address, &current_controller,
            sizeof(current_controller))) {
        trace_client_state("arsenal_controller_recheck_failed", 1);
        return;
    }
    if (current_controller != controller) {
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
    arsenal_slots(states);

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

#include "scenario_observation.h"

#if FANG_SCENARIO
#include <stdio.h>
#include <windows.h>

static volatile LONG observation_sequence;
static volatile LONG scene_load_return_count;
static volatile LONG object_message_construct_count;

unsigned long fang_scenario_message_count(void) {
    return (unsigned long)InterlockedCompareExchange(
        &object_message_construct_count, 0, 0);
}

int fang_scenario_scene_load_line(char* line, size_t capacity,
    unsigned int asset_id, unsigned int load_argument_0, unsigned int load_argument_1,
    unsigned long long time_ms, unsigned long frame, unsigned long thread) {
    unsigned long sequence = (unsigned long)InterlockedIncrement(&observation_sequence);
    unsigned long count = (unsigned long)InterlockedIncrement(&scene_load_return_count);
    return snprintf(line, capacity,
        "{\"time_ms\":%llu,\"protocol\":\"client_state\","
        "\"kind\":\"scenario_scene_load_return\",\"observation_sequence\":%lu,"
        "\"scene_load_return_count\":%lu,\"asset_id\":%u,"
        "\"load_argument_0\":%u,\"load_argument_1\":%u,\"frame_sequence\":%lu,"
        "\"thread\":%lu,\"is_materialization_complete\":null}\r\n",
        time_ms, sequence, count, asset_id, load_argument_0, load_argument_1, frame, thread);
}

int fang_scenario_message_line(char* line, size_t capacity,
    unsigned int message_id, unsigned long long time_ms,
    unsigned long frame, unsigned long thread) {
    unsigned long sequence = (unsigned long)InterlockedIncrement(&observation_sequence);
    unsigned long count = (unsigned long)InterlockedIncrement(&object_message_construct_count);
    return snprintf(line, capacity,
        "{\"time_ms\":%llu,\"protocol\":\"client_state\","
        "\"kind\":\"scenario_object_message_construct\",\"observation_sequence\":%lu,"
        "\"object_message_construct_count\":%lu,\"message_id\":%u,"
        "\"frame_sequence\":%lu,\"thread\":%lu,\"is_mutation_observed\":null}\r\n",
        time_ms, sequence, count, message_id, frame, thread);
}

int fang_scenario_registry_line(char* line, size_t capacity,
    const char* status, unsigned long long started_time_ms,
    unsigned long long completed_time_ms, unsigned long frame,
    unsigned int object_count, unsigned int unreadable_slot_count,
    unsigned int changed_handle_count, unsigned long started_message_count) {
    unsigned long sequence = (unsigned long)InterlockedIncrement(&observation_sequence);
    return snprintf(line, capacity,
        "{\"time_ms\":%llu,\"protocol\":\"client_state\","
        "\"kind\":\"scenario_registry\",\"frame_sequence\":%lu,"
        "\"registry_status\":\"%s\",\"registry_started_time_ms\":%llu,"
        "\"registry_completed_time_ms\":%llu,\"object_count\":%u,"
        "\"unreadable_slot_count\":%u,\"changed_handle_count\":%u,"
        "\"observation_sequence\":%lu,\"scene_load_return_count\":%lu,"
        "\"started_object_message_count\":%lu,\"completed_object_message_count\":%lu}\r\n",
        completed_time_ms, frame, status, started_time_ms, completed_time_ms,
        object_count, unreadable_slot_count, changed_handle_count, sequence,
        (unsigned long)InterlockedCompareExchange(&scene_load_return_count, 0, 0),
        started_message_count, fang_scenario_message_count());
}
#endif

#ifndef DARKSPIN_SCENARIO_OBSERVATION_H
#define DARKSPIN_SCENARIO_OBSERVATION_H

#include <stddef.h>

#ifndef FANG_SCENARIO
#define FANG_SCENARIO 0
#endif

#if FANG_SCENARIO
// These process-lifetime 32-bit counters identify observation order only;
// wrap is not a new epoch. A scene-load return does not establish
// materialization; message construction is not dispatch.
unsigned long fang_scenario_message_count(void);
int fang_scenario_scene_load_line(char* line, size_t capacity,
    unsigned int asset_id, unsigned int load_argument_0, unsigned int load_argument_1,
    unsigned long long time_ms, unsigned long frame, unsigned long thread);
int fang_scenario_message_line(char* line, size_t capacity,
    unsigned int message_id, unsigned long long time_ms,
    unsigned long frame, unsigned long thread);
// Serializes metadata only. All object reads remain at the existing frame
// keyframe observation point in fang.c.
int fang_scenario_registry_line(char* line, size_t capacity,
    const char* status, unsigned long long started_time_ms,
    unsigned long long completed_time_ms, unsigned long frame,
    unsigned int object_count, unsigned int unreadable_slot_count,
    unsigned int changed_handle_count, unsigned long started_message_count);
#endif

#endif

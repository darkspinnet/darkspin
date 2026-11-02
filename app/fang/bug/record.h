#ifndef DARKSPIN_FANG_RECORD_H
#define DARKSPIN_FANG_RECORD_H

#include "fang.h"

/* Core-owned projections. Each frame field keeps its original atomic read. */
int fang_record_trace_available(void);
BOOL fang_record_trace_write(const char* contents, DWORD length, DWORD* written);
const char* fang_record_directory(void);
LONG fang_record_frame_sequence(void);
LONG fang_record_frame_delta_bits(void);
LONG fang_record_frame_time_ms(void);
LONG fang_record_keyframe_started_time_ms(void);
LONG fang_record_keyframe_completed_time_ms(void);

void snapshot_ring_append(const char* contents, unsigned int size);
void snapshot_ring_clear(void);
void snapshot_ring_marker(const char* kind, const char* request,
    unsigned long long server_time_unix_nano, unsigned int buffer_ms,
    const char* keyframe_status, unsigned long long client_received_time_ms);
int valid_snapshot_directory_name(const char* name);
void snapshot_ring_dump(const char* directory_name, unsigned int buffer_ms);
void trace_hex_line(const char* header, int header_length,
    const BYTE* data, unsigned int size, const char* suffix);

#endif

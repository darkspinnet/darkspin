#ifndef DARKSPIN_SCENARIO_THREAD_H
#define DARKSPIN_SCENARIO_THREAD_H

#ifndef FANG_SCENARIO
#define FANG_SCENARIO 0
#endif

#if FANG_SCENARIO
#include <windows.h>
#include <stdint.h>

/* Fixed inline storage avoids cross-process pointers in the thread argument. */
typedef struct fang_scenario_capability_request {
    uint32_t protocol_version;
    uint32_t capacity;
    int32_t required;
    uint32_t reserved;
    unsigned char payload[4096];
} fang_scenario_capability_request;

typedef char fang_scenario_capability_size[(sizeof(fang_scenario_capability_request) == 4112) ? 1 : -1];

__declspec(dllexport) DWORD WINAPI RecapScenarioCapabilityThread(LPVOID parameter);
#endif
#endif

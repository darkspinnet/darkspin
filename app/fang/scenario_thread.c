#include "scenario_thread.h"

#if FANG_SCENARIO
extern int GoScenarioCapability(unsigned char* destination, unsigned int capacity);

/* This thread only reports compiled metadata; it performs no client action. */
__declspec(dllexport) DWORD WINAPI RecapScenarioCapabilityThread(LPVOID parameter) {
    fang_scenario_capability_request* request =
        (fang_scenario_capability_request*)parameter;
    if (request == NULL || request->protocol_version != 1 ||
        request->capacity != sizeof(request->payload) || request->reserved != 0) {
        return 1;
    }
    request->required = GoScenarioCapability(request->payload, request->capacity);
    if (request->required <= 0 ||
        (unsigned int)request->required > request->capacity) {
        return 2;
    }
    return 0;
}
#endif

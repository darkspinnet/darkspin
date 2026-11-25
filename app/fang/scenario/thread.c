#include "scenario/thread.h"

#if FANG_SCENARIO
#include <string.h>
#include "fang_generated.h"

/* This thread only reports compiled metadata; it performs no client action. */
// Modifies: validates the request and writes compiled capability metadata and required length into its output.
__declspec(dllexport) DWORD WINAPI RecapScenarioCapabilityThread(LPVOID parameter) {
    fang_scenario_capability_request* request =
        (fang_scenario_capability_request*)parameter;
    if (request == NULL || request->protocol_version != 1 ||
        request->capacity != sizeof(request->payload) || request->reserved != 0) {
        return 1;
    }
    request->required = sizeof(FANG_CAPABILITY_JSON);
    if (request->required <= request->capacity) memcpy(request->payload, FANG_CAPABILITY_JSON, request->required);
    if (request->required <= 0 ||
        (unsigned int)request->required > request->capacity) {
        return 2;
    }
    return 0;
}
#endif

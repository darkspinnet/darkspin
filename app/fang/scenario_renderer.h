#ifndef DARKSPIN_SCENARIO_RENDERER_H
#define DARKSPIN_SCENARIO_RENDERER_H

#ifndef FANG_SCENARIO
#define FANG_SCENARIO 0
#endif

#if FANG_SCENARIO
// Returns true only when both supported discovery call sites are observed.
// Successful observations do not cover later caps, display or depth checks.
int install_scenario_renderer_observation(void* executable);
#endif

#endif

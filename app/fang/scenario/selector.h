#ifndef DARKSPIN_SCENARIO_SELECTOR_H
#define DARKSPIN_SCENARIO_SELECTOR_H

#ifndef FANG_SCENARIO
#define FANG_SCENARIO 0
#endif

#if FANG_SCENARIO
// Coverage observes the supported scalar lookup, not selected entry admission,
// materialization, inventory completeness or object lifetime.
int install_scenario_selector_observation(void* executable);
#endif

#endif

#ifndef DARKSPIN_SCENARIO_DISPATCH_H
#define DARKSPIN_SCENARIO_DISPATCH_H

#ifndef FANG_SCENARIO
#define FANG_SCENARIO 0
#endif

#if FANG_SCENARIO
// Coverage means these two supported calls are observed, not that all native
// mutation paths or object lifetimes are known.
int install_scenario_dispatch_observation(void* executable);
void observe_scenario_dispatch_frame(void);
#endif

#endif

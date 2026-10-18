#include "scenario_renderer.h"

#if FANG_SCENARIO
#include "hook.h"
#include <d3d9.h>

typedef IDirect3D9* (WINAPI* scenario_direct3d_factory_fn)(UINT sdk_version);
typedef BOOL (WINAPI* scenario_d3dx_version_fn)(UINT d3d_sdk_version,
    UINT d3dx_sdk_version);

static scenario_direct3d_factory_fn original_scenario_direct3d_factory;
static scenario_d3dx_version_fn original_scenario_d3dx_version;

static IDirect3D9* WINAPI observe_scenario_direct3d_factory(UINT sdk_version) {
    IDirect3D9* renderer = original_scenario_direct3d_factory(sdk_version);
    DWORD last_error = GetLastError();
    trace_client_state("scenario_renderer_factory_nonnull", renderer != NULL);
    SetLastError(last_error);
    return renderer;
}

static BOOL WINAPI observe_scenario_d3dx_version(UINT d3d_sdk_version,
    UINT d3dx_sdk_version) {
    BOOL is_compatible = original_scenario_d3dx_version(
        d3d_sdk_version, d3dx_sdk_version);
    DWORD last_error = GetLastError();
    // Retain the original BOOL bits instead of normalizing a nonzero result.
    trace_client_state("scenario_renderer_version_result",
        (unsigned int)is_compatible);
    SetLastError(last_error);
    return is_compatible;
}

int install_scenario_renderer_observation(void* executable) {
    DWORD last_error = GetLastError();
    unsigned char* base = (unsigned char*)executable;
    int is_factory_hooked = 0;
    int is_version_hooked = 0;
    if (base != NULL) {
        original_scenario_direct3d_factory =
            (scenario_direct3d_factory_fn)(base + 0xA8EB88);
        original_scenario_d3dx_version =
            (scenario_d3dx_version_fn)(base + 0xA8EB8E);
        is_factory_hooked = patch_call(base + 0x912B7E,
            (void*)original_scenario_direct3d_factory,
            (void*)observe_scenario_direct3d_factory);
        is_version_hooked = patch_call(base + 0x912B92,
            (void*)original_scenario_d3dx_version,
            (void*)observe_scenario_d3dx_version);
    }
    trace_client_state("scenario_renderer_factory_hook",
        (unsigned int)is_factory_hooked);
    trace_client_state("scenario_renderer_version_hook",
        (unsigned int)is_version_hooked);
    trace_client_state("scenario_renderer_hook_count",
        (unsigned int)(is_factory_hooked + is_version_hooked));
    SetLastError(last_error);
    return is_factory_hooked && is_version_hooked;
}
#endif

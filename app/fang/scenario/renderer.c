#include "scenario/renderer.h"

#if FANG_SCENARIO
#include "hook.h"
#include <d3d9.h>

typedef IDirect3D9* (WINAPI* scenario_direct3d_factory_fn)(UINT sdk_version);
typedef BOOL (WINAPI* scenario_d3dx_version_fn)(UINT d3d_sdk_version,
    UINT d3dx_sdk_version);

static scenario_direct3d_factory_fn original_scenario_direct3d_factory;
static scenario_d3dx_version_fn original_scenario_d3dx_version;
typedef HRESULT (WINAPI* scenario_create_device_fn)(IDirect3D9*, UINT, D3DDEVTYPE,
    HWND, DWORD, D3DPRESENT_PARAMETERS*, IDirect3DDevice9**);
static scenario_create_device_fn original_create_device;

// Modifies: forwards native device creation and output updates, logs its result, and installs draw hooks on a created device.
static HRESULT WINAPI observe_create_device(IDirect3D9* renderer, UINT adapter,
    D3DDEVTYPE type, HWND window, DWORD behavior, D3DPRESENT_PARAMETERS* presentation,
    IDirect3DDevice9** device) {
    HRESULT result = original_create_device(renderer, adapter, type, window, behavior, presentation, device);
    DWORD last_error = GetLastError();
    trace_client_state("scenario_editor_draw_device_result", (unsigned int)result);
    if (SUCCEEDED(result) && device && *device) observe_scenario_draw_device(*device);
    SetLastError(last_error);
    return result;
}

// Modifies: forwards native renderer creation, logs it, and patches its CreateDevice function pointer.
static IDirect3D9* WINAPI observe_scenario_direct3d_factory(UINT sdk_version) {
    IDirect3D9* renderer = original_scenario_direct3d_factory(sdk_version);
    DWORD last_error = GetLastError();
    trace_client_state("scenario_renderer_factory_nonnull", renderer != NULL);
    if (renderer && readable_range(renderer->lpVtbl, sizeof(*renderer->lpVtbl)) &&
        renderer->lpVtbl->CreateDevice != observe_create_device) {
        scenario_create_device_fn create = renderer->lpVtbl->CreateDevice;
        if (!original_create_device || original_create_device == create) {
            original_create_device = create;
            trace_client_state("scenario_editor_draw_factory_hook", patch_pointer(
                (void**)&renderer->lpVtbl->CreateDevice, (void*)create, (void*)observe_create_device));
        }
    }
    SetLastError(last_error);
    return renderer;
}

// Reads/modifies: forwards the native compatibility query and logs its unchanged result while preserving LastError.
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

// Modifies: binds renderer helpers, patches factory/version calls, and logs installation results.
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

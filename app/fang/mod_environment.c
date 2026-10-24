#include "hook.h"

#include <stdint.h>

typedef BYTE* (__cdecl* environment_manager_fn)(void);
typedef unsigned char (FANG_THISCALL* environment_read_fn)(BYTE*, const uint32_t*, BYTE**,
    unsigned int, unsigned int, unsigned int, unsigned int);
typedef unsigned char (FANG_THISCALL* environment_directory_fn)(BYTE*, BYTE*);
typedef void (FANG_THISCALL* environment_ref_fn)(BYTE*);
typedef BYTE* (FANG_THISCALL* environment_find_fn)(BYTE*, uint32_t);
typedef void (FANG_THISCALL* environment_stop_fn)(BYTE*, uint32_t, int);

static BYTE* environment_base;
static BYTE* environment_owner;
static BYTE* environment_resource;
static BYTE* environment_directory;
static BYTE* environment_service;

int fang_mod_environment_active(BYTE* editor) {
    return editor && editor == environment_owner && environment_directory;
}

void fang_mod_environment_leave(BYTE* editor) {
    BYTE* manager;
    if (!fang_mod_environment_active(editor)) return;
    /* 663AF0 hard-stops this named effect and erases its retained instance.
     * CF1030 then unregisters the directory and invalidates cached lookups.
     * The resource reference is held until both operations have completed. */
    manager = ((environment_manager_fn)(environment_base + 0x264290))();
    ((environment_stop_fn)(environment_base + 0x263af0))(manager, FANG_EDITOR_ENV_ENTRY, 1);
    if (!((environment_directory_fn)(*(void***)environment_service)[25])(
        environment_service, environment_directory))
        trace_client_state("mod_editor_environment_unregister_failed", 1);
    ((environment_ref_fn)(*(void***)environment_resource)[1])(environment_resource);
    environment_owner = NULL;
    environment_resource = NULL;
    environment_directory = NULL;
    environment_service = NULL;
    trace_client_state("mod_editor_environment_active", 0);
}

int fang_mod_environment_prepare(BYTE* base, BYTE* editor) {
    static const uint32_t key[] = {FANG_EDITOR_ENV_DIRECTORY, 0xea5118b0u, 0x40450200u};
    BYTE* manager;
    BYTE* service;
    BYTE* resource = NULL;
    BYTE* directory;
    void** methods;
    if (fang_mod_environment_active(editor)) return 1;
    if (environment_owner) fang_mod_environment_leave(environment_owner);
    /* Retain the exact build-103 interfaces recovered from resource loading
     * at 7E9F30 and directory registration at CF1D60/CF1030. */
    if (!readable_range(base + 0x3aec80, 6) || base[0x3aec80] != 0xa1 ||
        base[0x3aec85] != 0xc3 || !readable_range(base + 0x3aeb70, 6) ||
        base[0x3aeb70] != 0xa1 || base[0x3aeb75] != 0xc3) return 0;
    manager = ((environment_manager_fn)(base + 0x3aeb70))();
    service = ((environment_manager_fn)(base + 0x3aec80))();
    if (!readable_range(manager, 4) || !readable_range(*(void**)manager, 16) ||
        !readable_range(service, 4) || !readable_range(*(void**)service, 104)) return 0;
    methods = *(void***)service;
    if (methods[24] != base + 0x8f1d60 || methods[25] != base + 0x8f1030 ||
        (*(void***)manager)[3] != base + 0x707a50) return 0;
    trace_client_state("mod_editor_environment_resource_begin", 1);
    if (!((environment_read_fn)(*(void***)manager)[3])(
        manager, key, &resource, 0, 0, 0, 0)) {
        if (resource) ((environment_ref_fn)(*(void***)resource)[1])(resource);
        trace_client_state("mod_editor_environment_resource_missing", 1);
        return 0;
    }
    trace_client_state("mod_editor_environment_resource_loaded", 1);
    if (!readable_range(resource, 24)) goto failed;
    directory = *(BYTE**)(resource + 20);
    if (!readable_range(directory, 4) || !readable_range(*(void**)directory, 8)) goto failed;
    trace_client_state("mod_editor_environment_register_begin", 1);
    if (!((environment_directory_fn)methods[24])(service, directory)) goto failed;
    environment_base = base;
    environment_owner = editor;
    environment_resource = resource;
    environment_directory = directory;
    environment_service = service;
    trace_client_state("mod_editor_environment_directory_registered", 1);
    return 1;
failed:
    if (resource) ((environment_ref_fn)(*(void***)resource)[1])(resource);
    trace_client_state("mod_editor_environment_directory_failed", 1);
    return 0;
}

int fang_mod_environment_started(BYTE* editor) {
    BYTE* manager;
    BYTE* effect;
    if (!fang_mod_environment_active(editor)) return 0;
    manager = ((environment_manager_fn)(environment_base + 0x264290))();
    effect = ((environment_find_fn)(environment_base + 0x263b50))(manager, FANG_EDITOR_ENV_ENTRY);
    trace_client_state("mod_editor_environment_active", effect != NULL);
    return effect != NULL;
}

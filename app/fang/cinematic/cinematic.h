#ifndef DARKSPIN_CINEMATIC_H
#define DARKSPIN_CINEMATIC_H
#include <windows.h>
#include <stdint.h>

typedef struct fang_cinematic_setting {
    uint32_t flags, thread_id;
    uint64_t completed_tick_ms;
} fang_cinematic_setting;

int fang_cinematic_install(HMODULE executable, int is_intro_skipped, int is_cinematic_skipped);
int fang_cinematic_configure(uint32_t mask, uint32_t flags, fang_cinematic_setting* setting);
#endif

#ifndef DARKSPIN_FANG_HOOK_H
#define DARKSPIN_FANG_HOOK_H

#include "fang.h"

#if defined(__GNUC__)
#define FANG_THISCALL __attribute__((thiscall))
#else
#define FANG_THISCALL __thiscall
#endif

int readable_range(const void* pointer, size_t size);
int patch_call(void* instruction, void* expected_target, void* replacement);
int patch_pointer(void** target, void* expected, void* replacement);
void trace_client_state(const char* kind, unsigned int value);

#endif

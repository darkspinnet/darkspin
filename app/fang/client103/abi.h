#ifndef DARKSPIN_FANG_ABI_H
#define DARKSPIN_FANG_ABI_H

#include "fang.h"
#include <stdint.h>

_Static_assert(sizeof(void*) == 4, "Fang's build-103 native ABI requires x86");
_Static_assert(sizeof(float) == 4, "Fang's build-103 native ABI requires float32");

#if defined(__GNUC__)
#define FANG_THISCALL __attribute__((thiscall))
#else
#define FANG_THISCALL __thiscall
#endif

#endif

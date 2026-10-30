#ifndef DARKSPIN_FANG_BUG_H
#define DARKSPIN_FANG_BUG_H
#include "fang.h"

int fang_install_exception_trace(HANDLE trace, HMODULE executable);
void fang_trace_arsenal_snapshot(HMODULE executable);

#endif

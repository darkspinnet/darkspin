#ifndef DARKSPIN_FANG_HISTORY_H
#define DARKSPIN_FANG_HISTORY_H

#include "fang.h"

int fang_history_begin(unsigned int frame, unsigned int delta_bits, unsigned int frame_time);
int fang_history_collect(const char* kind, unsigned int value);
void fang_history_end(void);
void fang_history_input(UINT message, WPARAM wparam);
void fang_history_write(const char* line, unsigned int length);

#endif

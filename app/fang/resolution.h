#ifndef DARKSPIN_FANG_RESOLUTION_H
#define DARKSPIN_FANG_RESOLUTION_H

#include "hook.h"

struct fang_resolution {
    int width;
    int height;
    int refresh;
};

int fang_read_resolution(BYTE* base, struct fang_resolution* resolution);
int fang_select_resolution(BYTE* base, const struct fang_resolution* resolution);
int fang_render_resolution(BYTE* base, void* app, const struct fang_resolution* resolution);

#endif

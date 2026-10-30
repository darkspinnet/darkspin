#ifndef DARKSPIN_FANG_DISPLAY_H
#define DARKSPIN_FANG_DISPLAY_H

#include "hook.h"

int fang_borderless_requested(void);
void fang_request_borderless(int is_enabled);
int fang_taskbar_overlay_requested(void);
void fang_request_taskbar_overlay(int is_enabled);
int fang_install_display_ui(BYTE* base);
void fang_update_display_ui(void);

int fang_install_display_preferences(HMODULE executable);
void fang_set_display_window(HWND window);

#endif

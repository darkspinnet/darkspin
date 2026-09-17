#ifndef darkspin_FANG_H
#define darkspin_FANG_H

#include <winsock2.h>
#include <windows.h>

int fang_install(const char* hostname, unsigned short port, unsigned short party_port,
    const char* trace_path, int skip_intro, int skip_cinematic, const char* jwt,
    const char* window_title);

int fang_install_display_preferences(HMODULE executable);
void fang_set_display_window(HWND window);

__declspec(dllexport) DWORD WINAPI RecapInitializeThread(LPVOID parameter);

#endif

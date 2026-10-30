#ifndef DARKSPIN_FANG_CHAT_COMMAND_H
#define DARKSPIN_FANG_CHAT_COMMAND_H

#include <stddef.h>

char* find_darkspin_chat_command(const char* text, int* command);
int normalize_fang_warp_command(const char* text, const char* command_text,
    char* output, size_t output_capacity);
int normalize_fang_spawn_command(const char* text, const char* command_text,
    char* output, size_t output_capacity);

#endif

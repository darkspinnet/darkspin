#ifndef DARKSPIN_FANG_CLIENT103_IMAGE_H
#define DARKSPIN_FANG_CLIENT103_IMAGE_H

#include "fang.h"
#include <stddef.h>
#include <stdint.h>

typedef struct fang_client103_image {
    BYTE* base;
    uint32_t size, timestamp, checksum, version_ms, version_ls;
} fang_client103_image;

int fang_client103_image_open(HMODULE executable, size_t minimum, fang_client103_image* image);
void* fang_client103_at(const fang_client103_image* image, uint32_t rva, size_t length);
void* fang_client103_binding(const fang_client103_image* image, const char* feature,
    const char* symbol, uint32_t rva, size_t length, const char* evidence);
void* fang_client103_required_binding(const fang_client103_image* image,
    const char* symbol, uint32_t rva, int* is_valid);
int fang_client103_expect(const fang_client103_image* image, const char* feature,
    const char* symbol, uint32_t rva, const BYTE* bytes, size_t length, const char* evidence);
void fang_client103_report(void (*write_line)(const char*, unsigned int));

#endif

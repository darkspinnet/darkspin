#ifndef DARKSPIN_FANG_HOOK_H
#define DARKSPIN_FANG_HOOK_H

#include "fang.h"
#include <stdint.h>

#if defined(__GNUC__)
#define FANG_THISCALL __attribute__((thiscall))
#else
#define FANG_THISCALL __thiscall
#endif

int readable_range(const void* pointer, size_t size);
int patch_call(void* instruction, void* expected_target, void* replacement);
int patch_pointer(void** target, void* expected, void* replacement);
void trace_client_state(const char* kind, unsigned int value);
int fang_mod_attach_dbbf(void* database, const wchar_t* path, unsigned int package_index);
void* fang_mod_open_source(const wchar_t* path);
void fang_mod_close_source(void* stream);
int fang_mod_import_source(const wchar_t* path, uint32_t instance, uint32_t* key, void** retained);
void fang_mod_install_character(BYTE* base, const wchar_t* manifest);
uint32_t fang_mod_noun_id(uintptr_t reference);
int fang_mod_observe_import(BYTE* base, const wchar_t* manifest);
void fang_mod_install_baseline(BYTE* base, const wchar_t* manifest);
int fang_mod_bind_baseline(const uint32_t* metadata, const uint32_t* model_key);
enum fang_mod_voice_kind {
    MOD_VOICE_DANCE, MOD_VOICE_TAUNT, MOD_VOICE_ATTACK, MOD_VOICE_HIT,
    MOD_VOICE_DEATH, MOD_VOICE_IDLE, MOD_VOICE_FEAR, MOD_VOICE_LAUGH,
    MOD_VOICE_HAPPY, MOD_VOICE_CAST, MOD_VOICE_GROWL, MOD_VOICE_COUNT
};
void fang_mod_install_audio(BYTE* base);
void fang_mod_install_music(BYTE* base, const wchar_t* manifest);
unsigned int fang_mod_music_package(BYTE* output, unsigned int capacity);
int fang_mod_attach_memory(void* database, const wchar_t* path, const BYTE* payload, uint32_t size);
void fang_mod_install_voice(BYTE* base);
int fang_mod_select_voice(char* event, int capacity);
uint32_t fang_mod_vocalization(BYTE* rig, uint32_t joint, enum fang_mod_voice_kind kind);
void fang_mod_install_dance(BYTE* base);
void fang_mod_install_emote(BYTE* base);
int fang_mod_is_emote(uint32_t state);
int fang_mod_prepare_emote(void* manager, uint32_t state);
void fang_mod_update_dance(void);
int fang_mod_is_bound_key(const uint32_t* key);
int fang_mod_bound_identity(const uint32_t* key, uint32_t* source, uint32_t* noun);
int fang_mod_portrait_source(const uint32_t* key, uint32_t* source);
void fang_mod_install_portrait(BYTE* base, const wchar_t* manifest);
void fang_mod_install_scale(BYTE* base, const wchar_t* manifest);
void fang_mod_install_fit(BYTE* base, const wchar_t* manifest);
void fang_mod_install_editor(BYTE* base, const wchar_t* manifest);
int fang_mod_install_editor_ui(BYTE* base);
void fang_mod_update_editor(void);
int fang_mod_editor_target(BYTE* context);
void __cdecl fang_mod_editor_open(BYTE* context);
BYTE* fang_mod_editor_active(void);
void fang_mod_editor_update_body(void);
void fang_mod_editor_close_body_ui(void);
void fang_mod_install_validation(BYTE* base);
void fang_mod_editor_clear_validation(void);
const char* fang_mod_editor_validation_text(unsigned int page, unsigned int* pages);

#endif

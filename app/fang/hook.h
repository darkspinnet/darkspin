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
int fang_mod_editor_identity(BYTE* context, uint32_t* identity);
void __cdecl fang_mod_editor_open(BYTE* context);
void __cdecl fang_mod_editor_open_scene(BYTE* context);
int fang_mod_editor_is_scene(BYTE* editor);
void fang_mod_editor_continue_scene(uint32_t instance, uint32_t type, uint32_t group);
BYTE* fang_mod_editor_active(void);
void* fang_mod_editor_exit_hook(void);
int fang_mod_editor_is_body(BYTE* editor);
int fang_mod_editor_is_save_busy(BYTE* editor);
int fang_mod_editor_preview_ready(BYTE* editor);
int fang_mod_editor_is_preview_rig(BYTE* rig);
int fang_mod_editor_preview_action(unsigned int action);
void fang_mod_editor_preview_update(void);
void fang_mod_editor_preview_controls(BYTE* base, BYTE* ui, HWND window);
int fang_mod_editor_preview_input(HWND window, UINT message, WPARAM wparam, LPARAM lparam);
void fang_mod_editor_preview_controls_leave(void);
void fang_mod_install_editor_preview(BYTE* base);
void fang_mod_install_editor_skin(BYTE* base);
void fang_mod_editor_update_skin(void);
void fang_mod_editor_update_body(void);
void fang_mod_install_palette(BYTE* base);
int fang_mod_palette_is_loading(void);
int fang_mod_install_palette_cards(BYTE* base);
int fang_mod_palette_warm(BYTE* panel);
void fang_mod_palette_cards_leave(BYTE* panel);
void fang_mod_palette_drag_card(BYTE* base, BYTE* card);
void fang_mod_palette_drag_leave(void);
void fang_mod_update_palette(void);
void fang_mod_palette_suspend(BYTE* editor);
void fang_mod_palette_leave(BYTE* editor);
int fang_mod_palette_cards(BYTE* base, BYTE* panel);
enum { FANG_PART_COLUMNS = 4, FANG_PART_ROWS = 10,
    FANG_PART_PAGE_SIZE = FANG_PART_COLUMNS * FANG_PART_ROWS };
int fang_mod_palette_pages(BYTE* base, BYTE* category);
void fang_mod_palette_tip_card(BYTE* base, BYTE* card);
int fang_mod_palette_tip_categories(BYTE* base, BYTE* panel);
int fang_mod_palette_contains(BYTE* panel, BYTE* entry);
void fang_mod_editor_close_body_ui(void);
void fang_mod_install_validation(BYTE* base);
void fang_mod_editor_clear_validation(void);
const char* fang_mod_editor_validation_text(unsigned int page, unsigned int* pages);
void fang_mod_editor_update_validation(BYTE* ui);
int fang_mod_editor_validation_scroll(HWND window, WPARAM wparam, LPARAM lparam);
enum { FANG_EDITOR_SCENE_MODEL = 0xfad0ce01u, FANG_EDITOR_SCENE_LIGHT = 0xfad0ce02u };
enum { FANG_EDITOR_ENV_DIRECTORY = 0xfad0ce03u, FANG_EDITOR_ENV_ENTRY = 0xfad0ce04u };
int fang_mod_scene_expand(const BYTE* source, unsigned int size, BYTE* output, unsigned int decoded);
typedef struct fang_environment_source {
    BYTE* payload;
    unsigned int stored;
    unsigned int decoded;
    unsigned int compression;
} fang_environment_source;
BYTE* fang_mod_environment_package(const fang_environment_source* sources, unsigned int* size);
BYTE* fang_mod_scene_ground(const BYTE* source, unsigned int stored,
    unsigned int decoded, unsigned int compression, unsigned int shader);
int fang_mod_attach_environment(void* database, const wchar_t* path);
int fang_mod_environment_active(BYTE* editor);
int fang_mod_environment_prepare(BYTE* base, BYTE* editor);
int fang_mod_environment_started(BYTE* editor);
void fang_mod_environment_leave(BYTE* editor);
int fang_mod_editor_button_skin(BYTE* base, BYTE* window, BYTE* donor);
const wchar_t* fang_mod_palette_category_name(BYTE* source);
int fang_mod_editor_tab_update(BYTE* base, BYTE* ui, int is_selected, int is_enabled);
void fang_mod_editor_tab_leave(void);
void fang_mod_palette_view(BYTE* base, BYTE* panel);
int fang_mod_palette_tabs(BYTE* base, BYTE* panel, BYTE* equipment);
void fang_mod_palette_tabs_update(BYTE* panel);
void fang_mod_palette_tabs_leave(void);
void fang_mod_palette_title(BYTE* base, BYTE* panel);
void fang_mod_palette_title_leave(void);
int fang_mod_palette_is_styled(void);
int fang_mod_attach_scene(void* database, const wchar_t* path);
void fang_mod_install_scene(BYTE* base);
int fang_mod_scene_available(void);
void fang_mod_update_scene(void);
void fang_mod_scene_leave(BYTE* editor);
BYTE* fang_mod_scene_floor(const BYTE* source, unsigned int stored, unsigned int decoded, unsigned int compression);

#endif

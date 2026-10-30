#ifndef DARKSPIN_FANG_CLIENT103_BOOTSTRAP_H
#define DARKSPIN_FANG_CLIENT103_BOOTSTRAP_H

#include "client103/abi.h"
#include "client103/image.h"

typedef int (WINAPI* messageboxw_fn)(HWND window, LPCWSTR text, LPCWSTR caption, UINT type);
typedef int (WINAPI* messageboxa_fn)(HWND window, LPCSTR text, LPCSTR caption, UINT type);
typedef DWORD (WINAPI* getfileattributesw_fn)(LPCWSTR file_name);
typedef void* (__cdecl* chat_lookup_fn)(void);
typedef void* (__cdecl* chat_create_fn)(void);
typedef void* (__cdecl* chat_text_convert_fn)(void* output, const char* text, int length);
typedef void (__cdecl* alert_fn)(const char* message, int code, int severity);
typedef void (__cdecl* login_error_fn)(unsigned int code, void* target);
typedef unsigned char (__cdecl* cinematic_ready_fn)(void);
typedef void (__cdecl* movie_load_fn)(void* output, const char* name, int unknown_a, int unknown_b);
typedef void* (__cdecl* movie_manager_fn)(void);
typedef unsigned char (__cdecl* spline_update_fn)(void* camera, float delta);
typedef void* (__cdecl* spline_skip_fn)(void);
typedef void* (__cdecl* camera_registry_fn)(void);
typedef void (__cdecl* navigation_update_fn)(void* controller);
typedef void (__cdecl* go_to_room_fn)(void* event);
typedef unsigned int (__cdecl* new_player_progress_fn)(void);
typedef void (__cdecl* map_room_start_game_fn)(void);
typedef void* (__cdecl* online_platform_fn)(void);
typedef void* (__cdecl* scene_asset_lookup_fn)(unsigned int asset_id);
typedef void* (__cdecl* creature_asset_lookup_fn)(void* resource, void* output);
typedef void* (__cdecl* creature_nested_lookup_fn)(void* asset, void* output);
typedef void (__cdecl* scene_load_fn)(void* scene, unsigned int markerset, unsigned int index);
typedef void (__cdecl* scene_change_fn)(void* simulator, void* scene);
typedef void* (__cdecl* reflection_lookup_fn)(unsigned int type_id);
typedef void* (__cdecl* ability_hud_fn)(void);
typedef void* (__cdecl* game_clock_owner_fn)(void);
typedef uint64_t (__cdecl* game_clock_now_fn)(void* owner);
typedef void* (__cdecl* controlled_player_fn)(void);
typedef void* (__cdecl* clock_root_fn)(void);
typedef void* (__cdecl* clock_state_fn)(void* root);
typedef void (__cdecl* clock_initialize_fn)(uint64_t source_time);
typedef unsigned char (__cdecl* ability_available_fn)(void* player, int ability_index);
typedef unsigned char (__cdecl* property_vector_read_fn)(void* resource, unsigned int property_id,
    unsigned int* count, const unsigned int** value);
typedef void* (__cdecl* resource_manager_fn)(void);
typedef void (__cdecl* local_event_fn)(unsigned int event_id, void* event);
typedef unsigned char (__cdecl* reflection_decode_callback_fn)(
    int context, unsigned int* object, int* field, char* data);
typedef unsigned char (__cdecl* server_event_decode_fn)(void* stream, void* event,
    int flags, reflection_decode_callback_fn callback);
typedef unsigned char (__cdecl* server_event_validate_fn)(void* event);
typedef void* (__cdecl* effect_system_fn)(void);
typedef unsigned char (__cdecl* loot_convert_fn)(void* output, const void* loot,
    int is_local, int unknown);
typedef double (__cdecl* mana_cost_fn)(void* ability, void* actor, int rank, float property);
typedef void* (__cdecl* ability_descriptor_lookup_fn)(unsigned int ability_guid);
typedef void* (__cdecl* combat_input_state_fn)(void);
typedef void* (__cdecl* settings_manager_fn)(void);
typedef void* (__cdecl* game_object_resolve_fn)(unsigned int object_id);
typedef void* (__stdcall* locomotion_receiver_fn)(void* message);
typedef float (__cdecl* frame_delta_fn)(void* frame);
#if defined(__GNUC__)
typedef void (__attribute__((thiscall))* sporenet_callback_fn)(void* request, void* response);
typedef void (__attribute__((thiscall))* web_script_fn)(void* host, const wchar_t* script);
typedef void (__attribute__((thiscall))* resource_lookup_fn)(void* manager,
    unsigned int type_id, unsigned int instance_id, void** resource);
typedef void (__attribute__((thiscall))* resource_release_fn)(void* resource);
typedef void* (__attribute__((thiscall))* cooldown_lookup_fn)(void* cooldown_map, const uint64_t* ability_key);
typedef void (__attribute__((thiscall))* cooldown_apply_fn)(void* object, uint64_t ability_key,
    int64_t duration, int64_t source_start, int64_t global_cooldown);
typedef void* (__attribute__((thiscall))* message_construct_fn)(void* message, unsigned char id,
    const void* data, unsigned int size);
typedef unsigned int (__attribute__((thiscall))* message_read_fn)(void* stream, void* destination,
    unsigned int size, void* context);
typedef unsigned char (__attribute__((thiscall))* screen_event_fn)(void* target, unsigned int event_id);
typedef unsigned char (__attribute__((thiscall))* movie_playing_fn)(void* manager);
typedef void (__attribute__((thiscall))* movie_stop_fn)(void* manager);
typedef void* (__attribute__((thiscall))* object_method_fn)(void* target);
typedef void* (__attribute__((thiscall))* lookup_method_fn)(void* target, unsigned int id);
typedef int (__attribute__((thiscall))* setting_read_fn)(void* target, unsigned int key);
typedef void (__attribute__((thiscall))* camera_set_fn)(void* camera, void* position, void* rotation,
    float angle_x, float angle_y, float angle_z);
typedef void (__attribute__((thiscall))* login_screen_init_fn)(void* controller, void* manager);
typedef unsigned char (__attribute__((thiscall))* login_ready_fn)(void* manager);
typedef void (__attribute__((thiscall))* login_submit_fn)(void* manager, const char* username, const char* password);
typedef void (__attribute__((thiscall))* launcher_ready_fn)(void* launcher);
typedef void (__attribute__((thiscall))* chat_initialize_fn)(void* chat);
typedef void (__attribute__((thiscall))* chat_show_fn)(void* chat, unsigned char is_visible);
typedef void (__attribute__((thiscall))* chat_open_fn)(void* chat, unsigned int action);
#else
typedef void (__thiscall* sporenet_callback_fn)(void* request, void* response);
typedef void (__thiscall* web_script_fn)(void* host, const wchar_t* script);
typedef void (__thiscall* resource_lookup_fn)(void* manager,
    unsigned int type_id, unsigned int instance_id, void** resource);
typedef void (__thiscall* resource_release_fn)(void* resource);
typedef void* (__thiscall* cooldown_lookup_fn)(void* cooldown_map, const uint64_t* ability_key);
typedef void (__thiscall* cooldown_apply_fn)(void* object, uint64_t ability_key,
    int64_t duration, int64_t source_start, int64_t global_cooldown);
typedef void* (__thiscall* message_construct_fn)(void* message, unsigned char id,
    const void* data, unsigned int size);
typedef unsigned int (__thiscall* message_read_fn)(void* stream, void* destination,
    unsigned int size, void* context);
typedef unsigned char (__thiscall* screen_event_fn)(void* target, unsigned int event_id);
typedef unsigned char (__thiscall* movie_playing_fn)(void* manager);
typedef void (__thiscall* movie_stop_fn)(void* manager);
typedef void* (__thiscall* object_method_fn)(void* target);
typedef void* (__thiscall* lookup_method_fn)(void* target, unsigned int id);
typedef int (__thiscall* setting_read_fn)(void* target, unsigned int key);
typedef void (__thiscall* camera_set_fn)(void* camera, void* position, void* rotation,
    float angle_x, float angle_y, float angle_z);
typedef void (__thiscall* login_screen_init_fn)(void* controller, void* manager);
typedef unsigned char (__thiscall* login_ready_fn)(void* manager);
typedef void (__thiscall* login_submit_fn)(void* manager, const char* username, const char* password);
typedef void (__thiscall* launcher_ready_fn)(void* launcher);
typedef void (__thiscall* chat_initialize_fn)(void* chat);
typedef void (__thiscall* chat_show_fn)(void* chat, unsigned char is_visible);
typedef void (__thiscall* chat_open_fn)(void* chat, unsigned int action);
#endif
typedef unsigned char (FANG_THISCALL* party_ready_fn)(void* party);
typedef void (FANG_THISCALL* combat_input_reset_fn)(void* state);
typedef void (FANG_THISCALL* rooms_bootstrap_fn)(void* platform);
typedef void* (FANG_THISCALL* rooms_map_lookup_fn)(void* map, unsigned int id);
typedef unsigned char (FANG_THISCALL* presence_query_fn)(void* platform,
    unsigned int id_low, unsigned int id_high, unsigned int* presence,
    unsigned short* playgroup, unsigned int* level_id, unsigned short* client_data);
typedef int (FANG_THISCALL* scaleform_stream_read_fn)(void* stream,
    void* destination, int length);

/* Build-103 legacy bootstrap bindings, transcribed from existing Fang call
 * interfaces and patch sites. These names do not assert new prologue evidence;
 * checked hook installation still verifies each existing target. */

enum {
    FANG103_ORIGINAL_ALERT_RVA = 0x3BAC40,
    FANG103_ORIGINAL_LOGIN_ERROR_RVA = 0x83FA80,
    FANG103_ORIGINAL_SCENE_READY_RVA = 0xF3FF0,
    FANG103_ORIGINAL_UI_READY_RVA = 0xE6310,
    FANG103_ORIGINAL_SCREEN_EVENT_RVA = 0x50EAF0,
    FANG103_ORIGINAL_MOVIE_LOAD_RVA = 0x3ADD40,
    FANG103_ORIGINAL_MOVIE_MANAGER_RVA = 0x3B6FD0,
    FANG103_ORIGINAL_SPLINE_UPDATE_RVA = 0x61AFE0,
    FANG103_ORIGINAL_SPLINE_SKIP_RVA = 0x61B840,
    FANG103_ORIGINAL_CAMERA_REGISTRY_RVA = 0x3AEBB0,
    FANG103_ORIGINAL_CAMERA_SET_RVA = 0x12F0E0,
    FANG103_ORIGINAL_NAVIGATION_UPDATE_RVA = 0x12D600,
    FANG103_ORIGINAL_PARTY_READY_RVA = 0x119960,
    FANG103_ORIGINAL_GO_TO_ROOM_RVA = 0x12CFD0,
    FANG103_ORIGINAL_NEW_PLAYER_PROGRESS_RVA = 0x1187D0,
    FANG103_ORIGINAL_MAP_ROOM_START_GAME_RVA = 0x116100,
    FANG103_ORIGINAL_ONLINE_PLATFORM_RVA = 0x826F80,
    FANG103_ORIGINAL_ROOMS_BOOTSTRAP_RVA = 0x828550,
    FANG103_ORIGINAL_ROOMS_MAP_LOOKUP_RVA = 0xA1BC10,
    FANG103_ORIGINAL_SCENE_ASSET_LOOKUP_RVA = 0x5C5FF0,
    FANG103_ORIGINAL_CREATURE_ASSET_LOOKUP_RVA = 0x5ECA10,
    FANG103_ORIGINAL_CREATURE_NESTED_LOOKUP_RVA = 0x5C8E70,
    FANG103_ORIGINAL_SCENE_LOAD_RVA = 0xE7480,
    FANG103_ORIGINAL_SCENE_CHANGE_RVA = 0xF3160,
    FANG103_ORIGINAL_ABILITY_HUD_RVA = 0x25980,
    FANG103_ORIGINAL_COOLDOWN_LOOKUP_RVA = 0x21F50,
    FANG103_ORIGINAL_COOLDOWN_APPLY_RVA = 0xD6320,
    FANG103_ORIGINAL_GAME_CLOCK_OWNER_RVA = 0xE4DE0,
    FANG103_ORIGINAL_GAME_CLOCK_NOW_RVA = 0x5D2920,
    FANG103_ORIGINAL_CONTROLLED_PLAYER_RVA = 0xE57B0,
    FANG103_ORIGINAL_CLOCK_ROOT_RVA = 0x5BCBE0,
    FANG103_ORIGINAL_CLOCK_STATE_RVA = 0x5BD190,
    FANG103_ORIGINAL_CLOCK_INITIALIZE_RVA = 0x5D2830,
    FANG103_ORIGINAL_ABILITY_AVAILABLE_RVA = 0x5C2A60,
    FANG103_ORIGINAL_PROPERTY_VECTOR_READ_RVA = 0x3B3E50,
    FANG103_ORIGINAL_RESOURCE_MANAGER_RVA = 0x3AECE0,
    FANG103_ORIGINAL_REFLECTION_LOOKUP_RVA = 0x5EF610,
    FANG103_ORIGINAL_SERVER_EVENT_DECODE_RVA = 0x61E1F0,
    FANG103_ORIGINAL_SERVER_EVENT_VALIDATE_RVA = 0xDFFB0,
    FANG103_ORIGINAL_EFFECT_SYSTEM_RVA = 0x3AEC80,
    FANG103_ORIGINAL_GAME_OBJECT_REGISTRY_RVA = 0x5BC740,
    FANG103_ORIGINAL_GAME_OBJECT_LOOKUP_RVA = 0x5D1730,
    FANG103_ORIGINAL_MESSAGE_CONSTRUCT_RVA = 0x687850,
    FANG103_ORIGINAL_MESSAGE_READ_RVA = 0x687BF0,
    FANG103_ORIGINAL_LOGIN_SCREEN_INIT_RVA = 0x114530,
    FANG103_ORIGINAL_LAUNCHER_READY_RVA = 0x10E7D0,
    FANG103_ORIGINAL_LOCAL_EVENT_RVA = 0xE17E0,
    FANG103_ORIGINAL_LOOT_CONVERT_RVA = 0x5CD7C0,
    FANG103_ORIGINAL_LOOT_FORMAT_RVA = 0x3A170,
    FANG103_ORIGINAL_MANA_COST_RVA = 0x5DD720,
    FANG103_ORIGINAL_ABILITY_DESCRIPTOR_LOOKUP_RVA = 0x5DAC00,
    FANG103_ORIGINAL_COMBAT_INPUT_STATE_RVA = 0xE4E00,
    FANG103_ORIGINAL_COMBAT_INPUT_RESET_RVA = 0xD5EF0,
    FANG103_ORIGINAL_SETTINGS_MANAGER_RVA = 0x3AEBD0,
    FANG103_ORIGINAL_GAME_OBJECT_RESOLVE_RVA = 0x5D94F0,
    FANG103_ORIGINAL_PLAYER_MOVE_RECEIVER_RVA = 0x1393D0,
    FANG103_ORIGINAL_LOCOMOTION_UPDATE_RECEIVER_RVA = 0x139900,
    FANG103_ORIGINAL_LOCOMOTION_UNRELIABLE_RECEIVER_RVA = 0x139810,
    FANG103_ORIGINAL_FRAME_DELTA_RVA = 0x5D2960,
    FANG103_ORIGINAL_SPORENET_CALLBACK_RVA = 0xA57D0,
    FANG103_ORIGINAL_SCALEFORM_STREAM_READ_RVA = 0xA54240,
    FANG103_ORIGINAL_CHAT_LOOKUP_RVA = 0x25680,
    FANG103_ORIGINAL_CHAT_CREATE_RVA = 0xB5D0,
    FANG103_ORIGINAL_CHAT_INITIALIZE_RVA = 0xABA0,
    FANG103_ORIGINAL_CHAT_SHOW_RVA = 0xD850,
    FANG103_ORIGINAL_CHAT_OPEN_RVA = 0xB8F0,
    FANG103_ORIGINAL_CHAT_TEXT_CONVERT_RVA = 0x6D9E40,
    FANG103_ORIGINAL_LOGIN_SCREEN_INIT_POINTER_1_RVA = 0xBDF9A4,
    FANG103_ORIGINAL_LAUNCHER_READY_CALL_1_RVA = 0x110BE6,
    FANG103_ORIGINAL_CHAT_TEXT_CONVERT_CALL_1_RVA = 0xC998,
    FANG103_ORIGINAL_PARTY_READY_CALL_1_RVA = 0x115A3F,
    FANG103_ORIGINAL_SCENE_READY_CALL_1_RVA = 0x5557A,
    FANG103_ORIGINAL_UI_READY_CALL_1_RVA = 0x55583,
    FANG103_ORIGINAL_SCREEN_EVENT_CALL_1_RVA = 0x55401,
    FANG103_ORIGINAL_SCREEN_EVENT_CALL_2_RVA = 0x55567,
    FANG103_ORIGINAL_SCREEN_EVENT_CALL_3_RVA = 0x555CC,
    FANG103_ORIGINAL_MESSAGE_CONSTRUCT_CALL_1_RVA = 0x68A4D3,
    FANG103_ORIGINAL_GAME_OBJECT_RESOLVE_CALL_1_RVA = 0x139447,
    FANG103_ORIGINAL_GAME_OBJECT_RESOLVE_CALL_2_RVA = 0x139886,
    FANG103_ORIGINAL_GAME_OBJECT_RESOLVE_CALL_3_RVA = 0x13997B,
    FANG103_ORIGINAL_PLAYER_MOVE_RECEIVER_CALL_1_RVA = 0x13AE74,
    FANG103_ORIGINAL_LOCOMOTION_UPDATE_RECEIVER_CALL_1_RVA = 0x13AE9B,
    FANG103_ORIGINAL_LOCOMOTION_UNRELIABLE_RECEIVER_CALL_1_RVA = 0x13AEA8,
    FANG103_ORIGINAL_FRAME_DELTA_CALL_1_RVA = 0x5BF1F6,
    FANG103_ORIGINAL_SCENE_ASSET_LOOKUP_CALL_1_RVA = 0x137E4B,
    FANG103_ORIGINAL_MESSAGE_READ_CALL_1_RVA = 0x137E37,
    FANG103_ORIGINAL_CREATURE_ASSET_LOOKUP_CALL_1_RVA = 0x1F9FD,
    FANG103_ORIGINAL_CREATURE_NESTED_LOOKUP_CALL_1_RVA = 0x1FA0E,
    FANG103_ORIGINAL_SCENE_LOAD_CALL_1_RVA = 0x12B9F9,
    FANG103_ORIGINAL_SCENE_LOAD_CALL_2_RVA = 0x137F99,
    FANG103_ORIGINAL_SCENE_CHANGE_CALL_1_RVA = 0xE7495,
    FANG103_ORIGINAL_COOLDOWN_APPLY_CALL_1_RVA = 0x13A5D7,
    FANG103_ORIGINAL_CLOCK_INITIALIZE_CALL_1_RVA = 0x137D30,
    FANG103_ORIGINAL_PROPERTY_VECTOR_READ_CALL_1_RVA = 0x5CF595,
    FANG103_ORIGINAL_ABILITY_AVAILABLE_CALL_1_RVA = 0xEC129,
    FANG103_ORIGINAL_ABILITY_AVAILABLE_CALL_2_RVA = 0xEC1A9,
    FANG103_ORIGINAL_ABILITY_DESCRIPTOR_LOOKUP_CALL_1_RVA = 0x21631,
    FANG103_ORIGINAL_SERVER_EVENT_DECODE_CALL_1_RVA = 0x139EDB,
    FANG103_ORIGINAL_SERVER_EVENT_VALIDATE_CALL_1_RVA = 0x106DB7,
    FANG103_ORIGINAL_LOCAL_EVENT_CALL_1_RVA = 0x139F12,
    FANG103_ORIGINAL_LOOT_CONVERT_CALL_1_RVA = 0xE1EBC,
    FANG103_ORIGINAL_LOOT_FORMAT_CALL_1_RVA = 0xE1EDD,
    FANG103_ORIGINAL_CREATURE_NESTED_LOOKUP_CALL_2_RVA = 0x5CD800,
    FANG103_ORIGINAL_CREATURE_NESTED_LOOKUP_CALL_3_RVA = 0x5CD82F,
    FANG103_ORIGINAL_CREATURE_NESTED_LOOKUP_CALL_4_RVA = 0x5CD89C,
    FANG103_ORIGINAL_CREATURE_NESTED_LOOKUP_CALL_5_RVA = 0x5CD8D2,
    FANG103_ORIGINAL_CREATURE_NESTED_LOOKUP_CALL_6_RVA = 0x5CD90A,
    FANG103_ORIGINAL_MANA_COST_CALL_1_RVA = 0x39284,
    FANG103_ORIGINAL_MANA_COST_CALL_2_RVA = 0xDEAD5,
    FANG103_ORIGINAL_MANA_COST_CALL_3_RVA = 0xE561D,
    FANG103_ORIGINAL_MANA_COST_CALL_4_RVA = 0x5E0863,
    FANG103_ORIGINAL_MANA_COST_CALL_5_RVA = 0x5E0D6D,
    FANG103_ORIGINAL_MANA_COST_CALL_6_RVA = 0x66824B,
    FANG103_ORIGINAL_ALERT_CALL_1_RVA = 0x10F47F,
    FANG103_ORIGINAL_ALERT_CALL_2_RVA = 0x10F82B,
    FANG103_ORIGINAL_ALERT_CALL_3_RVA = 0x13589B,
    FANG103_ORIGINAL_ALERT_CALL_4_RVA = 0x135A0F,
    FANG103_ORIGINAL_ALERT_CALL_5_RVA = 0x3E7835,
    FANG103_ORIGINAL_ALERT_CALL_6_RVA = 0x3E7990,
    FANG103_ORIGINAL_ALERT_CALL_7_RVA = 0x3E8CAF,
    FANG103_ORIGINAL_ALERT_CALL_8_RVA = 0x45483F,
    FANG103_ORIGINAL_ALERT_CALL_9_RVA = 0x454AFC,
    FANG103_ORIGINAL_ALERT_CALL_10_RVA = 0x459688,
    FANG103_ORIGINAL_ALERT_CALL_11_RVA = 0x4C9976,
    FANG103_ORIGINAL_LOGIN_ERROR_CALL_1_RVA = 0x83FF3B,
    FANG103_ORIGINAL_LOGIN_ERROR_CALL_2_RVA = 0x83FF9B,
    FANG103_ORIGINAL_LOGIN_ERROR_CALL_3_RVA = 0x84033B,
    FANG103_ORIGINAL_LOGIN_ERROR_CALL_4_RVA = 0x84039B,
    FANG103_ORIGINAL_LOGIN_ERROR_CALL_5_RVA = 0x840481,
};

#define FANG103_BIND(image, status, type, symbol, rva) \
    ((type)fang_client103_required_binding(image, #symbol, rva, status))

#endif

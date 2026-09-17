#include "fang.h"

#include <limits.h>
#include <ctype.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "_cgo_export.h"

#ifndef FANG_DIAGNOSTICS
#define FANG_DIAGNOSTICS 1
#endif

typedef struct hostent* (WSAAPI* gethostbyname_fn)(const char* name);
typedef int (WSAAPI* connect_fn)(SOCKET socket, const struct sockaddr* name, int name_length);
typedef int (WSAAPI* send_fn)(SOCKET socket, const char* buffer, int length, int flags);
typedef int (WSAAPI* recv_fn)(SOCKET socket, char* buffer, int length, int flags);
typedef int (WSAAPI* sendto_fn)(SOCKET socket, const char* buffer, int length, int flags, const struct sockaddr* destination, int destination_length);
typedef int (WSAAPI* recvfrom_fn)(SOCKET socket, char* buffer, int length, int flags, struct sockaddr* source, int* source_length);
typedef int (WSAAPI* closesocket_fn)(SOCKET socket);
typedef int (WSAAPI* wsasend_fn)(SOCKET socket, LPWSABUF buffers, DWORD count, LPDWORD sent, DWORD flags, LPWSAOVERLAPPED overlapped, LPWSAOVERLAPPED_COMPLETION_ROUTINE completion);
typedef int (WSAAPI* wsarecv_fn)(SOCKET socket, LPWSABUF buffers, DWORD count, LPDWORD received, LPDWORD flags, LPWSAOVERLAPPED overlapped, LPWSAOVERLAPPED_COMPLETION_ROUTINE completion);
typedef int (WSAAPI* wsarecvfrom_fn)(SOCKET socket, LPWSABUF buffers, DWORD count, LPDWORD received, LPDWORD flags, struct sockaddr* source, LPINT source_length, LPWSAOVERLAPPED overlapped, LPWSAOVERLAPPED_COMPLETION_ROUTINE completion);
typedef BOOL (WSAAPI* wsagetoverlappedresult_fn)(SOCKET socket, LPWSAOVERLAPPED overlapped, LPDWORD transferred, BOOL wait, LPDWORD flags);
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
typedef void* (__cdecl* creature_asset_lookup_fn)(unsigned int asset_id, void* output);
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
#define FANG_THISCALL __attribute__((thiscall))
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
#define FANG_THISCALL __thiscall
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

typedef struct redirect_tls {
    struct hostent host;
    struct in_addr address;
    char* addresses[2];
} redirect_tls;

static gethostbyname_fn original_gethostbyname;
static connect_fn original_connect;
static send_fn original_send;
static recv_fn original_recv;
static sendto_fn original_sendto;
static recvfrom_fn original_recvfrom;
static closesocket_fn original_closesocket;
static wsasend_fn original_wsasend;
static wsarecv_fn original_wsarecv;
static wsarecvfrom_fn original_wsarecvfrom;
static wsagetoverlappedresult_fn original_wsagetoverlappedresult;
static messageboxw_fn original_messageboxw;
static messageboxa_fn original_messageboxa;
static getfileattributesw_fn original_getfileattributesw;
static chat_lookup_fn original_chat_lookup;
static chat_create_fn original_chat_create;
static chat_initialize_fn original_chat_initialize;
static chat_show_fn original_chat_show;
static chat_open_fn original_chat_open;
static chat_text_convert_fn original_chat_text_convert;
static alert_fn original_alert;
static login_error_fn original_login_error;
static cinematic_ready_fn original_scene_ready;
static cinematic_ready_fn original_ui_ready;
static screen_event_fn original_screen_event;
static movie_load_fn original_movie_load;
static movie_manager_fn original_movie_manager;
static spline_update_fn original_spline_update;
static spline_skip_fn original_spline_skip;
static camera_registry_fn original_camera_registry;
static camera_set_fn original_camera_set;
static navigation_update_fn original_navigation_update;
static party_ready_fn original_party_ready;
static go_to_room_fn original_go_to_room;
static new_player_progress_fn original_new_player_progress;
static map_room_start_game_fn original_map_room_start_game;
static online_platform_fn original_online_platform;
static rooms_bootstrap_fn original_rooms_bootstrap;
static rooms_map_lookup_fn original_rooms_map_lookup;
static scene_asset_lookup_fn original_scene_asset_lookup;
static creature_asset_lookup_fn original_creature_asset_lookup;
static creature_nested_lookup_fn original_creature_nested_lookup;
static scene_load_fn original_scene_load;
static scene_change_fn original_scene_change;
static cooldown_lookup_fn original_cooldown_lookup;
static cooldown_apply_fn original_cooldown_apply;
static ability_hud_fn original_ability_hud;
static game_clock_owner_fn original_game_clock_owner;
static game_clock_now_fn original_game_clock_now;
static controlled_player_fn original_controlled_player;
static clock_root_fn original_clock_root;
static clock_state_fn original_clock_state;
static clock_initialize_fn original_clock_initialize;
static ability_available_fn original_ability_available;
static property_vector_read_fn original_property_vector_read;
static resource_manager_fn original_resource_manager;
static message_construct_fn original_message_construct;
static message_read_fn original_message_read;
static login_screen_init_fn original_login_screen_init;
static launcher_ready_fn original_launcher_ready;
static local_event_fn original_local_event;
static reflection_lookup_fn original_reflection_lookup;
static server_event_decode_fn original_server_event_decode;
static server_event_validate_fn original_server_event_validate;
static effect_system_fn original_effect_system;
static resource_manager_fn original_game_object_registry;
static lookup_method_fn original_game_object_lookup;
static loot_convert_fn original_loot_convert;
static loot_convert_fn original_loot_format;
static mana_cost_fn original_mana_cost;
static ability_descriptor_lookup_fn original_ability_descriptor_lookup;
static combat_input_state_fn original_combat_input_state;
static combat_input_reset_fn original_combat_input_reset;
static settings_manager_fn original_settings_manager;
static game_object_resolve_fn original_game_object_resolve;
static locomotion_receiver_fn original_player_move_receiver;
static locomotion_receiver_fn original_locomotion_update_receiver;
static locomotion_receiver_fn original_locomotion_unreliable_receiver;
static frame_delta_fn original_frame_delta;
static sporenet_callback_fn original_sporenet_callback;
static scaleform_stream_read_fn original_scaleform_stream_read;
static DWORD redirect_tls_index = TLS_OUT_OF_INDEXES;
static DWORD effect_preview_tls_index = TLS_OUT_OF_INDEXES;
static char redirect_hostname[256] = "localhost";
static unsigned short redirect_port = 42127;
static unsigned short redirect_party_port = 42128;
static const unsigned char party_envelope[] = {'D', 'S', 'P', 'G', 1, 1, 0, 0};
#define PARTY_SOCKET_LIMIT 64
static SRWLOCK party_socket_lock = SRWLOCK_INIT;
static SOCKET party_sockets[PARTY_SOCKET_LIMIT];
static unsigned int party_socket_count;
typedef struct party_receive {
    SOCKET socket;
    LPWSAOVERLAPPED overlapped;
    struct sockaddr* source;
    LPINT source_length;
} party_receive;
static party_receive party_receives[PARTY_SOCKET_LIMIT];
static unsigned int party_receive_count;
static unsigned int dns_diagnostics;
static HANDLE trace_file = INVALID_HANDLE_VALUE;
static CRITICAL_SECTION trace_lock;
static int trace_lock_ready;
static HMODULE executable_module;
static volatile LONG movie_skip_pending;
static volatile LONG ship_spline_skipped;
static volatile LONG ship_spline_skip_active;
static volatile LONG ship_navigation_ready;
static volatile LONG ship_start_selected;
static volatile LONG ship_start_deferred;
static volatile LONG tutorial_start_selected;
static volatile LONG ship_return_start_pending;
static volatile LONG ship_start_transition_skipped;
static volatile LONG rooms_bootstrap_started;
static volatile LONG rooms_target_repaired;
static void* traced_rooms_roster;
static unsigned int traced_rooms_member_count = UINT_MAX;
static unsigned int traced_rooms_resolved_count = UINT_MAX;
static DWORD ship_navigation_ready_time;
static int skip_cinematics_enabled;
static char jwt_login_token[(16 * 1024) + 1];
static volatile LONG jwt_login_started;
static volatile LONG jwt_login_completed;
static volatile LONG jwt_login_watchdog_started;
static void* jwt_login_manager;
static UINT_PTR jwt_login_timer;
static unsigned int jwt_login_retry_count;
static char client_result_path[2048];
static char client_launch_id[64];
static volatile LONG multiplayer_auto_started;
static volatile LONG solo_unranked_1v1_patched;
static volatile LONG solo_unranked_1v1_matchmaking_pending;
static volatile LONG traced_scene_asset_id;
static void* volatile traced_scene_asset;
static volatile LONG player_reflection_traced;
static volatile LONG ability_blink_trace_pending;
static volatile LONG action_response_trace_pending;
static volatile LONG diagnostic_controlled_object_id;
static volatile LONG snapshot_capture_enabled;
static volatile LONG snapshot_auto_probe_enabled;
static volatile LONG snapshot_frame_sequence;
static volatile LONG snapshot_frame_delta_bits;
static volatile LONG snapshot_frame_time_ms;
static volatile LONG snapshot_keyframe_pending;
static HANDLE snapshot_keyframe_event;
static DWORD snapshot_last_probe_time;
static DWORD movement_trace_tls_index = TLS_OUT_OF_INDEXES;
static char snapshot_control_path[MAX_PATH * 4];

typedef struct snapshot_line {
    struct snapshot_line* next;
    ULONGLONG time_ms;
    unsigned int size;
    char contents[1];
} snapshot_line;

static SRWLOCK snapshot_ring_lock = SRWLOCK_INIT;
static snapshot_line* snapshot_ring_head;
static snapshot_line* snapshot_ring_tail;
static size_t snapshot_ring_byte_count;
static ULONGLONG snapshot_ring_capacity_dropped_line_count;
static ULONGLONG snapshot_ring_capacity_dropped_byte_count;
static ULONGLONG snapshot_ring_capacity_last_dropped_time_ms;

typedef struct movement_trace_context {
    unsigned int object_id;
    const BYTE* object;
    const BYTE* locomotion;
} movement_trace_context;

#define COMBAT_TEXT_DAMAGE_RVA 0x1038B5Cu
#define COMBAT_TEXT_ENEMY_DAMAGE_RVA 0x1038B6Cu
#define COMBAT_TEXT_DAMAGE_CRITICAL_RVA 0x1038B64u
#define COMBAT_TEXT_ENEMY_DAMAGE_CRITICAL_RVA 0x1038B74u
#define COMBAT_TEXT_ABSORB_RVA 0x1038B7Cu
#define COMBAT_TEXT_ENEMY_ABSORB_RVA 0x1038B8Cu
#define COMBAT_TEXT_ABSORB_CRITICAL_RVA 0x1038B84u
#define COMBAT_TEXT_ENEMY_ABSORB_CRITICAL_RVA 0x1038B94u
#define COMBAT_TEXT_HEAL_RVA 0x1038B9Cu
#define COMBAT_TEXT_HEAL_CRITICAL_RVA 0x1038BA4u
#define COMBAT_TEXT_HEAL_TARGET_RVA 0x1038BACu
#define SHOW_DAMAGE_DONE_BY_ME_RVA 0x1038C0Cu
#define SHOW_DAMAGE_DONE_BY_ALLIES_RVA 0x1038C10u
#define SHOW_DAMAGE_DONE_TO_ME_RVA 0x1038C14u
#define SHOW_DAMAGE_DONE_TO_ALLIES_RVA 0x1038C18u

#define PENDING_MOVEMENT_ACTIVE_RVA 0x1038A0Cu
#define PENDING_ABILITY_ACTIVE_RVA 0x1038A4Cu
#define PENDING_AUXILIARY_ACTIVE_RVA 0x1038AC0u
#define INPUT_INTENT_RVA 0xDBEF08u
#define NEXT_ACTION_TOKEN_RVA 0xD19510u
#define LAST_ACTION_USER_DATA_RVA 0x1038B1Cu
static volatile LONG xp_thresholds_traced;
static volatile LONG xp_threshold_trace_pending;
static void* chat_observed;
static volatile LONG chat_create_attempted;
static volatile LONG chat_initialize_attempted;
static volatile LONG chat_open_pending;
static volatile LONG chat_exit_pending;
static volatile LONG basic_reconcile_pending;
static unsigned int chat_open_retry_count;
static DWORD chat_rooms_bootstrap_time;
static WNDPROC original_game_wndproc;
static HWND chat_game_window;

#define CHAT_OPEN_MESSAGE (WM_APP + 0x45)
#define CHAT_RESET_MESSAGE (WM_APP + 0x46)
#define BASIC_RECONCILE_MESSAGE (WM_APP + 0x47)
#define BASIC_RELEASE_GRACE_MS 750u

typedef struct effect_preview_offsets {
    unsigned int force_attached;
    unsigned int asset;
    unsigned int object_id;
    unsigned int position;
    unsigned int text_value;
    unsigned int client_event_id;
    unsigned int visibility_mask;
    int is_ready;
} effect_preview_offsets;

static effect_preview_offsets effect_preview_field;

enum {
    effect_preview_signature = 0x46414E47,
    effect_preview_world_mode = 0x574F524C,
};

static unsigned int paste_chat_clipboard(HWND window) {
    HANDLE clipboard_data;
    const WCHAR* wide_text;
    char text[2048];
    int length;
    unsigned int index;
    unsigned int pasted = 0;
    if (window == NULL || original_game_wndproc == NULL || !OpenClipboard(window)) {
        return 0;
    }
    clipboard_data = GetClipboardData(CF_UNICODETEXT);
    if (clipboard_data == NULL) {
        CloseClipboard();
        return 0;
    }
    wide_text = (const WCHAR*)GlobalLock(clipboard_data);
    if (wide_text == NULL) {
        CloseClipboard();
        return 0;
    }
    length = WideCharToMultiByte(CP_ACP, 0, wide_text, -1, text, sizeof(text), NULL, NULL);
    GlobalUnlock(clipboard_data);
    CloseClipboard();
    if (length <= 0) {
        return 0;
    }
    for (index = 0; index + 1 < (unsigned int)length; index++) {
        unsigned char character = (unsigned char)text[index];
        if (character == '\r') {
            continue;
        }
        if (character == '\n' || character == '\t') {
            character = ' ';
        }
        CallWindowProcA(original_game_wndproc, window, WM_CHAR, character, 1);
        pasted++;
    }
    return pasted;
}

static unsigned int repair_chat_rooms_target(void);
static unsigned int prepare_chat_rooms_target(void);
static void trace_rooms_roster(void);
static void trace_party_navigation_gate(void* party, unsigned char is_ready);
static void* traced_spline_camera;
static int traced_spline_nodes;
static float traced_spline_time = -1.0f;
static void trace_client_state(const char* kind, unsigned int value);
static uintptr_t trace_caller_rva(void* caller);
static void trace_client_message_payload(unsigned char id, const void* data, unsigned int size);
static void trace_raknet_payload(const char* direction, const char* kind, SOCKET socket,
    const struct sockaddr* address, const char* buffer, int length);
static int is_party_socket(SOCKET socket);
static void snapshot_ring_clear(void);
static void snapshot_ring_append(const char* contents, unsigned int size);
static void snapshot_ring_marker(const char* kind, const char* request,
    unsigned long long server_time_unix_nano, unsigned int buffer_ms);
static int valid_snapshot_directory_name(const char* name);
static void snapshot_ring_dump(
    const char* directory_name, unsigned int buffer_ms);
static void trace_client_object_registry(void);
static void trace_client_object_probe_registry(void);
static void trace_snapshot_action_state(void);

static float __cdecl hooked_frame_delta(void* frame) {
    float delta = original_frame_delta(frame);
    unsigned int delta_bits;
    DWORD probe_time;
    memcpy(&delta_bits, &delta, sizeof(delta_bits));
    InterlockedIncrement(&snapshot_frame_sequence);
    InterlockedExchange(&snapshot_frame_delta_bits, (LONG)delta_bits);
    InterlockedExchange(&snapshot_frame_time_ms, (LONG)GetTickCount());
    if (InterlockedCompareExchange(&snapshot_keyframe_pending, 2, 1) == 1) {
        trace_client_object_registry();
        trace_snapshot_action_state();
        if (snapshot_keyframe_event != NULL) {
            SetEvent(snapshot_keyframe_event);
        }
    }
    probe_time = GetTickCount();
    if (InterlockedCompareExchange(&snapshot_auto_probe_enabled, 0, 0) != 0 &&
        probe_time - snapshot_last_probe_time >= 250u) {
        snapshot_last_probe_time = probe_time;
        trace_client_object_probe_registry();
    }
    return delta;
}

static DWORD WINAPI watch_snapshot_control(LPVOID parameter) {
    char last_request[256];
    (void)parameter;
    ZeroMemory(last_request, sizeof(last_request));
    for (;;) {
        HANDLE control;
        char contents[1024];
        char request[256];
        char* mode;
        char* request_line;
        char* buffer_line;
        char* server_time_line;
        DWORD read_count = 0;
        unsigned int buffer_ms = 30000;
        unsigned long long server_time_unix_nano = 0;
        ZeroMemory(contents, sizeof(contents));
        ZeroMemory(request, sizeof(request));
        control = CreateFileA(
            snapshot_control_path, GENERIC_READ,
            FILE_SHARE_READ | FILE_SHARE_WRITE | FILE_SHARE_DELETE,
            NULL, OPEN_EXISTING, FILE_ATTRIBUTE_NORMAL, NULL);
        if (control != INVALID_HANDLE_VALUE) {
            ReadFile(control, contents, (DWORD)(sizeof(contents) - 1), &read_count, NULL);
            CloseHandle(control);
            if (read_count != 0) {
                mode = strstr(contents, "mode=");
                if (mode != NULL) {
                    mode += 5;
                }
                if (mode != NULL && _strnicmp(mode, "auto", 4) == 0) {
                    InterlockedExchange(&snapshot_capture_enabled, 1);
                    InterlockedExchange(&snapshot_auto_probe_enabled, 1);
                } else if (mode != NULL && _strnicmp(mode, "manual", 6) == 0) {
                    InterlockedExchange(&snapshot_capture_enabled, 1);
                    InterlockedExchange(&snapshot_auto_probe_enabled, 0);
                } else if (mode != NULL && _strnicmp(mode, "off", 3) == 0) {
                    InterlockedExchange(&snapshot_auto_probe_enabled, 0);
                    if (InterlockedExchange(&snapshot_capture_enabled, 0) != 0) {
                        snapshot_ring_clear();
                    }
                }
                request_line = strstr(contents, "request=");
                if (request_line != NULL) {
                    size_t request_length;
                    request_line += 8;
                    request_length = strcspn(request_line, "\r\n");
                    if (request_length >= sizeof(request)) {
                        request_length = sizeof(request) - 1;
                    }
                    memcpy(request, request_line, request_length);
                    request[request_length] = '\0';
                }
                buffer_line = strstr(contents, "buffer_ms=");
                if (buffer_line != NULL) {
                    unsigned long parsed = strtoul(buffer_line + 10, NULL, 10);
                    if (parsed >= 1000 && parsed <= 300000) {
                        buffer_ms = (unsigned int)parsed;
                    }
                }
                server_time_line = strstr(contents, "server_time_unix_nano=");
                if (server_time_line != NULL) {
                    server_time_unix_nano = _strtoui64(
                        server_time_line + 22, NULL, 10);
                }
                if (request[0] != '\0' && strcmp(request, last_request) != 0 &&
                    valid_snapshot_directory_name(request) &&
                    InterlockedCompareExchange(&snapshot_capture_enabled, 0, 0) != 0) {
                    strncpy(last_request, request, sizeof(last_request) - 1);
                    last_request[sizeof(last_request) - 1] = '\0';
                    if (snapshot_keyframe_event != NULL) {
                        ResetEvent(snapshot_keyframe_event);
                        InterlockedExchange(&snapshot_keyframe_pending, 1);
                        WaitForSingleObject(snapshot_keyframe_event, 250);
                        InterlockedExchange(&snapshot_keyframe_pending, 0);
                    }
                    snapshot_ring_marker(
                        "snapshot_boundary", request, server_time_unix_nano,
                        buffer_ms);
                    snapshot_ring_dump(request, buffer_ms);
                }
            }
        }
        Sleep(250);
    }
}

typedef struct mana_cost_trace {
    uintptr_t caller_rva;
    void* ability;
    void* actor;
    int rank;
    float property;
    float coefficient;
    double cost;
} mana_cost_trace;

static mana_cost_trace traced_mana_cost[64];
static unsigned int traced_mana_cost_count;

static void trace_mana_cost(void* caller, void* ability, void* actor, int rank,
    float property, float coefficient, double cost) {
    char line[512];
    DWORD written;
    uintptr_t caller_rva;
    unsigned int index;
    int line_length;
    double base;
    if (trace_file == INVALID_HANDLE_VALUE || !trace_lock_ready) {
        return;
    }
    caller_rva = trace_caller_rva(caller);
    if (caller_rva >= 5) {
        caller_rva -= 5;
    }
    base = cost == 0.0 ? 0.0 : cost - property * coefficient;
    EnterCriticalSection(&trace_lock);
    for (index = 0; index < traced_mana_cost_count; index++) {
        const mana_cost_trace* traced = &traced_mana_cost[index];
        if (traced->caller_rva == caller_rva && traced->ability == ability &&
            traced->actor == actor && traced->rank == rank &&
            traced->property == property && traced->coefficient == coefficient &&
            traced->cost == cost) {
            LeaveCriticalSection(&trace_lock);
            return;
        }
    }
    index = traced_mana_cost_count < 64 ? traced_mana_cost_count++ : 63;
    traced_mana_cost[index].caller_rva = caller_rva;
    traced_mana_cost[index].ability = ability;
    traced_mana_cost[index].actor = actor;
    traced_mana_cost[index].rank = rank;
    traced_mana_cost[index].property = property;
    traced_mana_cost[index].coefficient = coefficient;
    traced_mana_cost[index].cost = cost;
    line_length = snprintf(line, sizeof(line),
        "{\"time_ms\":%llu,\"protocol\":\"client\",\"kind\":\"mana_cost\",\"thread\":%lu,"
        "\"caller_rva\":\"0x%llx\",\"ability\":\"0x%08lx\",\"actor\":\"0x%08lx\","
        "\"rank\":%d,\"property\":%.9g,\"coefficient\":%.9g,\"base\":%.9g,\"cost\":%.9g}\r\n",
        (unsigned long long)GetTickCount(), (unsigned long)GetCurrentThreadId(),
        (unsigned long long)caller_rva,
        (unsigned long)(uintptr_t)ability, (unsigned long)(uintptr_t)actor,
        rank, property, coefficient, base, cost);
    if (line_length <= 0) {
        LeaveCriticalSection(&trace_lock);
        return;
    }
    WriteFile(trace_file, line, (DWORD)line_length, &written, NULL);
    LeaveCriticalSection(&trace_lock);
}

static double __cdecl hooked_mana_cost(void* ability, void* actor, int rank, float property) {
    void* caller = __builtin_return_address(0);
    double cost = original_mana_cost(ability, actor, rank, property);
    float coefficient = 0.0f;
    if (ability != NULL && rank >= 0 && rank <= 8) {
        coefficient = *(const float*)((const BYTE*)ability + 448 + (rank * 4));
    }
    trace_mana_cost(caller, ability, actor, rank, property, coefficient, cost);
    return cost;
}
static uint64_t trace_digest(const char* buffer, int length);
static int readable_pointer(const void* pointer);
static int readable_range(const void* pointer, size_t size);

typedef struct pending_sporenet_script {
    UINT_PTR timer;
    void* host;
    wchar_t* script;
    struct pending_sporenet_script* next;
} pending_sporenet_script;

static pending_sporenet_script* pending_sporenet_script_head;
static VOID CALLBACK sporenet_script_timer_callback(HWND window, UINT message, UINT_PTR timer, DWORD time);

static size_t bounded_text_length(const char* begin, const char* end, size_t maximum) {
    size_t length;
    if (begin == NULL || end == NULL || end < begin) {
        return 0;
    }
    length = (size_t)(end - begin);
    return length > maximum ? maximum : length;
}

static const char* sporenet_deferred_callback_name(void* request, size_t* name_length) {
    static const char account_callback[] = "accountinfocallback";
    static const char creature_callback[] = "spgetcreaturecallback";
    static const char player_profile_callback[] = "spgetplayerprofilecallback";
    static const char leaderboard_callback[] = "spgetleaderboardcallback";
    static const char social_search_callback[] = "getcallback";
    const char* callback;
    const char* callback_end;
    size_t callback_length;
    if (name_length != NULL) {
        *name_length = 0;
    }
    if (request == NULL) {
        return NULL;
    }
    callback = *(const char**)((BYTE*)request + 72);
    callback_end = *(const char**)((BYTE*)request + 76);
    callback_length = bounded_text_length(callback, callback_end, 128);
    if (callback_length == sizeof(account_callback) - 1 &&
        memcmp(callback, account_callback, callback_length) == 0) {
        if (name_length != NULL) {
            *name_length = callback_length;
        }
        return account_callback;
    }
    if (callback_length == sizeof(creature_callback) - 1 &&
        memcmp(callback, creature_callback, callback_length) == 0) {
        if (name_length != NULL) {
            *name_length = callback_length;
        }
        return creature_callback;
    }
    if (callback_length == sizeof(player_profile_callback) - 1 &&
        memcmp(callback, player_profile_callback, callback_length) == 0) {
        if (name_length != NULL) {
            *name_length = callback_length;
        }
        return player_profile_callback;
    }
    if (callback_length == sizeof(leaderboard_callback) - 1 &&
        memcmp(callback, leaderboard_callback, callback_length) == 0) {
        if (name_length != NULL) {
            *name_length = callback_length;
        }
        return leaderboard_callback;
    }
    if (callback_length == sizeof(social_search_callback) - 1 &&
        memcmp(callback, social_search_callback, callback_length) == 0) {
        if (name_length != NULL) {
            *name_length = callback_length;
        }
        return social_search_callback;
    }
    return NULL;
}

static int is_sporenet_callback_matching(void* request, const char* expected) {
    const char* callback;
    const char* callback_end;
    size_t callback_length;
    size_t expected_length;
    if (request == NULL || expected == NULL) {
        return 0;
    }
    callback = *(const char**)((BYTE*)request + 72);
    callback_end = *(const char**)((BYTE*)request + 76);
    callback_length = bounded_text_length(callback, callback_end, 128);
    expected_length = strlen(expected);
    return callback_length == expected_length && memcmp(callback, expected, expected_length) == 0;
}

static int is_response_containing(void* response, const char* expected) {
    void* body_owner;
    void** response_vtable;
    const char* body;
    unsigned int body_length;
    size_t expected_length;
    unsigned int index;
    if (response == NULL || expected == NULL) {
        return 0;
    }
    body_owner = *((void**)response + 1);
    response_vtable = *(void***)response;
    if (body_owner == NULL || response_vtable == NULL || response_vtable[7] == NULL) {
        return 0;
    }
    body = *(const char**)((BYTE*)body_owner + 8);
    body_length = ((unsigned int (FANG_THISCALL*)(void*))response_vtable[7])(response);
    expected_length = strlen(expected);
    if (body == NULL || expected_length == 0 || expected_length > body_length) {
        return 0;
    }
    for (index = 0; index + expected_length <= body_length; index++) {
        if (memcmp(body + index, expected, expected_length) == 0) {
            return 1;
        }
    }
    return 0;
}

static int is_sporenet_profile_request(void* request) {
    return sporenet_deferred_callback_name(request, NULL) != NULL;
}

static int queue_sporenet_static_script(void* request, const wchar_t* script) {
    pending_sporenet_script* pending;
    size_t script_length;
    if (request == NULL || script == NULL) {
        return 0;
    }
    pending = (pending_sporenet_script*)HeapAlloc(GetProcessHeap(), HEAP_ZERO_MEMORY, sizeof(*pending));
    if (pending == NULL) {
        return 0;
    }
    script_length = wcslen(script);
    pending->script = (wchar_t*)HeapAlloc(GetProcessHeap(), 0, (script_length + 1) * sizeof(wchar_t));
    if (pending->script == NULL) {
        HeapFree(GetProcessHeap(), 0, pending);
        return 0;
    }
    memcpy(pending->script, script, (script_length + 1) * sizeof(wchar_t));
    pending->host = *((void**)request + 22);
    if (pending->host == NULL) {
        HeapFree(GetProcessHeap(), 0, pending->script);
        HeapFree(GetProcessHeap(), 0, pending);
        return 0;
    }
    pending->timer = SetTimer(NULL, 0, 1, sporenet_script_timer_callback);
    if (pending->timer == 0) {
        HeapFree(GetProcessHeap(), 0, pending->script);
        HeapFree(GetProcessHeap(), 0, pending);
        return 0;
    }
    pending->next = pending_sporenet_script_head;
    pending_sporenet_script_head = pending;
    return 1;
}

static int queue_owned_account_refresh(void* request, void* response) {
    static const char callback_name[] = "accountinfocallback";
    static const char online_access[] = "<grant_online_access>1</grant_online_access>";
    static const wchar_t refresh_script[] =
        L"(function(){var n=0,r=0;function p(w){try{if(!w||!w.showupdates||w.__darkspinFeed)return 0;"
        L"var o=w.showupdates;w.showupdates=function(tab){o(tab);var u=tab==='mine'?w.mineupdates:w.friendsupdates;"
        L"var c=w.document.getElementById('Updates_Content_Frame');if(!c||!u)return;for(var x in u){var e=u[x];"
        L"if(String(e.messageid)!=='4')continue;var d=w.document.createElement('div');d.className='update_frame';"
        L"var a=w.document.createElement('img');a.src='http://'+w.EnvironmentURL+'/game/service/png?account_id='+e.accountid;"
        L"a.width=32;a.height=32;a.style.cssText='position:relative;top:2px;left:2px';d.appendChild(a);"
        L"var q=w.document.createElement('span');q.className='update_time_font';q.style.cssText='position:relative;top:-18px;left:9px';"
        L"var z=new Date(Number(e.time)*1000);q.appendChild(w.document.createTextNode(w.formatlocaldate(z)+' '+z.toLocaleTimeString()));"
        L"d.appendChild(q);d.appendChild(w.document.createElement('br'));var m=w.document.createElement('span');"
        L"m.className='update_main_font';m.style.cssText='position:relative;top:-16px;left:42px';"
        L"var s=w.document.createElement('span');s.style.color='#b0c5ff';s.appendChild(w.document.createTextNode(' '+e.name+' '));"
        L"m.appendChild(s);m.appendChild(w.document.createTextNode(e.metadata));d.appendChild(m);c.appendChild(d);}};"
        L"w.__darkspinFeed=1;w.showupdates('mine');return 1;}catch(e){return 0;}}"
        L"var t=setInterval(function(){var f=window.frames&&window.frames['FRAME3'];if(f&&!r&&f.hidestore&&f.showScreen){"
        L"var h=f.document&&f.document.getElementById('Tab_5_Frame');if(h&&window.Client&&window.Client.getWebHost){"
        L"h.src='http://'+window.Client.getWebHost()+'/web/sporelabsgame/manualen';}f.hidestore();f.showScreen('myprofile');r=1;}"
        L"var k=0;if(f){k|=p(f);for(var i=0;i<f.frames.length;i++)k|=p(f.frames[i]);}"
        L"if((r&&k)||++n>=200)clearInterval(t);},50);})();";
    if (!is_sporenet_callback_matching(request, callback_name) || !is_response_containing(response, online_access)) {
        return 0;
    }
    return queue_sporenet_static_script(request, refresh_script);
}

static void trace_sporenet_callback(void* request, void* response, const char* phase) {
    const char* method;
    const char* method_end;
    const char* callback;
    const char* callback_end;
    const char* body = NULL;
    void* body_owner;
    void** response_vtable;
    unsigned int body_length = 0;
    uint64_t body_digest = 0;
    char line[768];
    DWORD written;
    int line_length;
    size_t method_length;
    size_t callback_length;
    if (trace_file == INVALID_HANDLE_VALUE || !trace_lock_ready || request == NULL) {
        return;
    }
    method = *(const char**)((BYTE*)request + 8);
    method_end = *(const char**)((BYTE*)request + 12);
    callback = *(const char**)((BYTE*)request + 72);
    callback_end = *(const char**)((BYTE*)request + 76);
    method_length = bounded_text_length(method, method_end, 128);
    callback_length = bounded_text_length(callback, callback_end, 128);
    if (sporenet_deferred_callback_name(request, NULL) == NULL) {
        return;
    }
    if (response != NULL) {
        body_owner = *((void**)response + 1);
        response_vtable = *(void***)response;
        if (body_owner != NULL && response_vtable != NULL && response_vtable[7] != NULL) {
            body = *(const char**)((BYTE*)body_owner + 8);
            body_length = ((unsigned int (__attribute__((thiscall))*)(void*))response_vtable[7])(response);
            body_digest = trace_digest(body, (int)body_length);
        }
    }
    line_length = snprintf(line, sizeof(line),
        "{\"time_ms\":%llu,\"protocol\":\"client\",\"kind\":\"sporenet_callback\",\"phase\":\"%s\",\"thread\":%lu,\"method\":\"%.*s\",\"callback\":\"%.*s\",\"body_length\":%u,\"body_digest\":\"%016llx\"}\r\n",
        (unsigned long long)GetTickCount(), phase, (unsigned long)GetCurrentThreadId(),
        (int)method_length, method, (int)callback_length, callback, body_length,
        (unsigned long long)body_digest);
    if (line_length <= 0) {
        return;
    }
    EnterCriticalSection(&trace_lock);
    WriteFile(trace_file, line, (DWORD)line_length, &written, NULL);
    LeaveCriticalSection(&trace_lock);
}

static int queue_sporenet_profile_callback(void* request, void* response) {
    static const wchar_t account_prefix[] = L"try{accountinfocallback(decodeURIComponent('";
    static const wchar_t creature_prefix[] = L"spgetcreaturecallback(decodeURIComponent('";
    static const wchar_t player_profile_prefix[] = L"try{spgetplayerprofilecallback(decodeURIComponent('";
    static const wchar_t leaderboard_prefix[] =
        L"try{var c=spgetleaderboardcallback;c(decodeURIComponent('";
    static const wchar_t social_search_prefix[] =
        L"try{var c=getcallback;c(decodeURIComponent('";
    static const wchar_t creature_suffix[] = L"'));";
    static const wchar_t account_suffix[] = L"'));}catch(e){}";
    static const wchar_t player_profile_suffix[] =
        L"'));}finally{try{var i=document.getElementById('Loading_Icon');"
        L"if(i)i.style.visibility='hidden';}catch(e){}}";
    static const wchar_t leaderboard_suffix[] =
        L"'));}finally{try{var i=document.getElementById('Loader_Frame');"
        L"if(i)i.style.visibility='hidden';}catch(e){}}";
    static const wchar_t social_search_suffix[] =
        L"'));}finally{try{var i=document.getElementById('Get_Friends');"
        L"if(i)i.style.visibility='hidden';}catch(e){}}";
    static const wchar_t hex[] = L"0123456789ABCDEF";
    const char* callback_name;
    const wchar_t* prefix;
    const wchar_t* suffix;
    pending_sporenet_script* pending;
    const char* body;
    void* body_owner;
    void** response_vtable;
    unsigned int body_length;
    size_t prefix_length;
    size_t suffix_length;
    size_t script_length;
    size_t index;
    wchar_t* cursor;
    if (request == NULL || response == NULL) {
        return 0;
    }
    callback_name = sporenet_deferred_callback_name(request, NULL);
    if (callback_name == NULL) {
        return 0;
    }
    if (strcmp(callback_name, "accountinfocallback") == 0) {
        prefix = account_prefix;
        prefix_length = sizeof(account_prefix) / sizeof(account_prefix[0]) - 1;
        suffix = account_suffix;
        suffix_length = sizeof(account_suffix) / sizeof(account_suffix[0]) - 1;
    } else if (strcmp(callback_name, "spgetplayerprofilecallback") == 0) {
        prefix = player_profile_prefix;
        prefix_length = sizeof(player_profile_prefix) / sizeof(player_profile_prefix[0]) - 1;
        suffix = player_profile_suffix;
        suffix_length = sizeof(player_profile_suffix) / sizeof(player_profile_suffix[0]) - 1;
    } else if (strcmp(callback_name, "spgetleaderboardcallback") == 0) {
        prefix = leaderboard_prefix;
        prefix_length = sizeof(leaderboard_prefix) / sizeof(leaderboard_prefix[0]) - 1;
        suffix = leaderboard_suffix;
        suffix_length = sizeof(leaderboard_suffix) / sizeof(leaderboard_suffix[0]) - 1;
    } else if (strcmp(callback_name, "getcallback") == 0) {
        prefix = social_search_prefix;
        prefix_length = sizeof(social_search_prefix) / sizeof(social_search_prefix[0]) - 1;
        suffix = social_search_suffix;
        suffix_length = sizeof(social_search_suffix) / sizeof(social_search_suffix[0]) - 1;
    } else {
        prefix = creature_prefix;
        prefix_length = sizeof(creature_prefix) / sizeof(creature_prefix[0]) - 1;
        suffix = creature_suffix;
        suffix_length = sizeof(creature_suffix) / sizeof(creature_suffix[0]) - 1;
    }
    body_owner = *((void**)response + 1);
    response_vtable = *(void***)response;
    if (body_owner == NULL || response_vtable == NULL || response_vtable[7] == NULL) {
        return 0;
    }
    body = *(const char**)((BYTE*)body_owner + 8);
    body_length = ((unsigned int (FANG_THISCALL*)(void*))response_vtable[7])(response);
    if (body == NULL || body_length > 1024 * 1024) {
        return 0;
    }
    script_length = prefix_length + (size_t)body_length * 3 + suffix_length;
    pending = (pending_sporenet_script*)HeapAlloc(GetProcessHeap(), HEAP_ZERO_MEMORY, sizeof(*pending));
    if (pending == NULL) {
        return 0;
    }
    pending->script = (wchar_t*)HeapAlloc(GetProcessHeap(), 0, (script_length + 1) * sizeof(wchar_t));
    if (pending->script == NULL) {
        HeapFree(GetProcessHeap(), 0, pending);
        return 0;
    }
    memcpy(pending->script, prefix, prefix_length * sizeof(wchar_t));
    cursor = pending->script + prefix_length;
    for (index = 0; index < body_length; index++) {
        unsigned char byte = (unsigned char)body[index];
        *cursor++ = L'%';
        *cursor++ = hex[byte >> 4];
        *cursor++ = hex[byte & 0x0F];
    }
    memcpy(cursor, suffix, suffix_length * sizeof(wchar_t));
    pending->script[script_length] = L'\0';
    pending->host = *((void**)request + 22);
    if (pending->host == NULL) {
        HeapFree(GetProcessHeap(), 0, pending->script);
        HeapFree(GetProcessHeap(), 0, pending);
        return 0;
    }
    pending->timer = SetTimer(NULL, 0, 1, sporenet_script_timer_callback);
    if (pending->timer == 0) {
        HeapFree(GetProcessHeap(), 0, pending->script);
        HeapFree(GetProcessHeap(), 0, pending);
        return 0;
    }
    pending->next = pending_sporenet_script_head;
    pending_sporenet_script_head = pending;
    return 1;
}

static VOID CALLBACK sporenet_script_timer_callback(HWND window, UINT message, UINT_PTR timer, DWORD time) {
    pending_sporenet_script** link = &pending_sporenet_script_head;
    pending_sporenet_script* pending;
    void** host_vtable;
    web_script_fn execute_script;
    (void)window;
    (void)message;
    (void)time;
    while (*link != NULL && (*link)->timer != timer) {
        link = &(*link)->next;
    }
    pending = *link;
    if (pending == NULL) {
        return;
    }
    *link = pending->next;
    KillTimer(NULL, timer);
    host_vtable = readable_pointer(pending->host) ? *(void***)pending->host : NULL;
    execute_script = host_vtable == NULL ? NULL : (web_script_fn)host_vtable[16];
    if (execute_script != NULL) {
        execute_script(pending->host, pending->script);
        trace_client_state("sporenet_profile_delivered", 1);
    } else {
        trace_client_state("sporenet_profile_delivered", 0);
    }
    HeapFree(GetProcessHeap(), 0, pending->script);
    HeapFree(GetProcessHeap(), 0, pending);
}

static void FANG_THISCALL hooked_sporenet_callback(void* request, void* response) {
    if (is_sporenet_callback_matching(request, "accountinfocallback")) {
        int is_profile_queued;
        int is_refresh_queued;
        trace_sporenet_callback(request, response, "dispatch");
        is_profile_queued = queue_sporenet_profile_callback(request, response);
        is_refresh_queued = queue_owned_account_refresh(request, response);
        InterlockedExchange(&jwt_login_completed, 1);
        trace_client_state("sporenet_profile_queued", (unsigned int)is_profile_queued);
        trace_client_state("owned_account_refresh_queued", (unsigned int)is_refresh_queued);
        if (!is_profile_queued) {
            original_sporenet_callback(request, response);
            trace_sporenet_callback(request, response, "returned");
        }
        return;
    }
    if (!is_sporenet_profile_request(request)) {
        original_sporenet_callback(request, response);
        return;
    }
    trace_sporenet_callback(request, response, "dispatch");
    if (queue_sporenet_profile_callback(request, response)) {
        trace_client_state("sporenet_profile_queued", 1);
        return;
    }
    trace_client_state("sporenet_profile_queued", 0);
    original_sporenet_callback(request, response);
    trace_sporenet_callback(request, response, "returned");
}
static VOID CALLBACK jwt_login_timer_callback(HWND window, UINT message, UINT_PTR timer, DWORD time);

static int write_login_failure_result(void) {
    char temporary_path[2100];
    char document[768];
    HANDLE output;
    DWORD written;
    int document_length;
    int temporary_length;
    if (client_result_path[0] == '\0' || client_launch_id[0] == '\0') {
        return 0;
    }
    temporary_length = snprintf(temporary_path, sizeof(temporary_path), "%s.%lu.tmp",
        client_result_path, (unsigned long)GetCurrentProcessId());
    if (temporary_length <= 0 || temporary_length >= (int)sizeof(temporary_path)) {
        return 0;
    }
    document_length = snprintf(document, sizeof(document),
        "{\"pid\":%lu,\"launch_id\":\"%s\",\"stage\":\"account-profile\","
        "\"reason\":\"client-callback-timeout\","
        "\"message\":\"Game received the account response but did not finish loading it.\","
        "\"time_ms\":%lu}\r\n",
        (unsigned long)GetCurrentProcessId(), client_launch_id, (unsigned long)GetTickCount());
    if (document_length <= 0 || document_length >= (int)sizeof(document)) {
        return 0;
    }
    output = CreateFileA(temporary_path, GENERIC_WRITE, 0, NULL, CREATE_ALWAYS,
        FILE_ATTRIBUTE_NORMAL, NULL);
    if (output == INVALID_HANDLE_VALUE) {
        return 0;
    }
    if (!WriteFile(output, document, (DWORD)document_length, &written, NULL) ||
        written != (DWORD)document_length || !FlushFileBuffers(output)) {
        CloseHandle(output);
        DeleteFileA(temporary_path);
        return 0;
    }
    CloseHandle(output);
    if (!MoveFileExA(temporary_path, client_result_path,
        MOVEFILE_REPLACE_EXISTING | MOVEFILE_WRITE_THROUGH)) {
        DeleteFileA(temporary_path);
        return 0;
    }
    return 1;
}

static DWORD WINAPI jwt_login_watchdog_thread(LPVOID parameter) {
    (void)parameter;
    Sleep(30000);
    if (InterlockedCompareExchange(&jwt_login_completed, 0, 0) != 0) {
        return 0;
    }
    trace_client_state("jwt_login_watchdog_expired", 1);
    if (write_login_failure_result()) {
        ExitProcess(0xD15C0001u);
    }
    return 0;
}

static void start_jwt_login_watchdog(void) {
    HANDLE thread;
    if (client_result_path[0] == '\0' ||
        InterlockedCompareExchange(&jwt_login_watchdog_started, 1, 0) != 0) {
        return;
    }
    thread = CreateThread(NULL, 0, jwt_login_watchdog_thread, NULL, 0, NULL);
    if (thread == NULL) {
        InterlockedExchange(&jwt_login_watchdog_started, 0);
        trace_client_state("jwt_login_watchdog_failed", GetLastError());
        return;
    }
    CloseHandle(thread);
}

static void prepare_chat_ui(void) {
    void* chat = original_chat_lookup();
    if (chat == NULL) {
        if (chat_observed != NULL) {
            chat_observed = NULL;
            InterlockedExchange(&chat_create_attempted, 0);
            InterlockedExchange(&chat_initialize_attempted, 0);
        }
        if (InterlockedCompareExchange(&chat_create_attempted, 1, 0) != 0) {
            return;
        }
        chat = original_chat_create();
        chat_observed = chat;
        InterlockedExchange(&chat_initialize_attempted, chat != NULL);
        trace_client_state("chat_create", chat != NULL);
        return;
    }
    if (chat != chat_observed) {
        chat_observed = chat;
        InterlockedExchange(&chat_initialize_attempted, 0);
    }
    if (*((const BYTE*)chat + 40) != 0 ||
        InterlockedCompareExchange(&chat_initialize_attempted, 1, 0) != 0) {
        return;
    }
    original_chat_initialize(chat);
    trace_client_state("chat_initialize", 1);
}

static int open_chat_ui(void) {
    void* chat;
    prepare_chat_ui();
    chat = original_chat_lookup();
    if (chat == NULL || *((const BYTE*)chat + 40) == 0) {
        return 0;
    }
    if (*((const BYTE*)chat + 41) == 0) {
        *((DWORD*)chat + 11) = 2;
        *((BYTE*)chat + 42) = 1;
        original_chat_show(chat, 1);
        trace_client_state("chat_gameplay_show", *((const BYTE*)chat + 41));
    }
    original_chat_open(chat, 0x10000009);
    trace_client_state("chat_open", 1);
    return 1;
}

static unsigned int clear_pending_action_descriptors(void) {
    BYTE* movement;
    BYTE* ability;
    BYTE* auxiliary;
    unsigned int cleared = 0;
    if (executable_module == NULL) {
        return 0;
    }
    movement = (BYTE*)executable_module + PENDING_MOVEMENT_ACTIVE_RVA;
    ability = (BYTE*)executable_module + PENDING_ABILITY_ACTIVE_RVA;
    auxiliary = (BYTE*)executable_module + PENDING_AUXILIARY_ACTIVE_RVA;
    if (readable_pointer(movement)) {
        *movement = 0;
        cleared |= 1;
    }
    if (readable_pointer(ability)) {
        *ability = 0;
        cleared |= 2;
    }
    if (readable_pointer(auxiliary)) {
        *auxiliary = 0;
        cleared |= 4;
    }
    return cleared;
}

static int clear_input_action_intent(void) {
    BYTE* input;
    if (executable_module == NULL) {
        return 0;
    }
    input = (BYTE*)executable_module + INPUT_INTENT_RVA;
    if (!readable_range(input, 69)) {
        return 0;
    }
    *(unsigned int*)(input + 4) = 0;
    *(unsigned int*)(input + 8) = 0;
    *(unsigned int*)(input + 12) = 0;
    *(unsigned int*)(input + 28) = (unsigned int)-1;
    input[32] = 0;
    input[52] = 0;
    input[60] = 0;
    input[68] = 0;
    return 1;
}

static void trace_input_action_state(const char* boundary) {
    const BYTE* input;
    const BYTE* movement;
    const BYTE* ability;
    const BYTE* auxiliary;
    const BYTE* next_token;
    const BYTE* last_user_data;
    trace_client_state(boundary, 1);
    if (executable_module == NULL) {
        trace_client_state("input_action_state_missing", 1);
        return;
    }
    input = (const BYTE*)executable_module + INPUT_INTENT_RVA;
    movement = (const BYTE*)executable_module + PENDING_MOVEMENT_ACTIVE_RVA;
    ability = (const BYTE*)executable_module + PENDING_ABILITY_ACTIVE_RVA;
    auxiliary = (const BYTE*)executable_module + PENDING_AUXILIARY_ACTIVE_RVA;
    next_token = (const BYTE*)executable_module + NEXT_ACTION_TOKEN_RVA;
    last_user_data = (const BYTE*)executable_module + LAST_ACTION_USER_DATA_RVA;
    if (readable_range(input, 69)) {
        trace_client_state("input_mouse_mode", *(const unsigned int*)(input + 4));
        trace_client_state("input_retained_target", *(const unsigned int*)(input + 8));
        trace_client_state("input_fallback_active", *(const unsigned int*)(input + 12));
        trace_client_state("input_selected_action", *(const unsigned int*)(input + 28));
        trace_client_state("input_move_press", *(const unsigned char*)(input + 32));
        trace_client_state("input_move_hold", *(const unsigned char*)(input + 52));
        trace_client_state("input_basic_press", *(const unsigned char*)(input + 60));
        trace_client_state("input_basic_repeat", *(const unsigned char*)(input + 68));
    }
    if (readable_range(movement, 2)) {
        trace_client_state("pending_movement_active", movement[0]);
        trace_client_state("pending_movement_sent", movement[1]);
    }
    if (readable_range(ability, 36)) {
        trace_client_state("pending_ability_active", ability[0]);
        trace_client_state("pending_ability_sent", ability[1]);
        trace_client_state("pending_ability_sync", ability[2]);
        trace_client_state("pending_ability_type", ability[3]);
        trace_client_state("pending_ability_definition", *(const unsigned int*)(ability + 4));
        trace_client_state("pending_ability_target", *(const unsigned int*)(ability + 32));
    }
    if (readable_range(auxiliary, 8)) {
        trace_client_state("pending_auxiliary_active", auxiliary[0]);
        trace_client_state("pending_auxiliary_sent", auxiliary[1]);
        trace_client_state("pending_auxiliary_sync", auxiliary[2]);
        trace_client_state("pending_auxiliary_type", *(const unsigned int*)(auxiliary + 4));
    }
    if (readable_pointer(next_token)) {
        trace_client_state("action_next_token", *next_token);
    }
    if (readable_range(last_user_data, 4)) {
        trace_client_state("action_last_user_data", *(const unsigned int*)last_user_data);
    }
}

static void trace_snapshot_action_state(void) {
    const BYTE* state;
    const BYTE* player;
    unsigned int controlled_object_id;
    trace_client_state("action_snapshot_boundary", 1);
    state = original_combat_input_state != NULL ?
        (const BYTE*)original_combat_input_state() : NULL;
    if (readable_range(state, 64)) {
        trace_client_state("action_active_definition", *(const unsigned int*)(state + 4));
        trace_client_state("action_active_sync", *(const unsigned char*)(state + 8));
        trace_client_state("action_active_deadline", *(const unsigned int*)(state + 16));
        trace_client_state("action_response_definition", *(const unsigned int*)(state + 24));
        trace_client_state("action_response_sync", *(const unsigned char*)(state + 28));
        trace_client_state("action_response_end", *(const unsigned int*)(state + 40));
        trace_client_state("action_queued_definition", *(const unsigned int*)(state + 52));
        trace_client_state("action_queued_sync", *(const unsigned char*)(state + 56));
        trace_client_state("action_retained_target", *(const unsigned int*)(state + 60));
    } else {
        trace_client_state("action_state_missing", 1);
    }
    trace_input_action_state("input_snapshot_boundary");
    controlled_object_id = (unsigned int)InterlockedCompareExchange(
        &diagnostic_controlled_object_id, 0, 0);
    trace_client_state("combat_controlled_object", controlled_object_id);
    player = original_controlled_player != NULL ?
        (const BYTE*)original_controlled_player() : NULL;
    if (readable_range(player, 4668)) {
        trace_client_state("combat_controlled_handle", *(const unsigned int*)(player + 4664));
        trace_client_state("combat_player_index", *(const unsigned char*)(player + 4660));
    } else {
        trace_client_state("combat_controlled_missing", 1);
    }
}

static unsigned int reset_combat_input_state(void) {
    void* state = original_combat_input_state != NULL ? original_combat_input_state() : NULL;
    trace_input_action_state("input_action_reset_before");
    if (original_combat_input_reset == NULL || !readable_pointer(state) ||
        !readable_pointer((const BYTE*)state + 584)) {
        return 0;
    }
    original_combat_input_reset(state);
    {
        unsigned int result = 8 | clear_pending_action_descriptors();
        if (clear_input_action_intent()) {
            result |= 16;
        }
        trace_input_action_state("input_action_reset_after");
        return result;
    }
}

static int held_basic_needs_reconcile(void) {
    const BYTE* input;
    if (executable_module == NULL || (GetAsyncKeyState(VK_LBUTTON) & 0x8000) != 0) {
        return 0;
    }
    input = (const BYTE*)executable_module + INPUT_INTENT_RVA;
    return readable_range(input, 69) && input[68] != 0;
}

static unsigned int reconcile_released_basic_input(void) {
    BYTE* input;
    unsigned int state;
    if (!held_basic_needs_reconcile()) {
        return 0;
    }
    input = (BYTE*)executable_module + INPUT_INTENT_RVA;
    state = (unsigned int)input[60] | ((unsigned int)input[68] << 8);
    input[0] = 0;
    *(unsigned int*)(input + 4) = 0;
    *(unsigned int*)(input + 8) = 0;
    input[60] = 0;
    input[68] = 0;
    return state;
}

static LRESULT CALLBACK hooked_game_wndproc(HWND window, UINT message, WPARAM wparam, LPARAM lparam) {
    static const UINT_PTR chat_timer = 0xD45C;
    if (message == WM_KEYDOWN && (wparam == 'V' || wparam == 'v') &&
        (GetKeyState(VK_CONTROL) & 0x8000) != 0) {
        void* chat = original_chat_lookup();
        if (chat != NULL && *((const BYTE*)chat + 41) != 0) {
            unsigned int pasted = paste_chat_clipboard(window);
            trace_client_state("chat_paste", pasted);
            return 0;
        }
    }
    if ((message == WM_KEYDOWN && wparam == VK_RETURN && (lparam & (1L << 30)) == 0) ||
        message == CHAT_OPEN_MESSAGE) {
        void* chat = original_chat_lookup();
        unsigned int chat_state = chat != NULL;
        prepare_chat_rooms_target();
        if (chat != NULL) {
            chat_state |= *((const BYTE*)chat + 40) != 0 ? 2 : 0;
            chat_state |= *((const BYTE*)chat + 41) != 0 ? 4 : 0;
        }
        trace_client_state("chat_enter_state", chat_state);
        if (chat == NULL || *((const BYTE*)chat + 41) == 0) {
            if (open_chat_ui()) {
                InterlockedExchange(&chat_open_pending, 0);
                return 0;
            }
            chat_open_retry_count = 0;
            InterlockedExchange(&chat_open_pending, 1);
            SetTimer(window, chat_timer, 100, NULL);
            return 0;
        }
    }
    if (message == WM_TIMER && wparam == chat_timer && chat_open_pending != 0) {
        chat_open_retry_count++;
        if (open_chat_ui() || chat_open_retry_count >= 50) {
            KillTimer(window, chat_timer);
            InterlockedExchange(&chat_open_pending, 0);
        }
        return 0;
    }
    if (message == CHAT_RESET_MESSAGE) {
        unsigned int reset_state = reset_combat_input_state();
        if ((reset_state & 8) != 0) {
            trace_client_state("chat_reset_local", 1);
            trace_client_state("chat_reset_pending", reset_state & 7);
            trace_client_state("chat_reset_intent", (reset_state & 16) != 0);
        } else {
            trace_client_state("chat_reset_local", 0);
        }
        return 0;
    }
    if (message == BASIC_RECONCILE_MESSAGE) {
        unsigned int held_state = reconcile_released_basic_input();
        InterlockedExchange(&basic_reconcile_pending, 0);
        if (held_state != 0) {
            trace_client_state("input_basic_release_reconciled", held_state);
            trace_input_action_state("input_basic_release_after");
        }
        return 0;
    }
    return CallWindowProcA(original_game_wndproc, window, message, wparam, lparam);
}

static void install_chat_wndproc(HWND window) {
    WNDPROC current = (WNDPROC)GetWindowLongPtrA(window, GWLP_WNDPROC);
    if (current == hooked_game_wndproc) {
        chat_game_window = window;
        return;
    }
    original_game_wndproc = current;
    SetLastError(0);
    if (SetWindowLongPtrA(window, GWLP_WNDPROC, (LONG_PTR)hooked_game_wndproc) == 0 &&
        GetLastError() != 0) {
        trace_client_state("chat_wndproc", 0);
        return;
    }
    chat_game_window = window;
    trace_client_state("chat_wndproc", 1);
}

static DWORD WINAPI poll_chat_key(LPVOID parameter) {
    LONG is_return_down = 0;
#if FANG_DIAGNOSTICS
    DWORD basic_release_since = 0;
#endif
    (void)parameter;
    trace_client_state("chat_key_poll", 1);
    for (;;) {
        HWND window = chat_game_window;
        SHORT return_state = GetAsyncKeyState(VK_RETURN);
        if ((return_state & 0x8000) == 0) {
            is_return_down = 0;
        } else if (is_return_down == 0 && window != NULL && GetForegroundWindow() == window) {
            is_return_down = 1;
            trace_client_state("chat_enter_poll", 1);
            PostMessageA(window, CHAT_OPEN_MESSAGE, 0, 0);
        }
#if FANG_DIAGNOSTICS
        if (!held_basic_needs_reconcile()) {
            basic_release_since = 0;
        } else if (basic_release_since == 0) {
            basic_release_since = GetTickCount();
        } else if (window != NULL &&
            GetTickCount() - basic_release_since >= BASIC_RELEASE_GRACE_MS &&
            InterlockedCompareExchange(&basic_reconcile_pending, 1, 0) == 0) {
            if (PostMessageA(window, BASIC_RECONCILE_MESSAGE, 0, 0) != 0) {
                basic_release_since = 0;
            } else {
                InterlockedExchange(&basic_reconcile_pending, 0);
            }
        }
#endif
        Sleep(10);
    }
}

static void hooked_local_event(unsigned int event_id, void* event) {
    if (event_id == 0x615F3861 && event != NULL) {
        const unsigned char* value = (const unsigned char*)event;
        trace_client_state("loot_awarded_event", event_id);
        trace_client_state("loot_awarded_rig", *(const unsigned int*)(value + 0x78));
        trace_client_state("loot_awarded_level", *(const unsigned int*)(value + 0x88));
        trace_client_state("loot_awarded_rarity", *(const unsigned int*)(value + 0x8C));
    }
    original_local_event(event_id, event);
}

static unsigned char hooked_loot_convert(void* output, const void* loot,
    int is_local, int unknown) {
    if (loot != NULL) {
        const unsigned char* value = (const unsigned char*)loot;
        trace_client_state("loot_convert_id_low", *(const unsigned int*)(value + 0x00));
        trace_client_state("loot_convert_id_high", *(const unsigned int*)(value + 0x04));
        trace_client_state("loot_convert_rig", *(const unsigned int*)(value + 0x08));
        trace_client_state("loot_convert_suffix", *(const unsigned int*)(value + 0x0C));
        trace_client_state("loot_convert_prefix1", *(const unsigned int*)(value + 0x10));
        trace_client_state("loot_convert_prefix2", *(const unsigned int*)(value + 0x14));
        trace_client_state("loot_convert_level", *(const unsigned int*)(value + 0x18));
        trace_client_state("loot_convert_rarity", *(const unsigned int*)(value + 0x1C));
    }
    unsigned char result = original_loot_convert(output, loot, is_local, unknown);
    trace_client_state("loot_awarded_convert", result);
    return result;
}

static unsigned char hooked_loot_format(void* output, const void* loot,
    int is_local, int unknown) {
    unsigned char result = original_loot_format(output, loot, is_local, unknown);
    trace_client_state("loot_awarded_format", result);
    return result;
}

static void* hooked_loot_resource_lookup(void* asset, void* output) {
    void* result = original_creature_nested_lookup(asset, output);
    trace_client_state("loot_resource_arg0", (unsigned int)(uintptr_t)asset);
    trace_client_state("loot_resource_arg1", (unsigned int)(uintptr_t)output);
    trace_client_state("loot_resource_resolved", result != NULL);
    return result;
}
static void trace_scene_state(const char* kind, unsigned int asset_id,
    unsigned int value_a, unsigned int value_b, int is_resolved);
static int trace_xp_threshold_values(const unsigned int* begin, size_t count);
static int trace_xp_thresholds(void);
static int trace_xp_thresholds_from_resource(void);

static unsigned char __cdecl hooked_ability_blink_play_available(void* player, int ability_index) {
    unsigned char is_available = original_ability_available(player, ability_index);
    trace_client_state("ability_blink_play_call",
        (unsigned int)ability_index | ((unsigned int)is_available << 16));
    return is_available;
}

static unsigned char __cdecl hooked_ability_blink_stop_available(void* player, int ability_index) {
    unsigned char is_available = original_ability_available(player, ability_index);
    trace_client_state("ability_blink_stop_call",
        (unsigned int)ability_index | ((unsigned int)is_available << 16));
    return is_available;
}

static DWORD WINAPI trace_ability_blink_later(LPVOID parameter) {
    BYTE* hud;
    unsigned int value = 0;
    (void)parameter;
    Sleep(2200);
    hud = (BYTE*)original_ability_hud();
    if (readable_pointer(hud)) {
        value = (unsigned int)hud[2 + 0x0D] | ((unsigned int)hud[2 + 0x16] << 8);
    }
    trace_client_state("ability_blink_observe", value);
    InterlockedExchange(&ability_blink_trace_pending, 0);
    return 0;
}

#if defined(__GNUC__)
static void __attribute__((fastcall)) hooked_cooldown_apply(void* object, void* ignored,
    uint64_t ability_key, int64_t duration, int64_t source_start, int64_t global_cooldown) {
#else
static void __fastcall hooked_cooldown_apply(void* object, void* ignored,
    uint64_t ability_key, int64_t duration, int64_t source_start, int64_t global_cooldown) {
#endif
    char line[768];
    DWORD written;
    int length;
    void* entry;
    uint64_t now;
    uint64_t start = 0;
    uint64_t end = 0;
    uint64_t remote_current = 0;
    uint64_t remote_origin = 0;
    uint64_t local_origin = 0;
    double clock_scale = 0.0;
    unsigned int is_clock_initialized = 0;
    unsigned int is_clock_pending = 0;
    unsigned int is_active = 0;
    void* clock_root;
    void* clock_owner;
    const BYTE* clock_state = NULL;
    (void)ignored;
    now = original_game_clock_now(original_game_clock_owner());
    original_cooldown_apply(object, ability_key, duration, source_start, global_cooldown);
    entry = original_cooldown_lookup((BYTE*)object + 0x54, &ability_key);
    if (readable_pointer(entry)) {
        start = *(const uint64_t*)entry;
        end = *(const uint64_t*)((const BYTE*)entry + 8);
        is_active = *((const BYTE*)entry + 16);
    }
    clock_root = original_clock_root();
    clock_owner = original_clock_state(clock_root);
    if (readable_pointer(clock_owner) && readable_pointer(*(void**)clock_owner)) {
        clock_state = (const BYTE*)(*(void**)clock_owner) + 0x30;
    }
    if (readable_pointer(clock_state)) {
        remote_current = *(const uint64_t*)(clock_state + 0x00);
        remote_origin = *(const uint64_t*)(clock_state + 0x08);
        local_origin = *(const uint64_t*)(clock_state + 0x10);
        clock_scale = *(const double*)(clock_state + 0x18);
        is_clock_initialized = *(const BYTE*)(clock_state + 0x20);
        is_clock_pending = *(const BYTE*)(clock_state + 0x21);
    }
    if (trace_file == INVALID_HANDLE_VALUE || !trace_lock_ready) {
        return;
    }
    length = snprintf(line, sizeof(line),
        "{\"time_ms\":%llu,\"protocol\":\"client_scene\","
        "\"kind\":\"cooldown_apply\",\"ability_key\":%llu,"
        "\"duration\":%lld,\"source_start\":%lld,\"global_cooldown\":%lld,"
        "\"clock_now\":%llu,"
        "\"start\":%llu,\"end\":%llu,\"is_active\":%u,"
        "\"remote_current\":%llu,\"remote_origin\":%llu,\"local_origin\":%llu,"
        "\"clock_scale\":%.9g,\"is_clock_initialized\":%u,\"is_clock_pending\":%u}\r\n",
        (unsigned long long)GetTickCount(), (unsigned long long)ability_key,
        (long long)duration, (long long)source_start, (long long)global_cooldown,
        (unsigned long long)now, (unsigned long long)start,
        (unsigned long long)end, is_active,
        (unsigned long long)remote_current, (unsigned long long)remote_origin,
        (unsigned long long)local_origin, clock_scale,
        is_clock_initialized, is_clock_pending);
    if (length <= 0) {
        return;
    }
    EnterCriticalSection(&trace_lock);
    WriteFile(trace_file, line, (DWORD)length, &written, NULL);
    LeaveCriticalSection(&trace_lock);
}

static void encode_hex(char* destination, const BYTE* data, unsigned int size) {
    static const char hexadecimal[] = "0123456789abcdef";
    unsigned int index;
    for (index = 0; index < size; index++) {
        BYTE value = data[index];
        destination[index * 2] = hexadecimal[value >> 4];
        destination[index * 2 + 1] = hexadecimal[value & 0x0f];
    }
}

static void snapshot_ring_append(const char* contents, unsigned int size) {
    const size_t maximum_bytes = 64u * 1024u * 1024u;
    const ULONGLONG maximum_age_ms = 300000;
    snapshot_line* line;
    ULONGLONG now;
    if (contents == NULL || size == 0 || size > maximum_bytes) {
        return;
    }
    line = (snapshot_line*)HeapAlloc(
        GetProcessHeap(), 0, sizeof(*line) - 1 + size);
    if (line == NULL) {
        return;
    }
    now = GetTickCount64();
    line->next = NULL;
    line->time_ms = now;
    line->size = size;
    memcpy(line->contents, contents, size);
    AcquireSRWLockExclusive(&snapshot_ring_lock);
    if (snapshot_ring_tail == NULL) {
        snapshot_ring_head = line;
    } else {
        snapshot_ring_tail->next = line;
    }
    snapshot_ring_tail = line;
    snapshot_ring_byte_count += size;
    while (snapshot_ring_head != NULL &&
        (snapshot_ring_byte_count > maximum_bytes ||
         now - snapshot_ring_head->time_ms > maximum_age_ms)) {
        snapshot_line* removed = snapshot_ring_head;
        int is_capacity_drop = snapshot_ring_byte_count > maximum_bytes;
        snapshot_ring_head = removed->next;
        snapshot_ring_byte_count -= removed->size;
        if (is_capacity_drop) {
            snapshot_ring_capacity_dropped_line_count++;
            snapshot_ring_capacity_dropped_byte_count += removed->size;
            snapshot_ring_capacity_last_dropped_time_ms = removed->time_ms;
        }
        HeapFree(GetProcessHeap(), 0, removed);
    }
    if (snapshot_ring_head == NULL) {
        snapshot_ring_tail = NULL;
    }
    ReleaseSRWLockExclusive(&snapshot_ring_lock);
}

static void snapshot_ring_clear(void) {
    snapshot_line* line;
    AcquireSRWLockExclusive(&snapshot_ring_lock);
    line = snapshot_ring_head;
    snapshot_ring_head = NULL;
    snapshot_ring_tail = NULL;
    snapshot_ring_byte_count = 0;
    snapshot_ring_capacity_dropped_line_count = 0;
    snapshot_ring_capacity_dropped_byte_count = 0;
    snapshot_ring_capacity_last_dropped_time_ms = 0;
    while (line != NULL) {
        snapshot_line* next = line->next;
        HeapFree(GetProcessHeap(), 0, line);
        line = next;
    }
    ReleaseSRWLockExclusive(&snapshot_ring_lock);
}

static void snapshot_ring_marker(const char* kind, const char* request,
    unsigned long long server_time_unix_nano, unsigned int buffer_ms) {
    char line[768];
    int length;
    ULONGLONG oldest_time_ms = 0;
    ULONGLONG newest_time_ms = 0;
    ULONGLONG dropped_line_count;
    ULONGLONG dropped_byte_count;
    ULONGLONG last_dropped_time_ms;
    size_t byte_count;
    unsigned int line_count = 0;
    snapshot_line* ring_line;
    if (kind == NULL || request == NULL) {
        return;
    }
    AcquireSRWLockShared(&snapshot_ring_lock);
    byte_count = snapshot_ring_byte_count;
    dropped_line_count = snapshot_ring_capacity_dropped_line_count;
    dropped_byte_count = snapshot_ring_capacity_dropped_byte_count;
    last_dropped_time_ms = snapshot_ring_capacity_last_dropped_time_ms;
    if (snapshot_ring_head != NULL) {
        oldest_time_ms = snapshot_ring_head->time_ms;
    }
    if (snapshot_ring_tail != NULL) {
        newest_time_ms = snapshot_ring_tail->time_ms;
    }
    for (ring_line = snapshot_ring_head; ring_line != NULL;
        ring_line = ring_line->next) {
        line_count++;
    }
    ReleaseSRWLockShared(&snapshot_ring_lock);
    length = snprintf(line, sizeof(line),
        "{\"time_ms\":%llu,\"protocol\":\"client_state\","
        "\"kind\":\"%s\",\"thread\":%lu,\"frame_sequence\":%lu,"
        "\"frame_delta_bits\":%lu,\"frame_time_ms\":%lu,\"request\":\"%s\","
        "\"server_time_unix_nano\":%llu,\"ring_line_count\":%u,"
        "\"ring_byte_count\":%llu,\"capacity_dropped_line_count\":%llu,"
        "\"capacity_dropped_byte_count\":%llu,\"oldest_time_ms\":%llu,"
        "\"newest_time_ms\":%llu,\"capacity_last_dropped_time_ms\":%llu,"
        "\"buffer_ms\":%u}\r\n",
        (unsigned long long)GetTickCount64(), kind,
        (unsigned long)GetCurrentThreadId(),
        (unsigned long)InterlockedCompareExchange(&snapshot_frame_sequence, 0, 0),
        (unsigned long)InterlockedCompareExchange(&snapshot_frame_delta_bits, 0, 0),
        (unsigned long)InterlockedCompareExchange(&snapshot_frame_time_ms, 0, 0),
        request, server_time_unix_nano, line_count,
        (unsigned long long)byte_count,
        (unsigned long long)dropped_line_count,
        (unsigned long long)dropped_byte_count,
        (unsigned long long)oldest_time_ms,
        (unsigned long long)newest_time_ms,
        (unsigned long long)last_dropped_time_ms, buffer_ms);
    if (length <= 0 || (size_t)length >= sizeof(line)) {
        return;
    }
    snapshot_ring_append(line, (unsigned int)length);
}

static int valid_snapshot_directory_name(const char* name) {
    size_t index;
    size_t length;
    if (name == NULL) {
        return 0;
    }
    length = strlen(name);
    if (length == 0 || length >= 240) {
        return 0;
    }
    for (index = 0; index < length; index++) {
        unsigned char character = (unsigned char)name[index];
        if (!isalnum(character) && character != '-' && character != '_' &&
            character != '.') {
            return 0;
        }
    }
    return 1;
}

static void snapshot_ring_dump(
    const char* directory_name, unsigned int buffer_ms) {
    char output_path[MAX_PATH * 4];
    char temporary_path[MAX_PATH * 4];
    char* separator;
    snapshot_line* line;
    char* contents = NULL;
    size_t total = 0;
    size_t offset = 0;
    ULONGLONG now = GetTickCount64();
    ULONGLONG cutoff = now > buffer_ms ? now - buffer_ms : 0;
    HANDLE output;
    int is_complete = 0;
    int output_length;
    int temporary_length;
    size_t output_remaining;
    if (!valid_snapshot_directory_name(directory_name) ||
        snapshot_control_path[0] == '\0') {
        return;
    }
    strncpy(output_path, snapshot_control_path, sizeof(output_path) - 1);
    output_path[sizeof(output_path) - 1] = '\0';
    separator = strrchr(output_path, '\\');
    if (separator == NULL) {
        separator = strrchr(output_path, '/');
    }
    if (separator == NULL) {
        return;
    }
    separator[1] = '\0';
    output_remaining = sizeof(output_path) -
        (size_t)(separator + 1 - output_path);
    output_length = snprintf(separator + 1, output_remaining,
        "snapshots\\%s\\client-memory.jsonl", directory_name);
    if (output_length <= 0 || (size_t)output_length >= output_remaining) {
        return;
    }
    temporary_length = snprintf(
        temporary_path, sizeof(temporary_path), "%s.tmp", output_path);
    if (temporary_length <= 0 || (size_t)temporary_length >= sizeof(temporary_path)) {
        return;
    }
    AcquireSRWLockShared(&snapshot_ring_lock);
    for (line = snapshot_ring_head; line != NULL; line = line->next) {
        if (line->time_ms >= cutoff) {
            total += line->size;
        }
    }
    if (total != 0) {
        contents = (char*)HeapAlloc(GetProcessHeap(), 0, total);
    }
    if (total != 0 && contents != NULL) {
        for (line = snapshot_ring_head; line != NULL; line = line->next) {
            if (line->time_ms < cutoff) {
                continue;
            }
            memcpy(contents + offset, line->contents, line->size);
            offset += line->size;
        }
    }
    ReleaseSRWLockShared(&snapshot_ring_lock);
    if (total != 0 && contents == NULL) {
        return;
    }
    output = CreateFileA(
        temporary_path, GENERIC_WRITE, FILE_SHARE_READ,
        NULL, CREATE_ALWAYS, FILE_ATTRIBUTE_NORMAL, NULL);
    if (output != INVALID_HANDLE_VALUE) {
        size_t written_total = 0;
        while (written_total < total) {
            DWORD requested = (DWORD)(total - written_total);
            DWORD written = 0;
            if (!WriteFile(output, contents + written_total, requested, &written, NULL) ||
                written == 0) {
                break;
            }
            written_total += written;
        }
        if (written_total == total && FlushFileBuffers(output)) {
            is_complete = 1;
        }
        CloseHandle(output);
    }
    if (is_complete) {
        if (!MoveFileExA(temporary_path, output_path,
            MOVEFILE_REPLACE_EXISTING | MOVEFILE_WRITE_THROUGH)) {
            DeleteFileA(temporary_path);
        }
    } else {
        DeleteFileA(temporary_path);
    }
    if (contents != NULL) {
        HeapFree(GetProcessHeap(), 0, contents);
    }
}

static void trace_hex_line(
    const char* header, int header_length,
    const BYTE* data, unsigned int size, const char* suffix) {
    size_t suffix_length = strlen(suffix);
    size_t total;
    char* line;
    DWORD written;
    if (header == NULL || header_length <= 0 || data == NULL || suffix == NULL ||
        size > (SIZE_MAX - (size_t)header_length - suffix_length) / 2) {
        return;
    }
    total = (size_t)header_length + (size_t)size * 2 + suffix_length;
    if (total > UINT_MAX) {
        return;
    }
    line = (char*)HeapAlloc(GetProcessHeap(), 0, total);
    if (line == NULL) {
        return;
    }
    memcpy(line, header, (size_t)header_length);
    encode_hex(line + header_length, data, size);
    memcpy(line + header_length + size * 2, suffix, suffix_length);
    if (trace_file != INVALID_HANDLE_VALUE && trace_lock_ready) {
        EnterCriticalSection(&trace_lock);
        WriteFile(trace_file, line, (DWORD)total, &written, NULL);
        LeaveCriticalSection(&trace_lock);
    }
    snapshot_ring_append(line, (unsigned int)total);
    HeapFree(GetProcessHeap(), 0, line);
}

static void trace_client_message_payload(unsigned char id, const void* data, unsigned int size) {
    char header[320];
    const char suffix[] = "\"}\r\n";
    BYTE* payload;
    int length;
    if (InterlockedCompareExchange(&snapshot_capture_enabled, 0, 0) == 0 ||
        data == NULL || size == 0 || size == UINT_MAX) {
        return;
    }
    payload = (BYTE*)HeapAlloc(GetProcessHeap(), 0, (SIZE_T)size + 1);
    if (payload == NULL) {
        return;
    }
    payload[0] = id;
    memcpy(payload + 1, data, size);
    length = snprintf(header, sizeof(header),
        "{\"time_ms\":%llu,\"protocol\":\"client_raknet\","
        "\"kind\":\"application_receive\",\"thread\":%lu,"
        "\"frame_sequence\":%lu,\"frame_delta_bits\":%lu,\"frame_time_ms\":%lu,"
        "\"message_id\":%u,\"message_id_hex\":\"0x%02X\","
        "\"size\":%u,\"payload_hex\":\"",
        (unsigned long long)GetTickCount(), (unsigned long)GetCurrentThreadId(),
        (unsigned long)InterlockedCompareExchange(&snapshot_frame_sequence, 0, 0),
        (unsigned long)InterlockedCompareExchange(&snapshot_frame_delta_bits, 0, 0),
        (unsigned long)InterlockedCompareExchange(&snapshot_frame_time_ms, 0, 0),
        (unsigned int)id, (unsigned int)id, size + 1);
    if (length <= 0) {
        HeapFree(GetProcessHeap(), 0, payload);
        return;
    }
    trace_hex_line(
        header, length, payload, size + 1, suffix);
    HeapFree(GetProcessHeap(), 0, payload);
}

static void trace_message_receive(unsigned char id, const void* data, unsigned int size) {
    char line[384];
    const unsigned char* byte = (const unsigned char*)data;
    DWORD written;
    int length;
    unsigned int prefix_size = data != NULL ? (size < 16 ? size : 16) : 0;
    char prefix[33];
    unsigned int index;
    unsigned int objective_id = 0;
    if (id == 0xB8 && data != NULL && size >= 11) {
        memcpy(&objective_id, data, sizeof(objective_id));
        if (objective_id == 0xAC4273F3 && byte[10] != 0 &&
            InterlockedCompareExchange(&ability_blink_trace_pending, 1, 0) == 0) {
            HANDLE blink_thread = CreateThread(NULL, 0, trace_ability_blink_later, NULL, 0, NULL);
            if (blink_thread != NULL) {
                CloseHandle(blink_thread);
            } else {
                InterlockedExchange(&ability_blink_trace_pending, 0);
            }
        }
    }
    trace_client_message_payload(id, data, size);
    if (trace_file == INVALID_HANDLE_VALUE || !trace_lock_ready) {
        return;
    }
    for (index = 0; index < prefix_size; index++) {
        snprintf(prefix + index * 2, sizeof(prefix) - index * 2, "%02x", byte[index]);
    }
    prefix[prefix_size * 2] = '\0';
    length = snprintf(line, sizeof(line),
        "{\"time_ms\":%llu,\"protocol\":\"client_scene\","
        "\"kind\":\"message_receive\",\"message_id\":%u,\"message_id_hex\":\"0x%02X\","
        "\"size\":%u,\"prefix\":\"%s\"}\r\n",
        (unsigned long long)GetTickCount(), (unsigned int)id, (unsigned int)id, size, prefix);
    if (length <= 0) {
        return;
    }
    EnterCriticalSection(&trace_lock);
    WriteFile(trace_file, line, (DWORD)length, &written, NULL);
    LeaveCriticalSection(&trace_lock);
}

static void hooked_clock_initialize(uint64_t source_time) {
    char line[512];
    DWORD written;
    int length;
    uint64_t remote_current = 0;
    uint64_t remote_origin = 0;
    uint64_t local_origin = 0;
    double clock_scale = 0.0;
    unsigned int is_clock_initialized = 0;
    unsigned int is_clock_pending = 0;
    void* clock_root;
    void* clock_owner;
    const BYTE* clock_state = NULL;
    original_clock_initialize(source_time);
    clock_root = original_clock_root();
    clock_owner = original_clock_state(clock_root);
    if (readable_pointer(clock_owner) && readable_pointer(*(void**)clock_owner)) {
        clock_state = (const BYTE*)(*(void**)clock_owner) + 0x30;
    }
    if (readable_pointer(clock_state)) {
        remote_current = *(const uint64_t*)(clock_state + 0x00);
        remote_origin = *(const uint64_t*)(clock_state + 0x08);
        local_origin = *(const uint64_t*)(clock_state + 0x10);
        clock_scale = *(const double*)(clock_state + 0x18);
        is_clock_initialized = *(const BYTE*)(clock_state + 0x20);
        is_clock_pending = *(const BYTE*)(clock_state + 0x21);
    }
    if (trace_file == INVALID_HANDLE_VALUE || !trace_lock_ready) {
        return;
    }
    length = snprintf(line, sizeof(line),
        "{\"time_ms\":%llu,\"protocol\":\"client_scene\"," 
        "\"kind\":\"clock_initialize\",\"source_time\":%llu,"
        "\"remote_current\":%llu,\"remote_origin\":%llu,\"local_origin\":%llu,"
        "\"clock_scale\":%.9g,\"is_clock_initialized\":%u,\"is_clock_pending\":%u}\r\n",
        (unsigned long long)GetTickCount(), (unsigned long long)source_time,
        (unsigned long long)remote_current, (unsigned long long)remote_origin,
        (unsigned long long)local_origin, clock_scale,
        is_clock_initialized, is_clock_pending);
    if (length <= 0) {
        return;
    }
    EnterCriticalSection(&trace_lock);
    WriteFile(trace_file, line, (DWORD)length, &written, NULL);
    LeaveCriticalSection(&trace_lock);
}

static void trace_player_reflection(void) {
    const unsigned int player_type_id = 0x15FF16E2;
    reflection_lookup_fn lookup;
    const unsigned char* metadata;
    const unsigned char* field;
    unsigned int field_count;
    unsigned int index;
    char line[384];
    DWORD written;
    int length;
    if (trace_file == INVALID_HANDLE_VALUE || !trace_lock_ready || executable_module == NULL) {
        return;
    }
    if (InterlockedCompareExchange(&player_reflection_traced, 1, 0) != 0) {
        return;
    }
    lookup = (reflection_lookup_fn)((BYTE*)executable_module + 0x5EF610);
    metadata = (const unsigned char*)lookup(player_type_id);
    if (!readable_pointer(metadata)) {
        trace_client_state("player_reflection_missing", player_type_id);
        return;
    }
    field = *(const unsigned char* const*)(metadata + 0x08);
    field_count = *(const unsigned int*)(metadata + 0x0C);
    if (!readable_pointer(field) || field_count > 256) {
        trace_client_state("player_reflection_invalid", field_count);
        return;
    }
    for (index = 0; index < field_count; index++, field += 0x60) {
        length = snprintf(line, sizeof(line),
            "{\"time_ms\":%llu,\"protocol\":\"client_reflection\","
            "\"type_id\":\"0x%08X\",\"field\":%u,\"wire_index\":%u,"
            "\"offset\":%u,\"element_size\":%u,\"element_count\":%u,"
            "\"value_type\":\"0x%08X\",\"name_hash\":\"0x%08X\",\"mode\":%u}\r\n",
            (unsigned long long)GetTickCount(), player_type_id, index,
            *(const unsigned int*)(field + 0x50), *(const unsigned int*)(field + 0x14),
            *(const unsigned int*)(field + 0x08), *(const unsigned int*)(field + 0x18),
            *(const unsigned int*)(field + 0x04), *(const unsigned int*)(field + 0x10),
            *(const unsigned int*)(field + 0x40));
        if (length <= 0) {
            continue;
        }
        EnterCriticalSection(&trace_lock);
        WriteFile(trace_file, line, (DWORD)length, &written, NULL);
        LeaveCriticalSection(&trace_lock);
    }
}

static void trace_combat_text_assets(void) {
    const unsigned int asset_rva[] = {
        COMBAT_TEXT_DAMAGE_RVA,
        COMBAT_TEXT_ENEMY_DAMAGE_RVA,
        COMBAT_TEXT_DAMAGE_CRITICAL_RVA,
        COMBAT_TEXT_ENEMY_DAMAGE_CRITICAL_RVA,
        COMBAT_TEXT_ABSORB_RVA,
        COMBAT_TEXT_ENEMY_ABSORB_RVA,
        COMBAT_TEXT_ABSORB_CRITICAL_RVA,
        COMBAT_TEXT_ENEMY_ABSORB_CRITICAL_RVA,
        COMBAT_TEXT_HEAL_RVA,
        COMBAT_TEXT_HEAL_CRITICAL_RVA,
        COMBAT_TEXT_HEAL_TARGET_RVA,
    };
    const char* asset_kind[] = {
        "combat_text_damage_asset",
        "combat_text_enemy_damage_asset",
        "combat_text_damage_critical_asset",
        "combat_text_enemy_damage_critical_asset",
        "combat_text_absorb_asset",
        "combat_text_enemy_absorb_asset",
        "combat_text_absorb_critical_asset",
        "combat_text_enemy_absorb_critical_asset",
        "combat_text_heal_asset",
        "combat_text_heal_critical_asset",
        "combat_text_heal_target_asset",
    };
    unsigned int index;
    if (executable_module == NULL) {
        trace_client_state("combat_text_module_missing", 1);
        return;
    }
    for (index = 0; index < sizeof(asset_rva) / sizeof(asset_rva[0]); index++) {
        const BYTE* asset = (const BYTE*)executable_module + asset_rva[index];
        if (!readable_range(asset, sizeof(unsigned int))) {
            trace_client_state(asset_kind[index], 0);
            continue;
        }
        trace_client_state(asset_kind[index], *(const unsigned int*)asset);
    }
}

static void trace_combat_text_preferences(void) {
    const unsigned int preference_rva[] = {
        SHOW_DAMAGE_DONE_BY_ME_RVA,
        SHOW_DAMAGE_DONE_BY_ALLIES_RVA,
        SHOW_DAMAGE_DONE_TO_ME_RVA,
        SHOW_DAMAGE_DONE_TO_ALLIES_RVA,
    };
    const char* preference_kind[] = {
        "combat_text_damage_done_by_me",
        "combat_text_damage_done_by_allies",
        "combat_text_damage_done_to_me",
        "combat_text_damage_done_to_allies",
    };
    void* manager;
    void** virtual_table;
    setting_read_fn read_setting;
    unsigned int index;
    if (executable_module == NULL || original_settings_manager == NULL) {
        trace_client_state("combat_text_settings_missing", 1);
        return;
    }
    manager = original_settings_manager();
    if (!readable_pointer(manager)) {
        trace_client_state("combat_text_settings_manager_missing", 1);
        return;
    }
    virtual_table = *(void***)manager;
    if (!readable_range(virtual_table, 56)) {
        trace_client_state("combat_text_settings_table_missing", 1);
        return;
    }
    read_setting = (setting_read_fn)virtual_table[13];
    if (read_setting == NULL) {
        trace_client_state("combat_text_settings_reader_missing", 1);
        return;
    }
    for (index = 0; index < sizeof(preference_rva) / sizeof(preference_rva[0]); index++) {
        const BYTE* key_address = (const BYTE*)executable_module + preference_rva[index];
        if (!readable_range(key_address, sizeof(unsigned int))) {
            trace_client_state(preference_kind[index], 0);
            continue;
        }
        trace_client_state(
            preference_kind[index],
            (unsigned int)read_setting(manager, *(const unsigned int*)key_address));
    }
}

static movement_trace_context* get_movement_trace_context(void) {
    movement_trace_context* context;
    if (movement_trace_tls_index == TLS_OUT_OF_INDEXES) {
        return NULL;
    }
    context = (movement_trace_context*)TlsGetValue(movement_trace_tls_index);
    if (context != NULL) {
        return context;
    }
    context = (movement_trace_context*)HeapAlloc(
        GetProcessHeap(), HEAP_ZERO_MEMORY, sizeof(*context));
    if (context == NULL) {
        return NULL;
    }
    if (!TlsSetValue(movement_trace_tls_index, context)) {
        HeapFree(GetProcessHeap(), 0, context);
        return NULL;
    }
    return context;
}

static void trace_locomotion_snapshot(const char* phase,
    unsigned int object_id, const BYTE* object, const BYTE* locomotion) {
    char header[1536];
    const char suffix[] = "\"}\r\n";
    int length;
    if (InterlockedCompareExchange(&snapshot_capture_enabled, 0, 0) == 0 ||
        !readable_range(object, 668) || !readable_range(locomotion, 440)) {
        return;
    }
    length = snprintf(header, sizeof(header),
        "{\"time_ms\":%llu,\"protocol\":\"client_state\","
        "\"kind\":\"locomotion_snapshot\",\"phase\":\"%s\",\"thread\":%lu,"
        "\"frame_sequence\":%lu,\"frame_delta_bits\":%lu,\"frame_time_ms\":%lu,"
        "\"object_id\":%u,\"position_bits\":[%u,%u,%u],"
        "\"flags\":%u,\"goal_bits\":[%u,%u,%u],"
        "\"partial_goal_bits\":[%u,%u,%u],\"target_object_id\":%u,"
        "\"desired_stop_bits\":%u,\"component_size\":440,"
        "\"component_hex\":\"",
        (unsigned long long)GetTickCount(), phase,
        (unsigned long)GetCurrentThreadId(),
        (unsigned long)InterlockedCompareExchange(&snapshot_frame_sequence, 0, 0),
        (unsigned long)InterlockedCompareExchange(&snapshot_frame_delta_bits, 0, 0),
        (unsigned long)InterlockedCompareExchange(&snapshot_frame_time_ms, 0, 0),
        object_id,
        *(const unsigned int*)(object + 24),
        *(const unsigned int*)(object + 28),
        *(const unsigned int*)(object + 32),
        *(const unsigned int*)(locomotion + 324),
        *(const unsigned int*)(locomotion + 328),
        *(const unsigned int*)(locomotion + 332),
        *(const unsigned int*)(locomotion + 336),
        *(const unsigned int*)(locomotion + 340),
        *(const unsigned int*)(locomotion + 344),
        *(const unsigned int*)(locomotion + 348),
        *(const unsigned int*)(locomotion + 144),
        *(const unsigned int*)(locomotion + 416));
    if (length <= 0) {
        return;
    }
    trace_hex_line(header, length, locomotion, 440, suffix);
}

static void trace_client_object_memory(
    unsigned int object_id, const BYTE* object) {
    char header[512];
    const char suffix[] = "\"}\r\n";
    int length;
    if (!readable_range(object, 668)) {
        return;
    }
    length = snprintf(header, sizeof(header),
        "{\"time_ms\":%llu,\"protocol\":\"client_state\","
        "\"kind\":\"object_memory\",\"phase\":\"snapshot_boundary\","
        "\"thread\":%lu,\"frame_sequence\":%lu,"
        "\"frame_delta_bits\":%lu,\"frame_time_ms\":%lu,\"object_id\":%u,"
        "\"position_bits\":[%u,%u,%u],\"object_size\":668,"
        "\"object_hex\":\"",
        (unsigned long long)GetTickCount64(),
        (unsigned long)GetCurrentThreadId(),
        (unsigned long)InterlockedCompareExchange(&snapshot_frame_sequence, 0, 0),
        (unsigned long)InterlockedCompareExchange(&snapshot_frame_delta_bits, 0, 0),
        (unsigned long)InterlockedCompareExchange(&snapshot_frame_time_ms, 0, 0),
        object_id, *(const unsigned int*)(object + 24),
        *(const unsigned int*)(object + 28),
        *(const unsigned int*)(object + 32));
    if (length <= 0) {
        return;
    }
    trace_hex_line(header, length, object, 668, suffix);
}

static void trace_client_object_probe(
    unsigned int object_id, const BYTE* object) {
    char line[512];
    DWORD written;
    int length;
    if (!readable_range(object, 668)) {
        return;
    }
    length = snprintf(line, sizeof(line),
        "{\"time_ms\":%llu,\"protocol\":\"client_state\","
        "\"kind\":\"object_probe\",\"phase\":\"automatic\","
        "\"thread\":%lu,\"frame_sequence\":%lu,"
        "\"frame_delta_bits\":%lu,\"frame_time_ms\":%lu,\"object_id\":%u,"
        "\"position_bits\":[%u,%u,%u]}\r\n",
        (unsigned long long)GetTickCount64(),
        (unsigned long)GetCurrentThreadId(),
        (unsigned long)InterlockedCompareExchange(&snapshot_frame_sequence, 0, 0),
        (unsigned long)InterlockedCompareExchange(&snapshot_frame_delta_bits, 0, 0),
        (unsigned long)InterlockedCompareExchange(&snapshot_frame_time_ms, 0, 0),
        object_id, *(const unsigned int*)(object + 24),
        *(const unsigned int*)(object + 28),
        *(const unsigned int*)(object + 32));
    if (length <= 0 || (size_t)length >= sizeof(line)) {
        return;
    }
    if (trace_file != INVALID_HANDLE_VALUE && trace_lock_ready) {
        EnterCriticalSection(&trace_lock);
        WriteFile(trace_file, line, (DWORD)length, &written, NULL);
        LeaveCriticalSection(&trace_lock);
    }
    snapshot_ring_append(line, (unsigned int)length);
}

static void trace_client_object_probe_registry(void) {
    const BYTE* registry;
    const BYTE* table;
    const BYTE* slots;
    unsigned int capacity;
    unsigned int stride;
    unsigned int index;
    if (InterlockedCompareExchange(&snapshot_auto_probe_enabled, 0, 0) == 0 ||
        original_game_object_registry == NULL) {
        return;
    }
    registry = (const BYTE*)original_game_object_registry();
    if (!readable_range(registry, 8)) {
        return;
    }
    table = *(const BYTE* const*)(registry + 4);
    if (!readable_range(table, 24)) {
        return;
    }
    slots = *(const BYTE* const*)table;
    capacity = *(const unsigned int*)(table + 16);
    stride = *(const unsigned int*)(table + 20);
    if (slots == NULL || capacity == 0 || capacity > 65536 ||
        stride < 668 || stride > 16384 ||
        (size_t)(capacity - 1) >
            (SIZE_MAX - (size_t)(uintptr_t)slots) / stride) {
        return;
    }
    for (index = 0; index < capacity; index++) {
        const BYTE* object = slots + (size_t)index * stride;
        const BYTE* locomotion;
        unsigned int object_id;
        if (!readable_range(object, 668)) {
            continue;
        }
        object_id = *(const unsigned int*)object;
        if (object_id == 0 || (object_id & 0xffffu) != index ||
            (object_id >> 16) == 0) {
            continue;
        }
        locomotion = *(const BYTE* const*)(object + 664);
        if (!readable_range(locomotion, 440)) {
            continue;
        }
        trace_client_object_probe(object_id, object);
    }
}

static void trace_client_object_registry(void) {
    const BYTE* registry;
    const BYTE* table;
    const BYTE* slots;
    unsigned int capacity;
    unsigned int stride;
    unsigned int index;
    unsigned int captured = 0;
    if (InterlockedCompareExchange(&snapshot_capture_enabled, 0, 0) == 0 ||
        original_game_object_registry == NULL) {
        return;
    }
    registry = (const BYTE*)original_game_object_registry();
    if (!readable_range(registry, 8)) {
        return;
    }
    table = *(const BYTE* const*)(registry + 4);
    if (!readable_range(table, 24)) {
        return;
    }
    slots = *(const BYTE* const*)table;
    capacity = *(const unsigned int*)(table + 16);
    stride = *(const unsigned int*)(table + 20);
    if (slots == NULL || capacity == 0 || capacity > 65536 ||
        stride < 668 || stride > 16384 ||
        (size_t)(capacity - 1) >
            (SIZE_MAX - (size_t)(uintptr_t)slots) / stride) {
        return;
    }
    for (index = 0; index < capacity; index++) {
        const BYTE* object = slots + (size_t)index * stride;
        const BYTE* locomotion;
        unsigned int object_id;
        if (!readable_range(object, 668)) {
            continue;
        }
        object_id = *(const unsigned int*)object;
        if (object_id == 0 || (object_id & 0xffffu) != index ||
            (object_id >> 16) == 0) {
            continue;
        }
        trace_client_object_memory(object_id, object);
        locomotion = *(const BYTE* const*)(object + 664);
        if (readable_range(locomotion, 440)) {
            trace_locomotion_snapshot(
                "snapshot_boundary", object_id, object, locomotion);
        }
        captured++;
    }
    trace_client_state("snapshot_object_count", captured);
}

static void reset_movement_trace_context(void) {
    movement_trace_context* context = get_movement_trace_context();
    if (context != NULL) {
        ZeroMemory(context, sizeof(*context));
    }
}

static void trace_resolved_movement_context(const char* phase) {
    movement_trace_context* context = get_movement_trace_context();
    if (context == NULL || context->object_id == 0) {
        return;
    }
    trace_locomotion_snapshot(
        phase, context->object_id, context->object, context->locomotion);
}

static void* __cdecl hooked_movement_object_resolve(unsigned int object_id) {
    void* result = original_game_object_resolve(object_id);
    const BYTE* object;
    const BYTE* locomotion;
    movement_trace_context* context;
    trace_client_state("locomotion_resolve_object", object_id);
    if (!readable_range(result, 668)) {
        trace_client_state("locomotion_resolve_missing", object_id);
        return result;
    }
    object = (const BYTE*)result;
    locomotion = *(const BYTE* const*)(object + 664);
    if (!readable_range(locomotion, 440)) {
        trace_client_state("locomotion_resolve_component_missing", object_id);
        return result;
    }
    context = get_movement_trace_context();
    if (context != NULL) {
        context->object_id = object_id;
        context->object = object;
        context->locomotion = locomotion;
    }
    trace_locomotion_snapshot("before_apply", object_id, object, locomotion);
    trace_client_state("locomotion_resolve_component", object_id);
    trace_client_state(
        "locomotion_object_x_bits",
        *(const unsigned int*)(object + 24));
    trace_client_state(
        "locomotion_object_y_bits",
        *(const unsigned int*)(object + 28));
    trace_client_state(
        "locomotion_object_z_bits",
        *(const unsigned int*)(object + 32));
    trace_client_state("locomotion_pre_flags", *(const unsigned int*)(locomotion + 324));
    trace_client_state(
        "locomotion_goal_x_bits",
        *(const unsigned int*)(locomotion + 328));
    trace_client_state(
        "locomotion_goal_y_bits",
        *(const unsigned int*)(locomotion + 332));
    trace_client_state(
        "locomotion_goal_z_bits",
        *(const unsigned int*)(locomotion + 336));
    trace_client_state(
        "locomotion_partial_x_bits",
        *(const unsigned int*)(locomotion + 340));
    trace_client_state(
        "locomotion_partial_y_bits",
        *(const unsigned int*)(locomotion + 344));
    trace_client_state(
        "locomotion_partial_z_bits",
        *(const unsigned int*)(locomotion + 348));
    trace_client_state(
        "locomotion_pre_target_object",
        *(const unsigned int*)(locomotion + 144));
    trace_client_state(
        "locomotion_pre_desired_stop_bits",
        *(const unsigned int*)(locomotion + 416));
    return result;
}

static void* __stdcall hooked_player_move_receiver(void* message) {
    void* result;
    reset_movement_trace_context();
    result = original_player_move_receiver(message);
    trace_resolved_movement_context("after_apply");
    reset_movement_trace_context();
    return result;
}

static void* __stdcall hooked_locomotion_update_receiver(void* message) {
    void* result;
    reset_movement_trace_context();
    result = original_locomotion_update_receiver(message);
    trace_resolved_movement_context("after_apply");
    reset_movement_trace_context();
    return result;
}

static void* __stdcall hooked_locomotion_unreliable_receiver(void* message) {
    void* result;
    reset_movement_trace_context();
    result = original_locomotion_unreliable_receiver(message);
    trace_resolved_movement_context("after_apply");
    reset_movement_trace_context();
    return result;
}

#if defined(__GNUC__)
static void* __attribute__((fastcall)) hooked_message_construct(
    void* message, void* ignored, unsigned char id, const void* data, unsigned int size) {
#else
static void* __fastcall hooked_message_construct(
    void* message, void* ignored, unsigned char id, const void* data, unsigned int size) {
#endif
    void* constructed_message;
    (void)ignored;
    if (InterlockedExchange(&action_response_trace_pending, 0) != 0 &&
        original_combat_input_state != NULL) {
        const BYTE* state = (const BYTE*)original_combat_input_state();
        if (readable_pointer(state) && readable_pointer(state + 60)) {
            trace_client_state("action_active_definition", *(const unsigned int*)(state + 4));
            trace_client_state("action_active_sync", *(const unsigned char*)(state + 8));
            trace_client_state("action_active_deadline", *(const unsigned int*)(state + 16));
            trace_client_state("action_response_definition", *(const unsigned int*)(state + 24));
            trace_client_state("action_response_sync", *(const unsigned char*)(state + 28));
            trace_client_state("action_response_end", *(const unsigned int*)(state + 40));
            trace_client_state("action_queued_definition", *(const unsigned int*)(state + 52));
            trace_client_state("action_queued_sync", *(const unsigned char*)(state + 56));
            trace_client_state("action_retained_target", *(const unsigned int*)(state + 60));
            trace_input_action_state("input_action_response_after");
        } else {
            trace_client_state("action_state_missing", 1);
        }
    }
    if (id >= 0x80) {
        trace_message_receive(id, data, size);
    }
    if (id == 0xA1) {
        if (data != NULL && size == 9 &&
            *((const unsigned char*)data + 1) == 0x00 &&
            *((const unsigned char*)data + 2) == 0x10 &&
            *((const unsigned char*)data + 3) == 0x09 &&
            *((const unsigned char*)data + 8) == 0xFF) {
            unsigned int controlled_object_id;
            memcpy(&controlled_object_id, (const BYTE*)data + 4, sizeof(controlled_object_id));
            InterlockedExchange(
                &diagnostic_controlled_object_id,
                (LONG)controlled_object_id);
            trace_client_state("combat_controlled_binding", controlled_object_id);
        }
        trace_player_reflection();
    }
    if (id == 0xBA && original_controlled_player != NULL) {
        trace_combat_text_assets();
        trace_combat_text_preferences();
        const BYTE* player = (const BYTE*)original_controlled_player();
        if (readable_pointer(player) && readable_pointer(player + 4664)) {
            unsigned int controlled_handle = *(const unsigned int*)(player + 4664);
            unsigned int controlled_object = (unsigned int)InterlockedCompareExchange(
                &diagnostic_controlled_object_id, 0, 0);
            trace_client_state("combat_controlled_handle", controlled_handle);
            trace_client_state("combat_controlled_object", controlled_object);
            trace_client_state("combat_player_index", *(const unsigned char*)(player + 4660));
            if (data != NULL && size >= 19 && *(const unsigned char*)data == 0x9B) {
                unsigned int target_object;
                unsigned int source_object;
                unsigned int integer_hp_change;
                unsigned int relation = 0;
                memcpy(&target_object, (const BYTE*)data + 7, sizeof(target_object));
                memcpy(&source_object, (const BYTE*)data + 11, sizeof(source_object));
                memcpy(&integer_hp_change, (const BYTE*)data + 15, sizeof(integer_hp_change));
                if (controlled_object == source_object) {
                    relation |= 1;
                }
                if (controlled_object == target_object) {
                    relation |= 2;
                }
                trace_client_state("combat_target_object", target_object);
                trace_client_state("combat_source_object", source_object);
                trace_client_state("combat_integer_hp_change", integer_hp_change);
                trace_client_state("combat_control_relation", relation);
            }
        } else {
            trace_client_state("combat_controlled_missing", 1);
        }
    }
    constructed_message = original_message_construct(message, id, data, size);
    if (id == 0xA8 && data != NULL && size >= 2) {
        trace_client_state("action_response_sync_wire", *(const unsigned char*)data);
        trace_client_state("action_response_type_wire", *((const unsigned char*)data + 1));
        InterlockedExchange(&action_response_trace_pending, 1);
    }
    return constructed_message;
}

static void trace_game_prepare_decode(const unsigned int* value, unsigned int requested_size,
    unsigned int decoded_size, unsigned int stream_size, unsigned int context_offset);
static void trace_message_receive(unsigned char id, const void* data, unsigned int size);

typedef struct navigation_event {
    unsigned char reserved[8];
    double room;
} navigation_event;

#if defined(__GNUC__)
static void __attribute__((fastcall)) hooked_launcher_ready(void* launcher, void* unused) {
#else
static void __fastcall hooked_launcher_ready(void* launcher, void* unused) {
#endif
    HWND window;
    BOOL is_posted;
    (void)unused;
    original_launcher_ready(launcher);
    if (launcher == NULL) {
        return;
    }
    if (InterlockedCompareExchange(&multiplayer_auto_started, 1, 0) != 0) {
        return;
    }

    window = *(HWND*)((BYTE*)launcher + 0x10);
    if (window == NULL) {
        trace_client_state("multiplayer_auto_start", 0);
        return;
    }
    *(DWORD*)((BYTE*)launcher + 0x1C) = 1;
    is_posted = PostMessageA(window, WM_QUIT, 0, 0);
    trace_client_state("multiplayer_auto_start", is_posted != 0);
}

static int readable_pointer(const void* pointer) {
    MEMORY_BASIC_INFORMATION information;
    if (pointer == NULL || VirtualQuery(pointer, &information, sizeof(information)) == 0) {
        return 0;
    }
    if (information.State != MEM_COMMIT) {
        return 0;
    }
    if ((information.Protect & (PAGE_GUARD | PAGE_NOACCESS)) != 0) {
        return 0;
    }
    return 1;
}

static int readable_range(const void* pointer, size_t size) {
    MEMORY_BASIC_INFORMATION information;
    uintptr_t start = (uintptr_t)pointer;
    uintptr_t end;
    uintptr_t region_end;
    if (pointer == NULL || size == 0 || start > (uintptr_t)-1 - size) {
        return 0;
    }
    if (VirtualQuery(pointer, &information, sizeof(information)) == 0 ||
        information.State != MEM_COMMIT ||
        (information.Protect & (PAGE_GUARD | PAGE_NOACCESS)) != 0) {
        return 0;
    }
    end = start + size;
    region_end = (uintptr_t)information.BaseAddress + information.RegionSize;
    return end <= region_end;
}

static int finite_float_bits(const void* pointer) {
    unsigned int bits;
    memcpy(&bits, pointer, sizeof(bits));
    return (bits & 0x7F800000u) != 0x7F800000u;
}

static int initialize_effect_preview_offsets(void) {
    const unsigned char* metadata;
    const unsigned char* field;
    unsigned int field_count;
    unsigned int index;
    effect_preview_offsets offsets;
    memset(&offsets, 0xFF, sizeof(offsets));
    offsets.is_ready = 0;
    if (original_reflection_lookup == NULL) {
        return 0;
    }
    metadata = (const unsigned char*)original_reflection_lookup(0x8619FF24);
    if (!readable_range(metadata, 0x10)) {
        return 0;
    }
    field = *(const unsigned char* const*)(metadata + 0x08);
    field_count = *(const unsigned int*)(metadata + 0x0C);
    if (field_count > 256 || !readable_range(field, (size_t)field_count * 0x60)) {
        return 0;
    }
    for (index = 0; index < field_count; index++, field += 0x60) {
        unsigned int wire_index = *(const unsigned int*)(field + 0x50);
        unsigned int offset = *(const unsigned int*)(field + 0x14);
        switch (wire_index) {
        case 4:
            offsets.force_attached = offset;
            break;
        case 6:
            offsets.asset = offset;
            break;
        case 7:
            offsets.object_id = offset;
            break;
        case 10:
            offsets.position = offset;
            break;
        case 14:
            offsets.text_value = offset;
            break;
        case 15:
            offsets.client_event_id = offset;
            break;
        case 16:
            offsets.visibility_mask = offset;
            break;
        default:
            break;
        }
    }
    if (offsets.force_attached != 0x0B || offsets.asset != 0x10 ||
        offsets.object_id != 0x18 || offsets.position != 0x24 ||
        offsets.text_value != 0x58 || offsets.client_event_id != 0x5C ||
        offsets.visibility_mask != 0x60) {
        return 0;
    }
    effect_preview_field = offsets;
    effect_preview_field.is_ready = 1;
    return 1;
}

static void neutralize_debug_effect_preview(unsigned char* event, unsigned int failure) {
    if (event == NULL || !effect_preview_field.is_ready) {
        return;
    }
    *(unsigned int*)(event + effect_preview_field.asset) = 0;
    *(unsigned int*)(event + effect_preview_field.object_id) = 0;
    *(unsigned int*)(event + effect_preview_field.text_value) = 0;
    *(unsigned int*)(event + effect_preview_field.client_event_id) = 0;
    *(unsigned char*)(event + effect_preview_field.force_attached) = 0;
    *(unsigned char*)(event + effect_preview_field.visibility_mask) = 0;
    memset(event + effect_preview_field.position, 0, sizeof(float) * 3);
    trace_client_state("effect_preview_failure", failure);
}

static unsigned int prepare_debug_effect_preview(unsigned char* event) {
    const unsigned char* player;
    const unsigned char* object;
    void* registry;
    void* effect_system;
    unsigned int object_id;
    unsigned int asset;
    if (!effect_preview_field.is_ready || !readable_range(event, 0x98)) {
        return 1;
    }
    asset = *(const unsigned int*)(event + effect_preview_field.asset);
    if (asset == 0) {
        return 2;
    }
    if (original_controlled_player == NULL) {
        return 12;
    }
    player = (const unsigned char*)original_controlled_player();
    if (!readable_range(player, 4668)) {
        return 3;
    }
    object_id = *(const unsigned int*)(player + 4664);
    if (object_id == 0 || original_game_object_registry == NULL ||
        original_game_object_lookup == NULL) {
        return 4;
    }
    registry = original_game_object_registry();
    if (!readable_pointer(registry)) {
        return 5;
    }
    object = (const unsigned char*)original_game_object_lookup(registry, object_id);
    if (!readable_range(object, 0x24)) {
        return 6;
    }
    if (!finite_float_bits(object + 0x18) || !finite_float_bits(object + 0x1C) ||
        !finite_float_bits(object + 0x20)) {
        return 7;
    }
    if (original_effect_system == NULL) {
        return 8;
    }
    effect_system = original_effect_system();
    if (!readable_pointer(effect_system)) {
        return 9;
    }
    *(unsigned int*)(event + effect_preview_field.object_id) = 0;
    *(unsigned char*)(event + effect_preview_field.force_attached) = 0;
    memcpy(event + effect_preview_field.position, object + 0x18, sizeof(float) * 3);
    trace_client_state("effect_preview_asset", asset);
    trace_client_state("effect_preview_object", object_id);
    return 0;
}

static unsigned char __cdecl hooked_server_event_decode(void* stream, void* event,
    int flags, reflection_decode_callback_fn callback) {
    unsigned char result;
    unsigned char* value = (unsigned char*)event;
    unsigned int signature;
    unsigned int carrier;
    unsigned int carrier_asset;
    unsigned int failure;
    if (effect_preview_tls_index != TLS_OUT_OF_INDEXES) {
        TlsSetValue(effect_preview_tls_index, NULL);
    }
    result = original_server_event_decode(stream, event, flags, callback);
    if (!result) {
        return result;
    }
    if (!effect_preview_field.is_ready) {
        initialize_effect_preview_offsets();
    }
    if (!effect_preview_field.is_ready || !readable_range(value, 0x98)) {
        return result;
    }
    signature = *(const unsigned int*)(value + effect_preview_field.client_event_id);
    if (signature != effect_preview_signature) {
        return result;
    }
    carrier = *(const unsigned int*)(value + effect_preview_field.text_value);
    *(unsigned int*)(value + effect_preview_field.client_event_id) = 0;
    *(unsigned int*)(value + effect_preview_field.text_value) = 0;
    *(unsigned char*)(value + effect_preview_field.visibility_mask) = 0;
    carrier_asset = carrier ^ effect_preview_world_mode;
    if (carrier_asset == 0 || effect_preview_tls_index == TLS_OUT_OF_INDEXES) {
        neutralize_debug_effect_preview(value, 10);
        return result;
    }
    trace_client_state("effect_preview_asset_requested", carrier_asset);
    /* Field 6 is a reflected resource reference. After decoding it contains a
       resource object pointer, not the uint32 asset hash carried on the wire.
       Never restore the raw hash when resolution failed: sub_9C8E70 will
       dereference it as a resource object. prepare_debug_effect_preview
       rejects a null decoded reference before the retail presenter runs. */
    failure = prepare_debug_effect_preview(value);
    if (failure != 0) {
        neutralize_debug_effect_preview(value, failure);
        return result;
    }
    if (!TlsSetValue(effect_preview_tls_index, event)) {
        neutralize_debug_effect_preview(value, 11);
    }
    return result;
}

static unsigned char __cdecl hooked_server_event_validate(void* event) {
    void* preview_event;
    unsigned int failure;
    if (effect_preview_tls_index == TLS_OUT_OF_INDEXES) {
        return original_server_event_validate(event);
    }
    preview_event = TlsGetValue(effect_preview_tls_index);
    if (preview_event != event) {
        return original_server_event_validate(event);
    }
    TlsSetValue(effect_preview_tls_index, NULL);
    failure = prepare_debug_effect_preview((unsigned char*)event);
    if (failure != 0) {
        neutralize_debug_effect_preview((unsigned char*)event, failure);
        return 0;
    }
    trace_client_state("effect_preview_override", 1);
    return 1;
}

static int is_darkspin_chat_command(const char* text, const char* command) {
    size_t command_length;
    size_t index;
    char terminator;
    if (!readable_pointer(text)) {
        return 0;
    }
    command_length = strlen(command);
    for (index = 0; index < command_length; index++) {
        char text_character = text[index];
        char command_character = command[index];
        if (text_character >= 'A' && text_character <= 'Z') {
            text_character = (char)(text_character + ('a' - 'A'));
        }
        if (command_character >= 'A' && command_character <= 'Z') {
            command_character = (char)(command_character + ('a' - 'A'));
        }
        if (text_character != command_character) {
            return 0;
        }
    }
    terminator = text[command_length];
    return terminator == '\0' || terminator == ' ' || terminator == '\t';
}

/* Keep the accepted destination names at the client compatibility boundary.
   These are the complete build-103 Level resources indexed from Levels.package;
   the server receives only an exact canonical name selected here. */
static const char* fang_warp_locations[] = {
    "Creature_Vid_Capture",
    "CreatureEditor_EL",
    "cryos_1",
    "cryos_1_SM",
    "cryos_2",
    "cryos_2_PVP",
    "cryos_3",
    "cryos_4",
    "Game_Tutorial_cryos_1",
    "front_end_ship",
    "infinity_1",
    "infinity_1_PVP",
    "infinity_2",
    "infinity_3",
    "infinity_4",
    "infinity_4_SM",
    "Juggernaut_Mode_Testing",
    "nocturna_1",
    "nocturna_2",
    "nocturna_2_PVP",
    "nocturna_3",
    "nocturna_3_SM",
    "nocturna_4",
    "scaldron_1",
    "scaldron_1_PVP",
    "scaldron_2",
    "scaldron_3",
    "scaldron_4",
    "scaldron_4_SM",
    "Spectra_1",
    "Spectra_2",
    "Spectra_3",
    "Spectra_4",
    "test_AI_arena",
    "test_AI_zoo",
    "test_AI_zoo_bio",
    "test_AI_zoo_chrono",
    "test_AI_zoo_cyber",
    "test_AI_zoo_elites",
    "test_AI_zoo_plasma",
    "test_AI_zoo_supernatural",
    "test_AI_zoo_ugc",
    "test_Creature_Vid_Capture",
    "test_holodeck",
    "test_survivor_arena",
    "test_VFX_arena",
    "tnx173_2",
    "tnx173_3",
    "TNX_173",
    "verdanth_1",
    "verdanth_2",
    "verdanth_3",
    "verdanth_3_PVP",
    "verdanth_4",
    "verdanth_4_SM",
    "zelems_1",
    "zelems_1_SM",
    "zelems_2",
    "zelems_2_PVP",
    "zelems_3",
    "zelems_4"
};

/* Number only the disconnected and special-purpose areas surfaced by the
   level catalog. Keep this order aligned with the compact server chat listing;
   named matching still uses the complete location list above. */
static const char* fang_warp_aliases[] = {
    "Game_Tutorial_cryos_1",
    "CreatureEditor_EL",
    "Creature_Vid_Capture",
    "cryos_1_SM",
    "cryos_2_PVP",
    "front_end_ship",
    "infinity_1_PVP",
    "infinity_4_SM",
    "Juggernaut_Mode_Testing",
    "nocturna_2_PVP",
    "nocturna_3_SM",
    "scaldron_1_PVP",
    "scaldron_4_SM",
    "Spectra_1",
    "Spectra_2",
    "Spectra_3",
    "Spectra_4",
    "test_AI_arena",
    "test_AI_zoo",
    "test_AI_zoo_bio",
    "test_AI_zoo_chrono",
    "test_AI_zoo_cyber",
    "test_AI_zoo_elites",
    "test_AI_zoo_plasma",
    "test_AI_zoo_supernatural",
    "test_AI_zoo_ugc",
    "test_Creature_Vid_Capture",
    "test_holodeck",
    "test_survivor_arena",
    "test_VFX_arena",
    "TNX_173",
    "tnx173_2",
    "tnx173_3",
    "verdanth_3_PVP",
    "verdanth_4_SM",
    "zelems_1_SM",
    "zelems_2_PVP"
};

/* Keep developer NPC admission at the client compatibility boundary too.
   These are packaged, targetable combat families used by campaign, tutorial,
   captain, and Destructor content. The server still validates the selected
   canonical noun against the active warped zone's imported class catalog. */
static const char* fang_spawn_nouns[] = {
    "BabyMortar.Noun",
    "Boomer.Noun",
    "CitadelBasicGunner.Noun",
    "CitadelBasicMelee.Noun",
    "CitadelBasicRanged.Noun",
    "CitadelBasicShield.Noun",
    "CitadelBasicSuicide.Noun",
    "CitadelBoss.Noun",
    "CitadelBoss_2.Noun",
    "CitadelBoss_3.Noun",
    "CitadelSpecificFour.Noun",
    "CitadelSpecificOne.Noun",
    "CitadelSpecificThree.Noun",
    "CitadelSpecificTwo.Noun",
    "CitadelSpecialFour.Noun",
    "CitadelSpecialFour_Captain.Noun",
    "CitadelSpecialThree.Noun",
    "CitadelSpecialThree_Captain.Noun",
    "CitadelSpecialTwo.Noun",
    "CitadelSpecialTwo_Captain.Noun",
    "CryosBasicCharge.Noun",
    "CryosBasicFiery.Noun",
    "CryosBasicFireWave.Noun",
    "CryosBasicLightningMelee.Noun",
    "CryosBasicLightningRanged.Noun",
    "CryosBasicMelee.Noun",
    "CryosBasicPoison.Noun",
    "CryosBasicRanged.Noun",
    "CryosBoss.Noun",
    "CryosBoss_2.Noun",
    "CryosBoss_3.Noun",
    "CryosElementalSpecialThree.Noun",
    "CryosElementalSpecialThree_Captain.Noun",
    "CryosSpecialOne.Noun",
    "CryosSpecialOne_Captain.Noun",
    "CryosSpecialThree.Noun",
    "CryosSpecialThree_Captain.Noun",
    "CryosSpecialTwo.Noun",
    "CryosSpecialTwo_Captain.Noun",
    "MutationAgent.Noun",
    "nct_lieu_su_stealther.Noun",
    "nct_lieu_su_stealther_Captain.Noun",
    "nct_minn_su_drainer.Noun",
    "NoctBasicFlyer.Noun",
    "NoctBasicGhostCharger.Noun",
    "NoctBasicHopper.Noun",
    "NoctBasicMeleeDog.Noun",
    "NocturnaBasicHealthDrain.Noun",
    "NocturnaBasicRangedSilence.Noun",
    "NocturnaBasicStealth.Noun",
    "NocturnaSpecialDrift.Noun",
    "NocturnaSpecialHomer.Noun",
    "NocturnaSpecialLeech.Noun",
    "NocturnaSpecialLeech_Captain.Noun",
    "NocturnaSpecialMunch.Noun",
    "NocturnaSpecialMunch_Captain.Noun",
    "NomadBioSpecialTwo.Noun",
    "NomadBioSpecialTwo_Captain.Noun",
    "NomadCyberOne.Noun",
    "NomadDrag.Noun",
    "NomadRuption.Noun",
    "NomadRuption_Captain.Noun",
    "NomadScope.Noun",
    "NomadScope_Captain.Noun",
    "NomadShielder.Noun",
    "NomadShielder_Captain.Noun",
    "NomadSnipe.Noun",
    "NomadSnipe_Captain.Noun",
    "NomadSpacetimeAgent.Noun",
    "NomadSpecialOne.Noun",
    "NomadSpecialOne_Captain.Noun",
    "NomadSpecialThree.Noun",
    "NomadWithDrone.Noun",
    "NomadWithDrone_Captain.Noun",
    "Rezzer.Noun",
    "Rezzer_Captain.Noun",
    "ScaldronBasicBlink.Noun",
    "ScaldronBasicCopter.Noun",
    "ScaldronBasicDog.Noun",
    "ScaldronBasicDoppler.Noun",
    "ScaldronBasicMaser.Noun",
    "ScaldronBasicMines.Noun",
    "ScaldronBasicMonk.Noun",
    "ScaldronBasicNestle.Noun",
    "ScaldronBasicSinkhole.Noun",
    "ScaldronBasicThorno.Noun",
    "ScaldronBoss.Noun",
    "ScaldronBoss_2.Noun",
    "ScaldronBoss_3.Noun",
    "ShadowBoss.Noun",
    "ShadowBoss_2.Noun",
    "ShadowBoss_3.Noun",
    "ShadowBossMinion.Noun",
    "Shooter.Noun",
    "Sloth.Noun",
    "TutorialBasicDiseased.Noun",
    "TutorialBasicPoison.Noun",
    "TutorialBasicRanged.Noun",
    "TutorialSloth.Noun",
    "TutorialSpecialOne.Noun",
    "VerdanthBasicDiseased.Noun",
    "VerdanthBasicHealer.Noun",
    "VerdanthBasicMelee.Noun",
    "VerdanthBasicOoze.Noun",
    "VerdanthBasicPicky.Noun",
    "VerdanthBasicPlunge.Noun",
    "VerdanthBasicRanged.Noun",
    "VerdanthBasicRootmob.Noun",
    "VerdanthBasicSkeet.Noun",
    "VerdanthBoss.Noun",
    "VerdanthBoss_2.Noun",
    "VerdanthBoss_3.Noun",
    "VerdanthSpecialOne.Noun",
    "VerdanthSpecialOne_Captain.Noun",
    "VerdanthSpecialThree.Noun",
    "VerdanthSpecialThree_Captain.Noun",
    "VerdanthSpecialTwo.Noun",
    "VerdanthSpecialTwo_Captain.Noun",
    "ZelemBasicChargeup.Noun",
    "ZelemBasicFlyingMelee.Noun",
    "ZelemBasicHybrid.Noun",
    "ZelemBasicMelee.Noun",
    "ZelemBasicPackfly.Noun",
    "ZelemBasicPackMelee.Noun",
    "ZelemBasicRanged.Noun",
    "ZelemBasicRangedHoming.Noun",
    "ZelemBasicRepair.Noun",
    "ZelemBoss.Noun",
    "ZelemBoss_2.Noun",
    "ZelemBoss_3.Noun",
    "ZelemSpecialHaster.Noun",
    "ZelemSpecialHaster_Captain.Noun",
    "ZelemSpecialOne.Noun",
    "ZelemSpecialOne_Captain.Noun",
    "ZelemSpecialThree.Noun",
    "ZelemSpecialTwo.Noun",
    "ZelemSpecialTwo_Captain.Noun"
};

static char fang_ascii_lower(char character) {
    if (character >= 'A' && character <= 'Z') {
        return (char)(character + ('a' - 'A'));
    }
    return character;
}

static int fang_warp_name_equals(
    const char* location, const char* partial, size_t partial_length) {
    size_t location_length = strlen(location);
    size_t index;
    if (location_length != partial_length) {
        return 0;
    }
    for (index = 0; index < partial_length; index++) {
        if (fang_ascii_lower(location[index]) != fang_ascii_lower(partial[index])) {
            return 0;
        }
    }
    return 1;
}

static int fang_warp_name_contains(
    const char* location, const char* partial, size_t partial_length) {
    size_t location_length = strlen(location);
    size_t start;
    size_t index;
    if (partial_length == 0 || partial_length > location_length) {
        return 0;
    }
    for (start = 0; start + partial_length <= location_length; start++) {
        for (index = 0; index < partial_length; index++) {
            if (fang_ascii_lower(location[start + index]) !=
                fang_ascii_lower(partial[index])) {
                break;
            }
        }
        if (index == partial_length) {
            return 1;
        }
    }
    return 0;
}

static const char* find_fang_warp_alias(const char* partial, size_t partial_length) {
    unsigned int alias = 0;
    size_t index;
    size_t alias_count = sizeof(fang_warp_aliases) / sizeof(fang_warp_aliases[0]);
    if (partial_length == 0) {
        return NULL;
    }
    for (index = 0; index < partial_length; index++) {
        unsigned int digit;
        if (partial[index] < '0' || partial[index] > '9') {
            return NULL;
        }
        digit = (unsigned int)(partial[index] - '0');
        if (alias > 1000) {
            return NULL;
        }
        alias = alias * 10 + digit;
    }
    if (alias == 0 || alias > alias_count) {
        return NULL;
    }
    return fang_warp_aliases[alias - 1];
}

static const char* find_fang_warp_location(const char* partial, size_t partial_length) {
    const char* matched_location = find_fang_warp_alias(partial, partial_length);
    size_t location_index;
    unsigned int match_count = 0;
    if (matched_location != NULL) {
        return matched_location;
    }
    for (location_index = 0;
        location_index < sizeof(fang_warp_locations) / sizeof(fang_warp_locations[0]);
        location_index++) {
        const char* location = fang_warp_locations[location_index];
        if (fang_warp_name_equals(location, partial, partial_length)) {
            return location;
        }
        if (!fang_warp_name_contains(location, partial, partial_length)) {
            continue;
        }
        matched_location = location;
        match_count++;
    }
    if (match_count != 1) {
        return NULL;
    }
    return matched_location;
}

static int fang_spawn_name_equals(
    const char* noun_name, const char* partial, size_t partial_length) {
    size_t noun_length = strlen(noun_name);
    size_t index;
    if (fang_warp_name_equals(noun_name, partial, partial_length)) {
        return 1;
    }
    if (noun_length <= 5 || partial_length != noun_length - 5 ||
        fang_ascii_lower(noun_name[noun_length - 5]) != '.' ||
        fang_ascii_lower(noun_name[noun_length - 4]) != 'n' ||
        fang_ascii_lower(noun_name[noun_length - 3]) != 'o' ||
        fang_ascii_lower(noun_name[noun_length - 2]) != 'u' ||
        fang_ascii_lower(noun_name[noun_length - 1]) != 'n') {
        return 0;
    }
    for (index = 0; index < partial_length; index++) {
        if (fang_ascii_lower(noun_name[index]) != fang_ascii_lower(partial[index])) {
            return 0;
        }
    }
    return 1;
}

static const char* find_fang_spawn_noun(const char* partial, size_t partial_length) {
    const char* matched_noun = NULL;
    size_t noun_index;
    unsigned int match_count = 0;
    for (noun_index = 0;
        noun_index < sizeof(fang_spawn_nouns) / sizeof(fang_spawn_nouns[0]);
        noun_index++) {
        const char* noun_name = fang_spawn_nouns[noun_index];
        if (fang_spawn_name_equals(noun_name, partial, partial_length)) {
            return noun_name;
        }
        if (!fang_warp_name_contains(noun_name, partial, partial_length)) {
            continue;
        }
        matched_noun = noun_name;
        match_count++;
    }
    if (match_count != 1) {
        return NULL;
    }
    return matched_noun;
}

static char* find_darkspin_chat_command(const char* text, int* command) {
    const char* cursor = text;
    size_t remaining = 1024;
    if (!readable_pointer(text)) {
        return NULL;
    }
    while (remaining > 0 && *cursor != '\0') {
        if (*cursor == '/' &&
            (cursor == text || cursor[-1] == ' ' || cursor[-1] == '\t' || cursor[-1] == ']')) {
            if (is_darkspin_chat_command(cursor, "/ping")) {
                *command = 1;
                return (char*)cursor;
            }
            if (is_darkspin_chat_command(cursor, "/hint")) {
                *command = 21;
                return (char*)cursor;
            }
            if (is_darkspin_chat_command(cursor, "/loc")) {
                *command = 25;
                return (char*)cursor;
            }
            if (is_darkspin_chat_command(cursor, "/ss")) {
                *command = 22;
                return (char*)cursor;
            }
            if (is_darkspin_chat_command(cursor, "/help")) {
                *command = 3;
                return (char*)cursor;
            }
            if (is_darkspin_chat_command(cursor, "/exit")) {
                *command = 4;
                return (char*)cursor;
            }
            if (is_darkspin_chat_command(cursor, "/effect")) {
                *command = 5;
                return (char*)cursor;
            }
            if (is_darkspin_chat_command(cursor, "/summon")) {
                *command = 6;
                return (char*)cursor;
            }
            if (is_darkspin_chat_command(cursor, "/level")) {
                *command = 7;
                return (char*)cursor;
            }
            if (is_darkspin_chat_command(cursor, "/warp")) {
                *command = 23;
                return (char*)cursor;
            }
            if (is_darkspin_chat_command(cursor, "/spawn")) {
                *command = 24;
                return (char*)cursor;
            }
            if (is_darkspin_chat_command(cursor, "/dna")) {
                *command = 19;
                return (char*)cursor;
            }
            if (is_darkspin_chat_command(cursor, "/damage")) {
                *command = 8;
                return (char*)cursor;
            }
            if (is_darkspin_chat_command(cursor, "/heal")) {
                *command = 9;
                return (char*)cursor;
            }
            if (is_darkspin_chat_command(cursor, "/power")) {
                *command = 10;
                return (char*)cursor;
            }
            if (is_darkspin_chat_command(cursor, "/mana")) {
                *command = 11;
                return (char*)cursor;
            }
            if (is_darkspin_chat_command(cursor, "/event")) {
                *command = 12;
                return (char*)cursor;
            }
            if (is_darkspin_chat_command(cursor, "/goto")) {
                *command = 13;
                return (char*)cursor;
            }
            if (is_darkspin_chat_command(cursor, "/kill")) {
                *command = 14;
                return (char*)cursor;
            }
            if (is_darkspin_chat_command(cursor, "/reset")) {
                *command = 15;
                return (char*)cursor;
            }
            if (is_darkspin_chat_command(cursor, "/recap")) {
                *command = 20;
                return (char*)cursor;
            }
            if (is_darkspin_chat_command(cursor, "/victory")) {
                *command = 16;
                return (char*)cursor;
            }
            if (is_darkspin_chat_command(cursor, "/defeat")) {
                *command = 17;
                return (char*)cursor;
            }
            if (is_darkspin_chat_command(cursor, "/bug") ||
                is_darkspin_chat_command(cursor, "/b")) {
                *command = 18;
                return (char*)cursor;
            }
        }
        cursor++;
        remaining--;
    }
    return NULL;
}

static int normalize_fang_warp_command(
    const char* text, const char* command_text, char* output, size_t output_capacity) {
    const char* cursor = command_text + 5;
    const char* partial;
    const char* matched_location = NULL;
    size_t prefix_length;
    size_t partial_length;
    size_t location_length = 0;
    size_t output_length;
    if (!readable_pointer(text) || command_text < text || output == NULL || output_capacity == 0) {
        return -1;
    }
    while (*cursor == ' ' || *cursor == '\t') {
        cursor++;
    }
    partial = cursor;
    while (*cursor != '\0' && *cursor != ' ' && *cursor != '\t' &&
        *cursor != '\r' && *cursor != '\n') {
        cursor++;
    }
    partial_length = (size_t)(cursor - partial);
    while (*cursor == ' ' || *cursor == '\t' || *cursor == '\r' || *cursor == '\n') {
        cursor++;
    }
    if (*cursor == '\0') {
        matched_location = find_fang_warp_location(partial, partial_length);
    }
    prefix_length = (size_t)(command_text - text);
    if (matched_location != NULL) {
        location_length = strlen(matched_location);
    }
    output_length = prefix_length + 5;
    if (matched_location != NULL) {
        output_length += 1 + location_length;
    }
    if (output_length + 1 > output_capacity) {
        return -1;
    }
    if (prefix_length != 0) {
        memcpy(output, text, prefix_length);
    }
    memcpy(output + prefix_length, "/warp", 5);
    if (matched_location != NULL) {
        output[prefix_length + 5] = ' ';
        memcpy(output + prefix_length + 6, matched_location, location_length);
    }
    output[output_length] = '\0';
    return (int)output_length;
}

static int normalize_fang_spawn_command(
    const char* text, const char* command_text, char* output, size_t output_capacity) {
    const char* cursor = command_text + 6;
    const char* partial;
    const char* matched_noun = NULL;
    size_t prefix_length;
    size_t partial_length;
    size_t noun_length = 0;
    size_t output_length;
    if (!readable_pointer(text) || command_text < text || output == NULL || output_capacity == 0) {
        return -1;
    }
    while (*cursor == ' ' || *cursor == '\t') {
        cursor++;
    }
    partial = cursor;
    while (*cursor != '\0' && *cursor != ' ' && *cursor != '\t' &&
        *cursor != '\r' && *cursor != '\n') {
        cursor++;
    }
    partial_length = (size_t)(cursor - partial);
    while (*cursor == ' ' || *cursor == '\t' || *cursor == '\r' || *cursor == '\n') {
        cursor++;
    }
    if (*cursor == '\0') {
        matched_noun = find_fang_spawn_noun(partial, partial_length);
    }
    prefix_length = (size_t)(command_text - text);
    if (matched_noun != NULL) {
        noun_length = strlen(matched_noun);
    }
    output_length = prefix_length + 6;
    if (matched_noun != NULL) {
        output_length += 1 + noun_length;
    }
    if (output_length + 1 > output_capacity) {
        return -1;
    }
    if (prefix_length != 0) {
        memcpy(output, text, prefix_length);
    }
    memcpy(output + prefix_length, "/spawn", 6);
    if (matched_noun != NULL) {
        output[prefix_length + 6] = ' ';
        memcpy(output + prefix_length + 7, matched_noun, noun_length);
    }
    output[output_length] = '\0';
    return (int)output_length;
}

static void* __cdecl hooked_chat_text_convert(void* output, const char* text, int length) {
    char* command_text;
    const char* converted_source = text;
    char normalized_text[1024];
    void* converted_text;
    DWORD old_protection;
    DWORD ignored;
    int converted_length = length;
    int command = 0;
    int is_normalized = 0;
    command_text = find_darkspin_chat_command(text, &command);
    if (command == 23) {
        converted_length = normalize_fang_warp_command(
            text, command_text, normalized_text, sizeof(normalized_text));
        if (converted_length < 0) {
            return original_chat_text_convert(output, text, length);
        }
        converted_source = normalized_text;
        command_text = find_darkspin_chat_command(converted_source, &command);
        is_normalized = 1;
    }
    if (command == 24) {
        converted_length = normalize_fang_spawn_command(
            text, command_text, normalized_text, sizeof(normalized_text));
        if (converted_length < 0) {
            return original_chat_text_convert(output, text, length);
        }
        converted_source = normalized_text;
        command_text = find_darkspin_chat_command(converted_source, &command);
        is_normalized = 1;
    }
    if (command_text == NULL) {
        return original_chat_text_convert(output, text, length);
    }
    if (!is_normalized &&
        !VirtualProtect(command_text, 1, PAGE_READWRITE, &old_protection)) {
        return original_chat_text_convert(output, text, length);
    }
    prepare_chat_rooms_target();
    command_text[0] = 0x1F;
    converted_text = original_chat_text_convert(
        output, converted_source, converted_length);
    command_text[0] = '/';
    if (!is_normalized) {
        VirtualProtect(command_text, 1, old_protection, &ignored);
    }
    if (command != 0) {
        trace_client_state("chat_darkspin_command", (unsigned int)command);
    }
    if (command == 4) {
        int is_posted;
        InterlockedExchange(&chat_exit_pending, 1);
        is_posted = chat_game_window != NULL &&
            PostMessageA(chat_game_window, WM_CLOSE, 0, 0) != 0;
        if (!is_posted) {
            InterlockedExchange(&chat_exit_pending, 0);
        }
        trace_client_state("chat_exit", (unsigned int)is_posted);
    }
    if (command == 15) {
        int is_posted = chat_game_window != NULL &&
            PostMessageA(chat_game_window, CHAT_RESET_MESSAGE, 0, 0) != 0;
        trace_client_state("chat_reset_post", (unsigned int)is_posted);
    }
    return converted_text;
}

#if defined(__GNUC__)
static void __attribute__((fastcall)) hooked_login_screen_init(void* controller, void* unused, void* manager) {
#else
static void __fastcall hooked_login_screen_init(void* controller, void* unused, void* manager) {
#endif
    UINT_PTR timer;
    void** methods;
    login_ready_fn ready;
    login_submit_fn submit;
    (void)controller;
    (void)unused;
    original_login_screen_init(controller, manager);
    if (jwt_login_token[0] == '\0' || manager == NULL) {
        return;
    }
    jwt_login_manager = manager;
    methods = readable_pointer(manager) ? *(void***)manager : NULL;
    if (readable_pointer(methods)) {
        ready = (login_ready_fn)methods[1];
        submit = (login_submit_fn)methods[2];
        if (ready != NULL && submit != NULL && ready(manager) &&
            InterlockedCompareExchange(&jwt_login_started, 1, 0) == 0) {
            trace_client_state("jwt_login_submit", 1);
            trace_client_state("jwt_login_length", (unsigned int)strlen(jwt_login_token));
            submit(manager, "token@local.invalid", jwt_login_token);
            start_jwt_login_watchdog();
            return;
        }
    }
    if (InterlockedCompareExchange(&jwt_login_started, 0, 0) != 0 || jwt_login_timer != 0) {
        return;
    }
    jwt_login_retry_count = 0;
    timer = SetTimer(NULL, 0, 25, jwt_login_timer_callback);
    if (timer == 0) {
        trace_client_state("jwt_login_timer_failed", GetLastError());
        return;
    }
    jwt_login_timer = timer;
    trace_client_state("jwt_login_not_ready", 1);
}

static VOID CALLBACK jwt_login_timer_callback(HWND window, UINT message, UINT_PTR timer, DWORD time) {
    void** methods;
    login_ready_fn ready;
    login_submit_fn submit;
    void* manager = jwt_login_manager;
    (void)window;
    (void)message;
    (void)time;
    if (timer != jwt_login_timer) {
        return;
    }
    if (InterlockedCompareExchange(&jwt_login_started, 0, 0) != 0) {
        KillTimer(NULL, timer);
        jwt_login_timer = 0;
        return;
    }
    jwt_login_retry_count++;
    if (jwt_login_retry_count > 200 || jwt_login_token[0] == '\0' || !readable_pointer(manager)) {
        KillTimer(NULL, timer);
        jwt_login_timer = 0;
        trace_client_state("jwt_login_retry_expired", jwt_login_retry_count);
        return;
    }
    methods = *(void***)manager;
    if (!readable_pointer(methods)) {
        return;
    }
    ready = (login_ready_fn)methods[1];
    submit = (login_submit_fn)methods[2];
    if (ready == NULL || submit == NULL || !ready(manager)) {
        return;
    }
    if (InterlockedCompareExchange(&jwt_login_started, 1, 0) != 0) {
        return;
    }
    KillTimer(NULL, timer);
    jwt_login_timer = 0;
    trace_client_state("jwt_login_submit", 1);
    trace_client_state("jwt_login_length", (unsigned int)strlen(jwt_login_token));
    submit(manager, "token@local.invalid", jwt_login_token);
    start_jwt_login_watchdog();
    /* The build-103 submitter queues serialization and may retain the PASS
       pointer after this call returns. Keep the process-private buffer alive
       so the queued login cannot observe a cleared credential. */
}

static void trace_client_state(const char* kind, unsigned int value) {
    char line[512];
    DWORD written;
    int length;
    if (trace_file == INVALID_HANDLE_VALUE || !trace_lock_ready) {
        return;
    }
    length = snprintf(line, sizeof(line),
        "{\"time_ms\":%llu,\"protocol\":\"client_state\",\"kind\":\"%s\","
        "\"thread\":%lu,\"frame_sequence\":%lu,\"frame_delta_bits\":%lu,"
        "\"frame_time_ms\":%lu,\"value\":%u}\r\n",
        (unsigned long long)GetTickCount(), kind,
        (unsigned long)GetCurrentThreadId(),
        (unsigned long)InterlockedCompareExchange(&snapshot_frame_sequence, 0, 0),
        (unsigned long)InterlockedCompareExchange(&snapshot_frame_delta_bits, 0, 0),
        (unsigned long)InterlockedCompareExchange(&snapshot_frame_time_ms, 0, 0), value);
    if (length <= 0 || (size_t)length >= sizeof(line)) {
        return;
    }
    EnterCriticalSection(&trace_lock);
    WriteFile(trace_file, line, (DWORD)length, &written, NULL);
    LeaveCriticalSection(&trace_lock);
    if (InterlockedCompareExchange(&snapshot_capture_enabled, 0, 0) != 0) {
        snapshot_ring_append(line, (unsigned int)length);
    }
}

static void* hooked_ability_descriptor_lookup(unsigned int ability_guid) {
    char line[384];
    DWORD written;
    int length;
    void* descriptor = original_ability_descriptor_lookup(ability_guid);
    unsigned int label_id = 0;
    unsigned int icon_id = 0;
    unsigned int is_resolved = readable_range(descriptor, 124);
    unsigned int scene_asset_id =
        (unsigned int)InterlockedCompareExchange(&traced_scene_asset_id, 0, 0);
    if (is_resolved) {
        label_id = *(const unsigned int*)((const BYTE*)descriptor + 100);
        icon_id = *(const unsigned int*)((const BYTE*)descriptor + 120);
    }
    if (trace_file == INVALID_HANDLE_VALUE || !trace_lock_ready) {
        return descriptor;
    }
    length = snprintf(line, sizeof(line),
        "{\"time_ms\":%llu,\"protocol\":\"client_scene\","
        "\"kind\":\"ability_descriptor\",\"scene_asset_id\":%u,"
        "\"ability_guid\":%u,\"descriptor\":%u,\"is_resolved\":%u,"
        "\"label_id\":%u,\"icon_id\":%u}\r\n",
        (unsigned long long)GetTickCount(), scene_asset_id, ability_guid,
        (unsigned int)(uintptr_t)descriptor, is_resolved, label_id, icon_id);
    if (length <= 0) {
        return descriptor;
    }
    EnterCriticalSection(&trace_lock);
    WriteFile(trace_file, line, (DWORD)length, &written, NULL);
    LeaveCriticalSection(&trace_lock);
    return descriptor;
}

static int trace_xp_threshold_values(const unsigned int* begin, size_t count) {
    size_t index;
    char line[256];
    DWORD written;
    int length;
    if (InterlockedCompareExchange(&xp_thresholds_traced, 0, 0) != 0) {
        return 1;
    }
    if (trace_file == INVALID_HANDLE_VALUE || !trace_lock_ready) {
        return 0;
    }
    if (count == 0 || count > 256 || !readable_pointer(begin) ||
        !readable_pointer(begin + count - 1)) {
        return 0;
    }
    if (InterlockedCompareExchange(&xp_thresholds_traced, 1, 0) != 0) {
        return 1;
    }
    for (index = 0; index < count; index++) {
        length = snprintf(line, sizeof(line),
            "{\"time_ms\":%llu,\"protocol\":\"client_state\","
            "\"kind\":\"xp_threshold\",\"index\":%u,\"value\":%u}\r\n",
            (unsigned long long)GetTickCount(), (unsigned int)index, begin[index]);
        if (length <= 0) {
            continue;
        }
        EnterCriticalSection(&trace_lock);
        WriteFile(trace_file, line, (DWORD)length, &written, NULL);
        LeaveCriticalSection(&trace_lock);
    }
    return 1;
}

static int trace_xp_thresholds(void) {
    const unsigned int* begin;
    const unsigned int* end;
    if (InterlockedCompareExchange(&xp_thresholds_traced, 0, 0) != 0) {
        return 1;
    }
    if (executable_module == NULL) {
        return 0;
    }
    begin = *(const unsigned int**)((const BYTE*)executable_module + 0xD64CA0);
    end = *(const unsigned int**)((const BYTE*)executable_module + 0xD64CA4);
    if (!readable_pointer(begin) || !readable_pointer(end) || end <= begin) {
        return 0;
    }
    return trace_xp_threshold_values(begin, (size_t)(end - begin));
}

static int trace_xp_thresholds_from_resource(void) {
    void* manager;
    void** manager_method;
    void* resource = NULL;
    void** resource_method;
    resource_lookup_fn lookup;
    resource_release_fn release;
    unsigned int count = 0;
    const unsigned int* value = NULL;
    unsigned char is_read;
    int is_traced = 0;
    if (original_resource_manager == NULL || original_property_vector_read == NULL) {
        return 0;
    }
    manager = original_resource_manager();
    if (!readable_pointer(manager)) {
        return 0;
    }
    manager_method = *(void***)manager;
    if (!readable_pointer(manager_method) || !readable_pointer(manager_method + 11)) {
        return 0;
    }
    lookup = (resource_lookup_fn)manager_method[11];
    if (!readable_pointer((const void*)lookup)) {
        return 0;
    }
    lookup(manager, 0x3B01D7F6, 0x3B01D7F6, &resource);
    if (!readable_pointer(resource)) {
        return 0;
    }
    is_read = original_property_vector_read(resource, 0xC0B32F0F, &count, &value);
    if (is_read) {
        is_traced = trace_xp_threshold_values(value, count);
    }
    resource_method = *(void***)resource;
    if (readable_pointer(resource_method) && readable_pointer(resource_method + 1)) {
        release = (resource_release_fn)resource_method[1];
        if (readable_pointer((const void*)release)) {
            release(resource);
        }
    }
    return is_traced;
}

static unsigned char __cdecl hooked_property_vector_read(void* resource, unsigned int property_id,
    unsigned int* count, const unsigned int** value) {
    unsigned char is_read = original_property_vector_read(resource, property_id, count, value);
    if (is_read && property_id == 0xC0B32F0F && count != NULL && value != NULL) {
        (void)trace_xp_threshold_values(*value, *count);
    }
    return is_read;
}

static DWORD WINAPI trace_xp_thresholds_later(LPVOID parameter) {
    int attempt;
    (void)parameter;
    for (attempt = 0; attempt < 600; attempt++) {
        if (trace_xp_thresholds()) {
            InterlockedExchange(&xp_threshold_trace_pending, 0);
            return 0;
        }
        if (attempt == 50) {
            trace_client_state("xp_resource_attempt", 1);
            if (trace_xp_thresholds_from_resource()) {
                InterlockedExchange(&xp_threshold_trace_pending, 0);
                return 0;
            }
            trace_client_state("xp_resource_result", 0);
        }
        Sleep(100);
    }
    InterlockedExchange(&xp_threshold_trace_pending, 0);
    trace_client_state("xp_threshold_timeout", 1);
    return 1;
}

static void trace_scene_state(const char* kind, unsigned int asset_id,
    unsigned int value_a, unsigned int value_b, int is_resolved) {
    char line[384];
    DWORD written;
    int length;
    if (trace_file == INVALID_HANDLE_VALUE || !trace_lock_ready) {
        return;
    }
    length = snprintf(line, sizeof(line),
        "{\"time_ms\":%llu,\"protocol\":\"client_scene\",\"kind\":\"%s\","
        "\"asset_id\":%u,\"asset_hash\":\"0x%08X\",\"value_a\":%u,"
        "\"value_b\":%u,\"is_resolved\":%s}\r\n",
        (unsigned long long)GetTickCount(), kind, asset_id, asset_id,
        value_a, value_b, is_resolved ? "true" : "false");
    if (length <= 0) {
        return;
    }
    EnterCriticalSection(&trace_lock);
    WriteFile(trace_file, line, (DWORD)length, &written, NULL);
    LeaveCriticalSection(&trace_lock);
}

static void trace_game_prepare_decode(const unsigned int* value, unsigned int requested_size,
    unsigned int decoded_size, unsigned int stream_size, unsigned int context_offset) {
    char line[384];
    DWORD written;
    int length;
    if (trace_file == INVALID_HANDLE_VALUE || !trace_lock_ready || value == NULL) {
        return;
    }
    length = snprintf(line, sizeof(line),
        "{\"time_ms\":%llu,\"protocol\":\"client_scene\","
        "\"kind\":\"game_prepare_decode\",\"requested_size\":%u,\"decoded_size\":%u,"
        "\"stream_size\":%u,\"context_offset\":%u,"
        "\"value_0\":%u,\"value_0_hex\":\"0x%08X\",\"value_1\":%u,"
        "\"value_1_hex\":\"0x%08X\",\"value_2\":%u,\"value_3\":%u}\r\n",
        (unsigned long long)GetTickCount(), requested_size, decoded_size, stream_size, context_offset,
        value[0], value[0], value[1], value[1], value[2], value[3]);
    if (length <= 0) {
        return;
    }
    EnterCriticalSection(&trace_lock);
    WriteFile(trace_file, line, (DWORD)length, &written, NULL);
    LeaveCriticalSection(&trace_lock);
}

#if defined(__GNUC__)
static unsigned int __attribute__((fastcall)) hooked_quick_game_read(
    void* stream, void* ignored, void* destination, unsigned int size, void* context) {
#else
static unsigned int __fastcall hooked_quick_game_read(
    void* stream, void* ignored, void* destination, unsigned int size, void* context) {
#endif
    unsigned int decoded_size;
    unsigned int stream_size = stream != NULL ? *(const unsigned int*)((const BYTE*)stream + 8) : 0;
    unsigned int context_offset = context != NULL ? *(const unsigned int*)((const BYTE*)context + 4) : 0;
    (void)ignored;
    decoded_size = original_message_read(stream, destination, size, context);
    if (destination != NULL && size >= 16) {
        trace_game_prepare_decode((const unsigned int*)destination, size, decoded_size,
            stream_size, context_offset);
    }
    return decoded_size;
}

static unsigned int traced_scene_id(void* scene) {
    void* current = InterlockedCompareExchangePointer(
        (PVOID volatile*)&traced_scene_asset, NULL, NULL);
    if (scene == NULL || scene != current) {
        return 0;
    }
    return (unsigned int)InterlockedCompareExchange(&traced_scene_asset_id, 0, 0);
}

static void* __cdecl hooked_scene_asset_lookup(unsigned int asset_id) {
    void* scene = original_scene_asset_lookup(asset_id);
    InterlockedExchange(&traced_scene_asset_id, (LONG)asset_id);
    InterlockedExchangePointer((PVOID volatile*)&traced_scene_asset, scene);
    trace_scene_state("scene_asset_lookup", asset_id, 0, 0, scene != NULL);
    return scene;
}

static void* __cdecl hooked_creature_asset_lookup(unsigned int asset_id, void* output) {
    void* asset;
    unsigned int nested_asset_id = 0;
    trace_scene_state("creature_asset_lookup_input", asset_id,
        (unsigned int)(uintptr_t)output, 0, asset_id != 0);
    asset = original_creature_asset_lookup(asset_id, output);
    if (readable_pointer(asset) && readable_pointer((const BYTE*)asset + 0xA4)) {
        nested_asset_id = *(const unsigned int*)((const BYTE*)asset + 0xA4);
    }
    trace_scene_state("creature_asset_lookup", asset_id, nested_asset_id,
        (unsigned int)(uintptr_t)asset, asset != NULL && nested_asset_id != 0);
    return asset;
}

static void* __cdecl hooked_creature_nested_lookup(void* asset, void* output) {
    trace_scene_state("creature_nested_lookup", (unsigned int)(uintptr_t)asset,
        (unsigned int)(uintptr_t)output, 0, asset != NULL);
    return original_creature_nested_lookup(asset, output);
}

static void __cdecl hooked_scene_load(void* scene, unsigned int markerset, unsigned int index) {
    unsigned int asset_id = traced_scene_id(scene);
    trace_scene_state("scene_load_request", asset_id, markerset, index, asset_id != 0);
    original_scene_load(scene, markerset, index);
}

static void __cdecl hooked_scene_change(void* simulator, void* scene) {
    unsigned int asset_id = traced_scene_id(scene);
#if FANG_DIAGNOSTICS
    HANDLE xp_thread;
#endif
    trace_scene_state("scene_change", asset_id, 0, 0, asset_id != 0);
    trace_client_state("scene_input_reset", reset_combat_input_state());
#if !FANG_DIAGNOSTICS
    original_scene_change(simulator, scene);
    return;
#else
    if (trace_xp_thresholds() ||
        InterlockedCompareExchange(&xp_threshold_trace_pending, 1, 0) != 0) {
        original_scene_change(simulator, scene);
        return;
    }
    xp_thread = CreateThread(NULL, 0, trace_xp_thresholds_later, NULL, 0, NULL);
    if (xp_thread == NULL) {
        InterlockedExchange(&xp_threshold_trace_pending, 0);
        trace_client_state("xp_threshold_thread", 1);
        original_scene_change(simulator, scene);
        return;
    }
    CloseHandle(xp_thread);
    original_scene_change(simulator, scene);
#endif
}

static DWORD WINAPI stop_started_movie(LPVOID parameter) {
    /*
     * Game's boot state uses movie-manager vtable slot 5 (+0x14) to
     * detect active playback and slot 9 (+0x24) for its native skip action.
     * Wait until the scripted ship movie has actually started so stopping it
     * preserves the loader/player lifecycle expected by the camera script.
     */
    int attempt;
    (void)parameter;
    for (attempt = 0; attempt < 2000; attempt++) {
        void* manager = original_movie_manager();
        if (manager != NULL) {
            void** methods = *(void***)manager;
            movie_playing_fn playing = (movie_playing_fn)methods[5];
            movie_stop_fn stop = (movie_stop_fn)methods[9];
            if (playing(manager)) {
                stop(manager);
                trace_client_state("movie_stop", 1);
                InterlockedExchange(&movie_skip_pending, 0);
                return 0;
            }
        }
        Sleep(1);
    }
    trace_client_state("movie_stop_timeout", 1);
    InterlockedExchange(&movie_skip_pending, 0);
    return 1;
}

static void __cdecl hooked_movie_load(void* output, const char* name, int unknown_a, int unknown_b) {
    size_t length = name == NULL ? 0 : strlen(name);
    original_movie_load(output, name, unknown_a, unknown_b);
    if (length >= 4 && _stricmp(name + length - 4, ".vp6") == 0 &&
        InterlockedCompareExchange(&ship_spline_skip_active, 0, 0) == 0 &&
        InterlockedCompareExchange(&movie_skip_pending, 1, 0) == 0) {
        HANDLE stop_thread = CreateThread(NULL, 0, stop_started_movie, NULL, 0, NULL);
        if (stop_thread != NULL) {
            CloseHandle(stop_thread);
        } else {
            InterlockedExchange(&movie_skip_pending, 0);
            trace_client_state("movie_stop_thread", 1);
        }
    }
}

static int complete_active_spline(void) {
    void* skipped;
    void* registry;
    void* camera_owner = NULL;
    void* render_camera = NULL;

    InterlockedExchange(&ship_spline_skip_active, 1);
    skipped = original_spline_skip();
    registry = original_camera_registry();
    if (registry != NULL) {
        object_method_fn owner = (object_method_fn)(*(void***)registry)[27];
        void* scene = owner(registry);
        if (scene != NULL) {
            object_method_fn active = (object_method_fn)(*(void***)scene)[14];
            camera_owner = active(scene);
        }
    }
    if (camera_owner != NULL) {
        void* components = (BYTE*)camera_owner + 4;
        lookup_method_fn lookup = (lookup_method_fn)(*(void***)components)[3];
        render_camera = lookup(components, 0x06DE3415);
    }
    if (skipped != NULL && render_camera != NULL) {
        original_camera_set(render_camera, (BYTE*)skipped + 0x2A4, (BYTE*)skipped + 0x2B0,
            *(float*)((BYTE*)skipped + 0x2C0), *(float*)((BYTE*)skipped + 0x2C4),
            *(float*)((BYTE*)skipped + 0x2C8));
    }
    {
        void* manager = original_movie_manager();
        if (manager != NULL) {
            movie_stop_fn finish = (movie_stop_fn)(*(void***)manager)[8];
            finish(manager);
        }
    }
    InterlockedExchange(&ship_spline_skip_active, 0);
    return skipped != NULL;
}

static unsigned char __cdecl hooked_spline_update(void* camera, float delta) {
    if (camera != NULL) {
        BYTE* base = (BYTE*)camera;
        int node_count = *(int*)(base + 0x29C);
        float current_time = *(float*)(base + 0x90);
        float duration = *(float*)(base + 0x2A0) + *(float*)(base + 0x88);
        if (camera != traced_spline_camera || node_count != traced_spline_nodes ||
            current_time + 0.001f < traced_spline_time) {
            unsigned int duration_ms = duration > 0.0f ? (unsigned int)(duration * 1000.0f) : 0;
            trace_client_state("spline_nodes", (unsigned int)node_count);
            trace_client_state("spline_duration_ms", duration_ms);
        }
        traced_spline_camera = camera;
        traced_spline_nodes = node_count;
        traced_spline_time = current_time;
    }
    if (camera != NULL && skip_cinematics_enabled) {
        BYTE* base = (BYTE*)camera;
        int node_count = *(int*)(base + 0x29C);
        float duration = *(float*)(base + 0x2A0) + *(float*)(base + 0x88);
        volatile LONG* skip_state = NULL;
        const char* trace_kind = NULL;
        /*
         * Game 5.3.0.103's post-login ship tour is the 14-node,
         * 23.75-second spline. Its START action begins a second six-node,
         * 2.75-second transition into the Arsenal. Use the engine's manual
         * skip routine for both and reproduce its caller's continuation at
         * VA 0x0052C211. Calling only the skip routine leaves the transition
         * white.
         */
        if (node_count == 14 && duration > 23.7f && duration < 23.8f) {
            skip_state = &ship_spline_skipped;
            trace_kind = "spline_skip";
        } else if (node_count == 6 && duration > 2.7f && duration < 2.8f &&
            InterlockedCompareExchange(&ship_start_selected, 0, 0) != 0) {
            skip_state = &ship_start_transition_skipped;
            trace_kind = "ship_start_transition_skip";
        }
        if (skip_state != NULL && InterlockedCompareExchange(skip_state, 1, 0) == 0) {
            int is_completed = complete_active_spline();
            trace_client_state(trace_kind, is_completed);
            return 0;
        }
    }
    return original_spline_update(camera, delta);
}

static unsigned int repair_rooms_target(void) {
    void* platform = original_online_platform();
    void** rooms;
    void* api;
    void* view;
    void* category;
    unsigned int room_id;
    if (!readable_range(platform, 13 * sizeof(void*))) {
        return 0;
    }
    rooms = (void**)((void**)platform)[12];
    if (!readable_range(rooms, 6 * sizeof(void*))) {
        return 0;
    }
    if (rooms[3] != NULL) {
        return 2;
    }
    api = rooms[2];
    if (!readable_range(api, 468)) {
        return 0;
    }
    view = original_rooms_map_lookup((BYTE*)api + 444, (unsigned int)(uintptr_t)rooms[4]);
    if (view == NULL) {
        return 0;
    }
    category = original_rooms_map_lookup(view, (unsigned int)(uintptr_t)rooms[5]);
    if (category == NULL || !readable_range(category, 8)) {
        return 0;
    }
    /*
     * This is the same nested lookup performed by build 103's successful
     * join callback. Notifications can be dispatched after that callback on
     * the accelerated ship path, so repeat it after the SDK maps are live.
     * A selected category contains only rooms belonging to that category.
     */
    for (room_id = 1; room_id <= 64; room_id++) {
        void* room = original_rooms_map_lookup((BYTE*)category + 4, room_id);
        if (room != NULL) {
            rooms[3] = room;
            trace_client_state("rooms_target_id", room_id);
            return 1;
        }
    }
    return 0;
}

static unsigned int repair_chat_rooms_target(void) {
    void* platform = original_online_platform();
    void* chat = original_chat_lookup();
    void** rooms;
    void* room;
    if (!readable_range(platform, 13 * sizeof(void*)) ||
        !readable_range(chat, 13 * sizeof(void*))) {
        return 0;
    }
    rooms = (void**)((void**)platform)[12];
    if (!readable_range(rooms, 4 * sizeof(void*))) {
        return 0;
    }
    room = rooms[3];
    if (!readable_range(room, 4 * sizeof(void*)) ||
        ((void**)room)[3] == NULL || ((void**)chat)[12] == room) {
        return 0;
    }
    ((void**)chat)[12] = room;
    return 1;
}

static unsigned int prepare_chat_rooms_target(void) {
    void* platform;
    void* rooms;
    DWORD now;
    unsigned int state = repair_rooms_target();
    if (state != 0) {
        InterlockedExchange(&rooms_target_repaired, 1);
    }
    now = GetTickCount();
    if (chat_rooms_bootstrap_time == 0 ||
        (state == 0 && now - chat_rooms_bootstrap_time >= 1000)) {
        platform = original_online_platform();
        rooms = NULL;
        if (readable_range(platform, 13 * sizeof(void*))) {
            rooms = ((void**)platform)[12];
        }
        if (rooms != NULL && readable_range(rooms, 4 * sizeof(void*))) {
            chat_rooms_bootstrap_time = now;
            original_rooms_bootstrap(platform);
            InterlockedExchange(&rooms_bootstrap_started, 1);
            trace_client_state("chat_rooms_bootstrap", 1);
            state |= 4;
        }
    }
    if (repair_chat_rooms_target() != 0) {
        trace_client_state("chat_rooms_target_repaired", 1);
        state |= 8;
    }
    return state;
}

static void trace_rooms_roster(void) {
    void* platform = original_online_platform();
    void** rooms;
    BYTE* room;
    BYTE* members_begin;
    BYTE* members_end;
    unsigned int member_count;
    unsigned int resolved_count = 0;
    unsigned int index;
    if (!readable_range(platform, 13 * sizeof(void*))) {
        return;
    }
    rooms = (void**)((void**)platform)[12];
    if (!readable_range(rooms, 4 * sizeof(void*))) {
        return;
    }
    room = (BYTE*)rooms[3];
    if (!readable_range(room, 308)) {
        return;
    }
    members_begin = *(BYTE**)(room + 300);
    members_end = *(BYTE**)(room + 304);
    if (members_begin == NULL || members_end == NULL || members_end < members_begin ||
        (size_t)(members_end - members_begin) > 64 * sizeof(void*) ||
        ((size_t)(members_end - members_begin) % sizeof(void*)) != 0 ||
        !readable_range(members_begin, (size_t)(members_end - members_begin))) {
        member_count = 0;
    } else {
        member_count = (unsigned int)((members_end - members_begin) / sizeof(void*));
        for (index = 0; index < member_count; index++) {
            BYTE* member = *(BYTE**)(members_begin + index * sizeof(void*));
            if (readable_range(member, 2 * sizeof(void*)) && *(void**)(member + sizeof(void*)) != NULL) {
                resolved_count++;
            }
        }
    }
    if (room == traced_rooms_roster && member_count == traced_rooms_member_count &&
        resolved_count == traced_rooms_resolved_count) {
        return;
    }
    traced_rooms_roster = room;
    traced_rooms_member_count = member_count;
    traced_rooms_resolved_count = resolved_count;
    trace_client_state("rooms_roster_members", member_count);
    trace_client_state("rooms_roster_resolved", resolved_count);
}

static void trace_party_navigation_gate(void* party, unsigned char is_ready) {
    BYTE* party_bytes = (BYTE*)party;
    void** buckets;
    void* sentinel;
    void* platform;
    void** vtable;
    presence_query_fn query;
    unsigned int bucket_count;
    unsigned int member_count;
    unsigned int bucket_index;
    unsigned int traced_count = 0;
    trace_client_state("party_navigation_gate", is_ready != 0);
    if (!readable_range(party_bytes, 268)) {
        trace_client_state("party_navigation_owner", 0);
        return;
    }
    buckets = *(void***)(party_bytes + 256);
    bucket_count = *(const unsigned int*)(party_bytes + 260);
    member_count = *(const unsigned int*)(party_bytes + 264);
    trace_client_state("party_navigation_members", member_count);
    if (buckets == NULL || bucket_count == 0 || bucket_count > 4096 ||
        !readable_range(buckets, ((size_t)bucket_count + 1) * sizeof(void*))) {
        trace_client_state("party_navigation_buckets", 0);
        return;
    }
    sentinel = buckets[bucket_count];
    platform = original_online_platform();
    if (!readable_range(platform, sizeof(void*)) ||
        !readable_range(*(void***)platform, 49 * sizeof(void*))) {
        trace_client_state("party_navigation_platform", 0);
        return;
    }
    vtable = *(void***)platform;
    query = (presence_query_fn)vtable[48];
    if (query == NULL) {
        trace_client_state("party_navigation_query", 0);
        return;
    }
    for (bucket_index = 0; bucket_index < bucket_count && traced_count < 16; bucket_index++) {
        BYTE* member = (BYTE*)buckets[bucket_index];
        while (member != NULL && member != sentinel && traced_count < 16) {
            unsigned int presence = UINT_MAX;
            unsigned short playgroup = USHRT_MAX;
            unsigned int level_id = UINT_MAX;
            unsigned short client_data = USHRT_MAX;
            unsigned char is_resolved;
            char kind[64];
            if (!readable_range(member, 52)) {
                trace_client_state("party_navigation_member_read", 0);
                return;
            }
            is_resolved = query(platform,
                *(const unsigned int*)(member + 0), *(const unsigned int*)(member + 4),
                &presence, &playgroup, &level_id, &client_data);
            snprintf(kind, sizeof(kind), "party_navigation_%u_id_low", traced_count);
            trace_client_state(kind, *(const unsigned int*)(member + 0));
            snprintf(kind, sizeof(kind), "party_navigation_%u_id_high", traced_count);
            trace_client_state(kind, *(const unsigned int*)(member + 4));
            snprintf(kind, sizeof(kind), "party_navigation_%u_resolved", traced_count);
            trace_client_state(kind, is_resolved != 0);
            snprintf(kind, sizeof(kind), "party_navigation_%u_presence", traced_count);
            trace_client_state(kind, presence);
            snprintf(kind, sizeof(kind), "party_navigation_%u_playgroup", traced_count);
            trace_client_state(kind, playgroup);
            snprintf(kind, sizeof(kind), "party_navigation_%u_level", traced_count);
            trace_client_state(kind, level_id);
            snprintf(kind, sizeof(kind), "party_navigation_%u_client_data", traced_count);
            trace_client_state(kind, client_data);
            traced_count++;
            member = *(BYTE**)(member + 48);
        }
    }
    trace_client_state("party_navigation_traced", traced_count);
}

static unsigned char FANG_THISCALL hooked_party_ready(void* party) {
    unsigned char is_ready = original_party_ready(party);
    trace_party_navigation_gate(party, is_ready);
    return is_ready;
}

static void __cdecl hooked_navigation_update(void* controller) {
    navigation_event event;
    int is_return;
    unsigned int new_player_progress;
    original_navigation_update(controller);
    if (!skip_cinematics_enabled) {
        return;
    }
    if (InterlockedCompareExchange(&ship_navigation_ready, 0, 0) == 0) {
        return;
    }
    is_return = InterlockedCompareExchange(&ship_start_selected, 0, 0) != 0;
    new_player_progress = original_new_player_progress();
    if (!is_return && InterlockedCompareExchange(&tutorial_start_selected, 0, 0) == 0) {
        trace_client_state("new_player_progress", new_player_progress);
    }
    if (!is_return && new_player_progress <= 2000) {
        if (InterlockedCompareExchange(&tutorial_start_selected, 1, 0) == 0) {
            original_map_room_start_game();
            trace_client_state("tutorial_start", new_player_progress);
        }
        return;
    }
    /*
     * Progress 3000 and later belong to native ship-room flow. Calling the
     * weakly-typed Flash callback directly faults at both the first Arsenal
     * lesson and an established progress-9000 profile before it can emit
     * ship_start. Cinematic acceleration may skip camera presentation, but
     * room selection must remain with the native UI.
     */
    if (!is_return && new_player_progress >= 3000) {
        if (InterlockedCompareExchange(&rooms_bootstrap_started, 1, 0) == 0) {
            void* platform = original_online_platform();
            void* rooms = NULL;
            if (readable_range(platform, 13 * sizeof(void*))) {
                rooms = ((void**)platform)[12];
            }
            if (rooms != NULL && readable_range(rooms, 4 * sizeof(void*))) {
                original_rooms_bootstrap(platform);
                chat_rooms_bootstrap_time = GetTickCount();
                trace_client_state("rooms_bootstrap", 1);
            } else {
                InterlockedExchange(&rooms_bootstrap_started, 0);
                trace_client_state("rooms_bootstrap", 0);
            }
        }
        if (InterlockedCompareExchange(&rooms_bootstrap_started, 0, 0) != 0 &&
            InterlockedCompareExchange(&rooms_target_repaired, 0, 0) == 0) {
            unsigned int repair_state = repair_rooms_target();
            if (repair_state != 0) {
                InterlockedExchange(&rooms_target_repaired, 1);
                trace_client_state("rooms_target_ready", repair_state);
            }
        }
        /*
         * Result teardown can clear only the chat controller's selected Rooms
         * pointer while the SDK-owned room remains valid. Repair that projection
         * on every established ship frame so native /lobby works without first
         * requiring a Darkspin developer command.
         */
        if (repair_chat_rooms_target() != 0) {
            trace_client_state("chat_rooms_target_repaired", 1);
        }
        trace_rooms_roster();
        if (InterlockedCompareExchange(&ship_start_deferred, 1, 0) == 0) {
            trace_client_state("ship_start_deferred", new_player_progress);
        }
        return;
    }
    if (is_return) {
        if (GetTickCount() - ship_navigation_ready_time < 250) {
            return;
        }
        if (InterlockedCompareExchange(&ship_return_start_pending, 0, 1) != 1) {
            return;
        }
    } else {
        if (InterlockedCompareExchange(&ship_start_selected, 1, 0) != 0) {
            return;
        }
    }

    ZeroMemory(&event, sizeof(event));
    event.room = 1.0;
    original_go_to_room(&event);
    trace_client_state(is_return ? "ship_return_start" : "ship_start", 1);
}

static unsigned char __cdecl hooked_scene_ready(void) {
    unsigned char ready = original_scene_ready();
    trace_client_state("scene_ready", ready);
    return ready;
}

static unsigned char __cdecl hooked_ui_ready(void) {
    unsigned char ready = original_ui_ready();
    trace_client_state("ui_ready", ready);
    return ready;
}

#if defined(__GNUC__)
static unsigned char __attribute__((fastcall)) hooked_screen_event(void* target, void* ignored, unsigned int event_id) {
#else
static unsigned char __fastcall hooked_screen_event(void* target, void* ignored, unsigned int event_id) {
#endif
    unsigned char result;
    (void)ignored;
    trace_client_state("screen_event", event_id);
    if (event_id == 0x1003) {
        InterlockedExchange(&jwt_login_completed, 1);
        trace_client_state("jwt_login_scene_entered", 1);
    }
    result = original_screen_event(target, event_id);
    if (skip_cinematics_enabled && event_id == 0x1003) {
        InterlockedExchange(&ship_spline_skipped, 0);
        InterlockedExchange(&ship_navigation_ready, 0);
        InterlockedExchange(&ship_start_deferred, 0);
        if (InterlockedCompareExchange(&ship_start_selected, 0, 0) != 0) {
            InterlockedExchange(&ship_return_start_pending, 1);
        }
        InterlockedExchange(&ship_start_transition_skipped, 0);
        trace_client_state("ship_cinematic_reset", 1);
    }
    if (skip_cinematics_enabled && event_id == 0x1002) {
        if (InterlockedCompareExchange(&ship_navigation_ready, 1, 0) == 0) {
            ship_navigation_ready_time = GetTickCount();
            trace_client_state("ship_navigation_ready", 1);
        }
    }
    return result;
}

static uintptr_t trace_caller_rva(void* caller) {
    if (executable_module == NULL || caller == NULL) {
        return 0;
    }
    return (uintptr_t)((BYTE*)caller - (BYTE*)executable_module);
}

static uint64_t trace_digest(const char* buffer, int length) {
    uint64_t digest = 1469598103934665603ULL;
    int index;
    if (buffer == NULL || length <= 0) {
        return 0;
    }
    for (index = 0; index < length; index++) {
        digest ^= (unsigned char)buffer[index];
        digest *= 1099511628211ULL;
    }
    return digest;
}

static void trace_endpoint(const struct sockaddr* address, char* host, size_t host_length, unsigned short* port) {
    const struct sockaddr_in* ipv4;
    uint32_t value;
    if (host_length > 0) {
        host[0] = '\0';
    }
    *port = 0;
    if (address == NULL || address->sa_family != AF_INET) {
        return;
    }
    ipv4 = (const struct sockaddr_in*)address;
    value = ntohl(ipv4->sin_addr.s_addr);
    snprintf(host, host_length, "%u.%u.%u.%u",
        (unsigned int)((value >> 24) & 0xff),
        (unsigned int)((value >> 16) & 0xff),
        (unsigned int)((value >> 8) & 0xff),
        (unsigned int)(value & 0xff));
    *port = ntohs(ipv4->sin_port);
}

static void trace_socket_event(const char* kind, SOCKET socket, const struct sockaddr* address, uintptr_t caller_rva, int requested, int result, int error, uint64_t digest) {
    char line[768];
    char host[64];
    unsigned short port;
    DWORD written;
    int length;
    if (trace_file == INVALID_HANDLE_VALUE || !trace_lock_ready) {
        return;
    }
    trace_endpoint(address, host, sizeof(host), &port);
    length = snprintf(line, sizeof(line),
        "{\"time_ms\":%llu,\"protocol\":\"socket\",\"kind\":\"%s\",\"thread\":%lu,\"socket\":%llu,\"caller_rva\":\"0x%llx\",\"remote\":\"%s\",\"port\":%u,\"requested\":%d,\"result\":%d,\"error\":%d,\"digest\":\"%016llx\"}\r\n",
        (unsigned long long)GetTickCount(), kind, (unsigned long)GetCurrentThreadId(),
        (unsigned long long)(uintptr_t)socket, (unsigned long long)caller_rva,
        host, (unsigned int)port, requested, result, error, (unsigned long long)digest);
    if (length <= 0) {
        return;
    }
    if ((size_t)length > sizeof(line)) {
        length = (int)sizeof(line);
    }
    EnterCriticalSection(&trace_lock);
    WriteFile(trace_file, line, (DWORD)length, &written, NULL);
    LeaveCriticalSection(&trace_lock);
}

static int is_datagram_socket(SOCKET socket) {
    int socket_type = 0;
    int length = sizeof(socket_type);
    if (getsockopt(socket, SOL_SOCKET, SO_TYPE, (char*)&socket_type, &length) != 0) {
        return 0;
    }
    return socket_type == SOCK_DGRAM;
}

static void trace_raknet_payload(const char* direction, const char* kind, SOCKET socket,
    const struct sockaddr* address, const char* buffer, int length) {
    char header[640];
    char host[64];
    const char suffix[] = "\"}\r\n";
    unsigned short port;
    int header_length;
    int is_party;
    if (InterlockedCompareExchange(&snapshot_capture_enabled, 0, 0) == 0 ||
        buffer == NULL || length <= 0 || length > 65535 ||
        !is_datagram_socket(socket)) {
        return;
    }
    trace_endpoint(address, host, sizeof(host), &port);
    is_party = is_party_socket(socket) || port == redirect_party_port;
    if (port != redirect_port && port != redirect_party_port && !is_party) {
        return;
    }
    header_length = snprintf(header, sizeof(header),
        "{\"time_ms\":%llu,\"protocol\":\"client_raknet\","
        "\"kind\":\"datagram\",\"direction\":\"%s\",\"call\":\"%s\","
        "\"thread\":%lu,\"frame_sequence\":%lu,\"socket\":%llu,"
        "\"remote\":\"%s\",\"port\":%u,"
        "\"is_party\":%s,\"size\":%d,\"payload_hex\":\"",
        (unsigned long long)GetTickCount(), direction, kind,
        (unsigned long)GetCurrentThreadId(),
        (unsigned long)InterlockedCompareExchange(&snapshot_frame_sequence, 0, 0),
        (unsigned long long)(uintptr_t)socket, host, (unsigned int)port,
        is_party ? "true" : "false", length);
    if (header_length <= 0) {
        return;
    }
    trace_hex_line(
        header, header_length, (const BYTE*)buffer,
        (unsigned int)length, suffix);
}

static void trace_alert_event(LPCWSTR text, uintptr_t caller_rva) {
    char utf8[384];
    char escaped[512];
    char line[768];
    DWORD written;
    int utf8_length;
    size_t input_index;
    size_t output_index = 0;
    int line_length;
    if (trace_file == INVALID_HANDLE_VALUE || !trace_lock_ready) {
        return;
    }
    utf8_length = text == NULL ? 0 : WideCharToMultiByte(CP_UTF8, 0, text, -1, utf8, sizeof(utf8), NULL, NULL);
    if (utf8_length <= 0) {
        utf8[0] = '\0';
    }
    for (input_index = 0; utf8[input_index] != '\0' && output_index + 2 < sizeof(escaped); input_index++) {
        unsigned char value = (unsigned char)utf8[input_index];
        if (value == '"' || value == '\\') {
            escaped[output_index++] = '\\';
            escaped[output_index++] = (char)value;
        } else if (value >= 0x20) {
            escaped[output_index++] = (char)value;
        }
    }
    escaped[output_index] = '\0';
    line_length = snprintf(line, sizeof(line),
        "{\"time_ms\":%llu,\"protocol\":\"client\",\"kind\":\"alert\",\"thread\":%lu,\"caller_rva\":\"0x%llx\",\"message\":\"%s\"}\r\n",
        (unsigned long long)GetTickCount(), (unsigned long)GetCurrentThreadId(),
        (unsigned long long)caller_rva, escaped);
    if (line_length <= 0) {
        return;
    }
    EnterCriticalSection(&trace_lock);
    WriteFile(trace_file, line, (DWORD)line_length, &written, NULL);
    LeaveCriticalSection(&trace_lock);
}

static void trace_path_event(LPCWSTR path, uintptr_t caller_rva, DWORD result, DWORD error) {
    char utf8[512];
    char escaped[640];
    char line[896];
    DWORD written;
    int utf8_length;
    size_t input_index;
    size_t output_index = 0;
    int line_length;
    if (trace_file == INVALID_HANDLE_VALUE || !trace_lock_ready) {
        return;
    }
    utf8_length = path == NULL ? 0 : WideCharToMultiByte(CP_UTF8, 0, path, -1, utf8, sizeof(utf8), NULL, NULL);
    if (utf8_length <= 0) {
        utf8[0] = '\0';
    }
    for (input_index = 0; utf8[input_index] != '\0' && output_index + 2 < sizeof(escaped); input_index++) {
        unsigned char value = (unsigned char)utf8[input_index];
        if (value == '"' || value == '\\') {
            escaped[output_index++] = '\\';
            escaped[output_index++] = (char)value;
        } else if (value >= 0x20) {
            escaped[output_index++] = (char)value;
        }
    }
    escaped[output_index] = '\0';
    line_length = snprintf(line, sizeof(line),
        "{\"time_ms\":%llu,\"protocol\":\"client\",\"kind\":\"path_check\",\"thread\":%lu,\"caller_rva\":\"0x%llx\",\"path\":\"%s\",\"result\":%lu,\"error\":%lu}\r\n",
        (unsigned long long)GetTickCount(), (unsigned long)GetCurrentThreadId(),
        (unsigned long long)caller_rva, escaped, (unsigned long)result, (unsigned long)error);
    if (line_length <= 0) {
        return;
    }
    EnterCriticalSection(&trace_lock);
    WriteFile(trace_file, line, (DWORD)line_length, &written, NULL);
    LeaveCriticalSection(&trace_lock);
}

static void trace_internal_alert_event(const char* message, int code, int severity, uintptr_t caller_rva) {
    WCHAR wide[384];
    char utf8[512];
    char escaped[640];
    char line[896];
    DWORD written;
    int converted;
    int utf8_length;
    size_t input_index;
    size_t output_index = 0;
    int line_length;
    if (trace_file == INVALID_HANDLE_VALUE || !trace_lock_ready) {
        return;
    }
    converted = message == NULL ? 0 : MultiByteToWideChar(CP_ACP, 0, message, -1, wide, sizeof(wide) / sizeof(wide[0]));
    if (converted <= 0) {
        wide[0] = L'\0';
    }
    utf8_length = WideCharToMultiByte(CP_UTF8, 0, wide, -1, utf8, sizeof(utf8), NULL, NULL);
    if (utf8_length <= 0) {
        utf8[0] = '\0';
    }
    for (input_index = 0; utf8[input_index] != '\0' && output_index + 2 < sizeof(escaped); input_index++) {
        unsigned char value = (unsigned char)utf8[input_index];
        if (value == '"' || value == '\\') {
            escaped[output_index++] = '\\';
            escaped[output_index++] = (char)value;
        } else if (value >= 0x20) {
            escaped[output_index++] = (char)value;
        }
    }
    escaped[output_index] = '\0';
    line_length = snprintf(line, sizeof(line),
        "{\"time_ms\":%llu,\"protocol\":\"client\",\"kind\":\"internal_alert\",\"thread\":%lu,\"caller_rva\":\"0x%llx\",\"code\":%d,\"severity\":%d,\"message\":\"%s\"}\r\n",
        (unsigned long long)GetTickCount(), (unsigned long)GetCurrentThreadId(),
        (unsigned long long)caller_rva, code, severity, escaped);
    if (line_length <= 0) {
        return;
    }
    EnterCriticalSection(&trace_lock);
    WriteFile(trace_file, line, (DWORD)line_length, &written, NULL);
    LeaveCriticalSection(&trace_lock);
}

static void trace_login_error_event(unsigned int code, uintptr_t caller_rva) {
    char line[384];
    DWORD written;
    int line_length;
    if (trace_file == INVALID_HANDLE_VALUE || !trace_lock_ready) {
        return;
    }
    line_length = snprintf(line, sizeof(line),
        "{\"time_ms\":%llu,\"protocol\":\"client\",\"kind\":\"login_error\",\"thread\":%lu,\"caller_rva\":\"0x%llx\",\"code\":%u}\r\n",
        (unsigned long long)GetTickCount(), (unsigned long)GetCurrentThreadId(),
        (unsigned long long)caller_rva, code);
    if (line_length <= 0) {
        return;
    }
    EnterCriticalSection(&trace_lock);
    WriteFile(trace_file, line, (DWORD)line_length, &written, NULL);
    LeaveCriticalSection(&trace_lock);
}

static void peer_address(SOCKET socket, struct sockaddr_storage* storage, int* length) {
    *length = sizeof(*storage);
    memset(storage, 0, sizeof(*storage));
    if (getpeername(socket, (struct sockaddr*)storage, length) != 0) {
        *length = 0;
    }
}

static redirect_tls* get_redirect_tls(void) {
    redirect_tls* data;
    if (redirect_tls_index == TLS_OUT_OF_INDEXES) {
        return NULL;
    }
    data = (redirect_tls*)TlsGetValue(redirect_tls_index);
    if (data != NULL) {
        return data;
    }
    data = (redirect_tls*)HeapAlloc(GetProcessHeap(), HEAP_ZERO_MEMORY, sizeof(redirect_tls));
    if (data == NULL) {
        return NULL;
    }
    if (!TlsSetValue(redirect_tls_index, data)) {
        HeapFree(GetProcessHeap(), 0, data);
        return NULL;
    }
    return data;
}

static struct hostent* WSAAPI hooked_gethostbyname(const char* name) {
    struct hostent* resolved;
    redirect_tls* data;
    if (name == NULL) {
        return original_gethostbyname(name);
    }
    resolved = original_gethostbyname(redirect_hostname);
    if (resolved == NULL || resolved->h_addr_list == NULL || resolved->h_addr_list[0] == NULL) {
        return NULL;
    }
    data = get_redirect_tls();
    if (data == NULL) {
        return NULL;
    }
    memcpy(&data->address, resolved->h_addr_list[0], sizeof(data->address));
    data->addresses[0] = (char*)&data->address;
    data->addresses[1] = NULL;
    data->host.h_name = NULL;
    data->host.h_aliases = NULL;
    data->host.h_addrtype = AF_INET;
    data->host.h_length = sizeof(data->address);
    data->host.h_addr_list = data->addresses;
    return &data->host;
}

static int is_ipv4_port(const struct sockaddr* address, int length, unsigned short port) {
    const struct sockaddr_in* ipv4;
    if (address == NULL || length < (int)sizeof(struct sockaddr_in) ||
        address->sa_family != AF_INET) {
        return 0;
    }
    ipv4 = (const struct sockaddr_in*)address;
    return ipv4->sin_port == htons(port);
}

static int redirect_ipv4(struct in_addr* address) {
    struct hostent* resolved;
    if (address == NULL) {
        return 0;
    }
    resolved = original_gethostbyname(redirect_hostname);
    if (resolved == NULL || resolved->h_addr_list == NULL || resolved->h_addr_list[0] == NULL) {
        return 0;
    }
    memcpy(address, resolved->h_addr_list[0], sizeof(*address));
    return 1;
}

static void remember_party_socket(SOCKET socket) {
    unsigned int index;
    AcquireSRWLockExclusive(&party_socket_lock);
    for (index = 0; index < party_socket_count; index++) {
        if (party_sockets[index] == socket) {
            ReleaseSRWLockExclusive(&party_socket_lock);
            return;
        }
    }
    if (party_socket_count < PARTY_SOCKET_LIMIT) {
        party_sockets[party_socket_count] = socket;
        party_socket_count++;
    }
    ReleaseSRWLockExclusive(&party_socket_lock);
}

static int is_party_socket(SOCKET socket) {
    unsigned int index;
    int is_party = 0;
    AcquireSRWLockShared(&party_socket_lock);
    for (index = 0; index < party_socket_count; index++) {
        if (party_sockets[index] == socket) {
            is_party = 1;
            break;
        }
    }
    ReleaseSRWLockShared(&party_socket_lock);
    return is_party;
}

static void restore_party_source(SOCKET socket, struct sockaddr* source, int source_length);

static void forget_party_socket(SOCKET socket) {
    unsigned int index;
    AcquireSRWLockExclusive(&party_socket_lock);
    for (index = 0; index < party_socket_count; index++) {
        if (party_sockets[index] != socket) {
            continue;
        }
        party_socket_count--;
        party_sockets[index] = party_sockets[party_socket_count];
        break;
    }
    index = 0;
    while (index < party_receive_count) {
        if (party_receives[index].socket != socket) {
            index++;
            continue;
        }
        party_receive_count--;
        party_receives[index] = party_receives[party_receive_count];
    }
    ReleaseSRWLockExclusive(&party_socket_lock);
}

static void remember_party_receive(SOCKET socket, LPWSAOVERLAPPED overlapped,
    struct sockaddr* source, LPINT source_length) {
    unsigned int index;
    if (overlapped == NULL || source == NULL || source_length == NULL) {
        return;
    }
    AcquireSRWLockExclusive(&party_socket_lock);
    for (index = 0; index < party_receive_count; index++) {
        if (party_receives[index].socket == socket &&
            party_receives[index].overlapped == overlapped) {
            party_receives[index].source = source;
            party_receives[index].source_length = source_length;
            ReleaseSRWLockExclusive(&party_socket_lock);
            return;
        }
    }
    if (party_receive_count < PARTY_SOCKET_LIMIT) {
        party_receives[party_receive_count].socket = socket;
        party_receives[party_receive_count].overlapped = overlapped;
        party_receives[party_receive_count].source = source;
        party_receives[party_receive_count].source_length = source_length;
        party_receive_count++;
    }
    ReleaseSRWLockExclusive(&party_socket_lock);
}

static void complete_party_receive(SOCKET socket, LPWSAOVERLAPPED overlapped) {
    unsigned int index;
    struct sockaddr* source = NULL;
    LPINT source_length = NULL;
    AcquireSRWLockExclusive(&party_socket_lock);
    for (index = 0; index < party_receive_count; index++) {
        if (party_receives[index].socket != socket ||
            party_receives[index].overlapped != overlapped) {
            continue;
        }
        source = party_receives[index].source;
        source_length = party_receives[index].source_length;
        party_receive_count--;
        party_receives[index] = party_receives[party_receive_count];
        break;
    }
    ReleaseSRWLockExclusive(&party_socket_lock);
    if (source != NULL && source_length != NULL) {
        restore_party_source(socket, source, *source_length);
    }
}

static char* envelop_party_datagram(const char* buffer, int length, int* wrapped_length) {
    char* wrapped;
    size_t envelope_length = sizeof(party_envelope);
    if (buffer == NULL || length < 0 || length > INT_MAX - (int)envelope_length) {
        WSASetLastError(WSAEINVAL);
        return NULL;
    }
    *wrapped_length = length + (int)envelope_length;
    wrapped = (char*)HeapAlloc(GetProcessHeap(), 0, (SIZE_T)*wrapped_length);
    if (wrapped == NULL) {
        WSASetLastError(WSAENOBUFS);
        return NULL;
    }
    memcpy(wrapped, party_envelope, envelope_length);
    memcpy(wrapped + envelope_length, buffer, (size_t)length);
    return wrapped;
}

static int party_send_result(int result, int original_length) {
    if (result == SOCKET_ERROR) {
        return SOCKET_ERROR;
    }
    if (result == original_length + (int)sizeof(party_envelope)) {
        return original_length;
    }
    if (result <= (int)sizeof(party_envelope)) {
        return 0;
    }
    return result - (int)sizeof(party_envelope);
}

static void restore_party_source(SOCKET socket, struct sockaddr* source, int source_length) {
    struct sockaddr_in* ipv4;
    if (!is_party_socket(socket) ||
        !is_ipv4_port(source, source_length, redirect_port)) {
        return;
    }
    ipv4 = (struct sockaddr_in*)source;
    ipv4->sin_port = htons(redirect_party_port);
}

static int WSAAPI hooked_connect(SOCKET socket, const struct sockaddr* name, int name_length) {
    struct sockaddr_in redirected;
    const struct sockaddr* effective = name;
    int result;
    int error;
    int is_party_destination = 0;
    if (name != NULL && name_length >= (int)sizeof(struct sockaddr_in) && name->sa_family == AF_INET) {
        memcpy(&redirected, name, sizeof(redirected));
        if (redirected.sin_port == htons(80)) {
            redirected.sin_port = htons(redirect_port);
            redirect_ipv4(&redirected.sin_addr);
            effective = (const struct sockaddr*)&redirected;
        } else if (redirected.sin_port == htons(redirect_party_port)) {
            is_party_destination = 1;
            redirected.sin_port = htons(redirect_port);
            redirect_ipv4(&redirected.sin_addr);
            effective = (const struct sockaddr*)&redirected;
        } else if (redirected.sin_port == htons(redirect_port)) {
            redirect_ipv4(&redirected.sin_addr);
            effective = (const struct sockaddr*)&redirected;
        }
    }
    result = original_connect(socket, effective, name_length);
    error = result == SOCKET_ERROR ? WSAGetLastError() : 0;
    if (result != SOCKET_ERROR && is_party_destination) {
        remember_party_socket(socket);
    }
    trace_socket_event("connect", socket, effective, trace_caller_rva(__builtin_return_address(0)), name_length, result, error, 0);
    WSASetLastError(error);
    return result;
}

static int patch_matchmaking_string(BYTE* payload, int length,
    const char* key, char expected, char replacement) {
    size_t key_length = strlen(key);
    int patch_count = 0;
    int index;
    if (payload == NULL || key_length >= 63 || length <= 0) {
        return 0;
    }
    for (index = 0; index + (int)key_length + 5 <= length; index++) {
        BYTE* entry = payload + index;
        if (entry[0] != (BYTE)(key_length + 1) ||
            memcmp(entry + 1, key, key_length) != 0 ||
            entry[key_length + 1] != 0 ||
            entry[key_length + 2] != 2 ||
            entry[key_length + 3] != (BYTE)expected ||
            entry[key_length + 4] != 0) {
            continue;
        }
        entry[key_length + 3] = (BYTE)replacement;
        patch_count++;
    }
    return patch_count;
}

static int patch_solo_unranked_1v1_matchmaking(BYTE* buffer, int length) {
    int offset = 0;
    int patch_count = 0;
    if (buffer == NULL || length < 12 ||
        InterlockedCompareExchange(&solo_unranked_1v1_matchmaking_pending, 0, 0) == 0) {
        return 0;
    }
    while (offset + 12 <= length) {
        BYTE* frame = buffer + offset;
        int payload_length = (int)frame[0] << 8 | (int)frame[1];
        int frame_length = 12 + payload_length;
        int expected_count;
        int team_count;
        if (frame_length > length - offset) {
            break;
        }
        if (frame[2] != 0 || frame[3] != 4 ||
            frame[4] != 0 || frame[5] != 13 || (frame[8] >> 4) != 0) {
            offset += frame_length;
            continue;
        }
        expected_count = patch_matchmaking_string(
            frame + 12, payload_length, "ExpectedPlayerCount", '4', '4');
        team_count = patch_matchmaking_string(
            frame + 12, payload_length, "TeamSize", '2', '2');
        if (expected_count == 0 || team_count == 0) {
            offset += frame_length;
            continue;
        }
        patch_count += patch_matchmaking_string(
            frame + 12, payload_length, "ExpectedPlayerCount", '4', '2');
        patch_count += patch_matchmaking_string(
            frame + 12, payload_length, "TeamSize", '2', '1');
        offset += frame_length;
    }
    if (patch_count != 0) {
        trace_client_state("solo_unranked_1v1_matchmaking", (unsigned int)patch_count);
    }
    return patch_count;
}

static int WSAAPI hooked_send(SOCKET socket, const char* buffer, int length, int flags) {
    struct sockaddr_storage remote;
    int remote_length;
    int wrapped_length = 0;
    char* wrapped = NULL;
    char* matchmaking = NULL;
    const char* effective = buffer;
    int result;
    if (!is_party_socket(socket) && buffer != NULL && length > 0 &&
        InterlockedCompareExchange(&solo_unranked_1v1_matchmaking_pending, 0, 0) != 0) {
        matchmaking = (char*)HeapAlloc(GetProcessHeap(), 0, (SIZE_T)length);
        if (matchmaking != NULL) {
            memcpy(matchmaking, buffer, (size_t)length);
            if (patch_solo_unranked_1v1_matchmaking((BYTE*)matchmaking, length) != 0) {
                effective = matchmaking;
            }
        }
    }
    if (is_party_socket(socket)) {
        wrapped = envelop_party_datagram(buffer, length, &wrapped_length);
        if (wrapped == NULL) {
            return SOCKET_ERROR;
        }
        result = original_send(socket, wrapped, wrapped_length, flags);
    } else {
        result = original_send(socket, effective, length, flags);
    }
    int error = result == SOCKET_ERROR ? WSAGetLastError() : 0;
    if (matchmaking != NULL) {
        HeapFree(GetProcessHeap(), 0, matchmaking);
    }
    if (wrapped != NULL) {
        HeapFree(GetProcessHeap(), 0, wrapped);
        result = party_send_result(result, length);
    }
    peer_address(socket, &remote, &remote_length);
    trace_socket_event("send", socket, remote_length > 0 ? (struct sockaddr*)&remote : NULL, trace_caller_rva(__builtin_return_address(0)), length, result, error, trace_digest(buffer, result));
    trace_raknet_payload("client_to_server", "send", socket,
        remote_length > 0 ? (struct sockaddr*)&remote : NULL, buffer, result);
    WSASetLastError(error);
    return result;
}

static int WSAAPI hooked_recv(SOCKET socket, char* buffer, int length, int flags) {
    struct sockaddr_storage remote;
    int remote_length;
    int result = original_recv(socket, buffer, length, flags);
    int error = result == SOCKET_ERROR ? WSAGetLastError() : 0;
    peer_address(socket, &remote, &remote_length);
    if (result != 1 && error != WSAEWOULDBLOCK) {
        trace_socket_event("recv", socket, remote_length > 0 ? (struct sockaddr*)&remote : NULL, trace_caller_rva(__builtin_return_address(0)), length, result, error, trace_digest(buffer, result));
        trace_raknet_payload("server_to_client", "recv", socket,
            remote_length > 0 ? (struct sockaddr*)&remote : NULL, buffer, result);
    }
    WSASetLastError(error);
    return result;
}

static int WSAAPI hooked_sendto(SOCKET socket, const char* buffer, int length, int flags, const struct sockaddr* destination, int destination_length) {
    struct sockaddr_in redirected;
    const struct sockaddr* effective = destination;
    int wrapped_length = 0;
    char* wrapped = NULL;
    int result;
    if (is_ipv4_port(destination, destination_length, redirect_party_port)) {
        remember_party_socket(socket);
        memcpy(&redirected, destination, sizeof(redirected));
        redirected.sin_port = htons(redirect_port);
        redirect_ipv4(&redirected.sin_addr);
        effective = (const struct sockaddr*)&redirected;
        wrapped = envelop_party_datagram(buffer, length, &wrapped_length);
        if (wrapped == NULL) {
            return SOCKET_ERROR;
        }
        result = original_sendto(socket, wrapped, wrapped_length, flags, effective, destination_length);
    } else if (is_ipv4_port(destination, destination_length, redirect_port)) {
        memcpy(&redirected, destination, sizeof(redirected));
        redirect_ipv4(&redirected.sin_addr);
        effective = (const struct sockaddr*)&redirected;
        result = original_sendto(socket, buffer, length, flags, effective, destination_length);
    } else {
        result = original_sendto(socket, buffer, length, flags, destination, destination_length);
    }
    int error = result == SOCKET_ERROR ? WSAGetLastError() : 0;
    if (wrapped != NULL) {
        HeapFree(GetProcessHeap(), 0, wrapped);
        result = party_send_result(result, length);
    }
    trace_socket_event("sendto", socket, effective, trace_caller_rva(__builtin_return_address(0)), length, result, error, trace_digest(buffer, result));
    trace_raknet_payload(
        "client_to_server", "sendto", socket, effective, buffer, result);
    WSASetLastError(error);
    return result;
}

static int WSAAPI hooked_recvfrom(SOCKET socket, char* buffer, int length, int flags, struct sockaddr* source, int* source_length) {
    int result = original_recvfrom(socket, buffer, length, flags, source, source_length);
    int error = result == SOCKET_ERROR ? WSAGetLastError() : 0;
    if (result != SOCKET_ERROR && source_length != NULL) {
        restore_party_source(socket, source, *source_length);
    }
    trace_socket_event("recvfrom", socket, source, trace_caller_rva(__builtin_return_address(0)), length, result, error, trace_digest(buffer, result));
    trace_raknet_payload(
        "server_to_client", "recvfrom", socket, source, buffer, result);
    WSASetLastError(error);
    return result;
}

static int WSAAPI hooked_closesocket(SOCKET socket) {
    int result;
    forget_party_socket(socket);
    result = original_closesocket(socket);
    return result;
}

static DWORD wsabuf_length(LPWSABUF buffers, DWORD count) {
    DWORD total = 0;
    DWORD index;
    for (index = 0; index < count; index++) {
        total += buffers[index].len;
    }
    return total;
}

static void trace_wsabuf_payload(const char* direction, const char* kind,
    SOCKET socket, const struct sockaddr* address, LPWSABUF buffers,
    DWORD count, DWORD completed) {
    BYTE* payload;
    DWORD copied = 0;
    DWORD index;
    if (buffers == NULL || count == 0 || completed == 0 || completed > 65535) {
        return;
    }
    payload = (BYTE*)HeapAlloc(GetProcessHeap(), 0, (SIZE_T)completed);
    if (payload == NULL) {
        return;
    }
    for (index = 0; index < count && copied < completed; index++) {
        DWORD remaining = completed - copied;
        DWORD copy_length = buffers[index].len < remaining ? buffers[index].len : remaining;
        if (copy_length > 0 && buffers[index].buf != NULL) {
            memcpy(payload + copied, buffers[index].buf, copy_length);
            copied += copy_length;
        }
    }
    if (copied == completed) {
        trace_raknet_payload(
            direction, kind, socket, address, (const char*)payload, (int)completed);
    }
    HeapFree(GetProcessHeap(), 0, payload);
}

static int WSAAPI hooked_wsasend(SOCKET socket, LPWSABUF buffers, DWORD count, LPDWORD sent, DWORD flags, LPWSAOVERLAPPED overlapped, LPWSAOVERLAPPED_COMPLETION_ROUTINE completion) {
    struct sockaddr_storage remote;
    int remote_length;
    DWORD total = wsabuf_length(buffers, count);
    BYTE* matchmaking = NULL;
    DWORD offset = 0;
    DWORD index;
    if (!is_party_socket(socket) && buffers != NULL && total > 0 &&
        InterlockedCompareExchange(&solo_unranked_1v1_matchmaking_pending, 0, 0) != 0) {
        matchmaking = (BYTE*)HeapAlloc(GetProcessHeap(), 0, (SIZE_T)total);
        if (matchmaking != NULL) {
            for (index = 0; index < count; index++) {
                memcpy(matchmaking + offset, buffers[index].buf, buffers[index].len);
                offset += buffers[index].len;
            }
            if (patch_solo_unranked_1v1_matchmaking(matchmaking, (int)total) != 0) {
                offset = 0;
                for (index = 0; index < count; index++) {
                    memcpy(buffers[index].buf, matchmaking + offset, buffers[index].len);
                    offset += buffers[index].len;
                }
            }
            HeapFree(GetProcessHeap(), 0, matchmaking);
        }
    }
    int result = original_wsasend(socket, buffers, count, sent, flags, overlapped, completion);
    int error = result == SOCKET_ERROR ? WSAGetLastError() : 0;
    int completed = result == 0 && sent != NULL ? (int)*sent : result;
    peer_address(socket, &remote, &remote_length);
    trace_socket_event("WSASend", socket, remote_length > 0 ? (struct sockaddr*)&remote : NULL, trace_caller_rva(__builtin_return_address(0)), (int)total, completed, error, 0);
    if (completed > 0) {
        trace_wsabuf_payload(
            "client_to_server", "WSASend", socket,
            remote_length > 0 ? (struct sockaddr*)&remote : NULL,
            buffers, count, (DWORD)completed);
    }
    WSASetLastError(error);
    return result;
}

static int WSAAPI hooked_wsarecv(SOCKET socket, LPWSABUF buffers, DWORD count, LPDWORD received, LPDWORD flags, LPWSAOVERLAPPED overlapped, LPWSAOVERLAPPED_COMPLETION_ROUTINE completion) {
    struct sockaddr_storage remote;
    int remote_length;
    int result = original_wsarecv(socket, buffers, count, received, flags, overlapped, completion);
    int error = result == SOCKET_ERROR ? WSAGetLastError() : 0;
    int completed = result == 0 && received != NULL ? (int)*received : result;
    peer_address(socket, &remote, &remote_length);
    trace_socket_event("WSARecv", socket, remote_length > 0 ? (struct sockaddr*)&remote : NULL, trace_caller_rva(__builtin_return_address(0)), (int)wsabuf_length(buffers, count), completed, error, 0);
    if (completed > 0) {
        trace_wsabuf_payload(
            "server_to_client", "WSARecv", socket,
            remote_length > 0 ? (struct sockaddr*)&remote : NULL,
            buffers, count, (DWORD)completed);
    }
    WSASetLastError(error);
    return result;
}

static int WSAAPI hooked_wsarecvfrom(SOCKET socket, LPWSABUF buffers, DWORD count, LPDWORD received, LPDWORD flags, struct sockaddr* source, LPINT source_length, LPWSAOVERLAPPED overlapped, LPWSAOVERLAPPED_COMPLETION_ROUTINE completion) {
    int result = original_wsarecvfrom(socket, buffers, count, received, flags, source, source_length, overlapped, completion);
    int error = result == SOCKET_ERROR ? WSAGetLastError() : 0;
    int completed = result == 0 && received != NULL ? (int)*received : result;
    if (result == 0 && source_length != NULL) {
        restore_party_source(socket, source, *source_length);
    } else if (result == SOCKET_ERROR && error == WSA_IO_PENDING) {
        remember_party_receive(socket, overlapped, source, source_length);
    }
    trace_socket_event("WSARecvFrom", socket, source, trace_caller_rva(__builtin_return_address(0)), (int)wsabuf_length(buffers, count), completed, error, 0);
    if (completed > 0) {
        trace_wsabuf_payload(
            "server_to_client", "WSARecvFrom", socket, source,
            buffers, count, (DWORD)completed);
    }
    WSASetLastError(error);
    return result;
}

static BOOL WSAAPI hooked_wsagetoverlappedresult(SOCKET socket,
    LPWSAOVERLAPPED overlapped, LPDWORD transferred, BOOL wait, LPDWORD flags) {
    BOOL result = original_wsagetoverlappedresult(socket, overlapped, transferred, wait, flags);
    int error = result ? 0 : WSAGetLastError();
    if (result) {
        complete_party_receive(socket, overlapped);
    }
    WSASetLastError(error);
    return result;
}

static int WINAPI hooked_messageboxw(HWND window, LPCWSTR text, LPCWSTR caption, UINT type) {
    trace_alert_event(text, trace_caller_rva(__builtin_return_address(0)));
    if (InterlockedExchange(&chat_exit_pending, 0) != 0) {
        unsigned int buttons = type & MB_TYPEMASK;
        trace_client_state("chat_exit_confirm", buttons);
        if (buttons == MB_YESNO || buttons == MB_YESNOCANCEL) {
            return IDYES;
        }
        return IDOK;
    }
    return original_messageboxw(window, text, caption, type);
}

static int WINAPI hooked_messageboxa(HWND window, LPCSTR text, LPCSTR caption, UINT type) {
    WCHAR wide[384];
    int converted = text == NULL ? 0 : MultiByteToWideChar(CP_ACP, 0, text, -1, wide, sizeof(wide) / sizeof(wide[0]));
    if (converted <= 0) {
        wide[0] = L'\0';
    }
    trace_alert_event(wide, trace_caller_rva(__builtin_return_address(0)));
    if (InterlockedExchange(&chat_exit_pending, 0) != 0) {
        unsigned int buttons = type & MB_TYPEMASK;
        trace_client_state("chat_exit_confirm", buttons);
        if (buttons == MB_YESNO || buttons == MB_YESNOCANCEL) {
            return IDYES;
        }
        return IDOK;
    }
    return original_messageboxa(window, text, caption, type);
}

static DWORD WINAPI hooked_getfileattributesw(LPCWSTR file_name) {
    uintptr_t caller_rva = trace_caller_rva(__builtin_return_address(0));
    DWORD result = original_getfileattributesw(file_name);
    DWORD error = result == INVALID_FILE_ATTRIBUTES ? GetLastError() : 0;
    if (caller_rva == 0xAF2E85) {
        trace_path_event(file_name, caller_rva, result, error);
    }
    SetLastError(error);
    return result;
}

static void __cdecl hooked_internal_alert(const char* message, int code, int severity) {
    trace_internal_alert_event(message, code, severity, trace_caller_rva(__builtin_return_address(0)));
    original_alert(message, code, severity);
}

static void __cdecl hooked_login_error(unsigned int code, void* target) {
    InterlockedExchange(&jwt_login_completed, 1);
    trace_login_error_event(code, trace_caller_rva(__builtin_return_address(0)));
    original_login_error(code, target);
}

static void hooked_ssl_ctx_set_verify(void* context, int mode, void* callback) {
    (void)context;
    (void)mode;
    (void)callback;
}

static long hooked_ssl_get_verify_result(const void* ssl) {
    (void)ssl;
    return 0;
}

static int hooked_wildcard_match_no_case(const char* first, const char* second) {
    (void)first;
    (void)second;
    return 0;
}

static int hooked_verify_certificate(void* certificate, int self_signed) {
    (void)certificate;
    (void)self_signed;
    return 0;
}

static int patch_import(HMODULE module, const char* library_name, const char* function_name, WORD function_ordinal, void* replacement) {
    BYTE* base = (BYTE*)module;
    IMAGE_DOS_HEADER* dos = (IMAGE_DOS_HEADER*)base;
    IMAGE_NT_HEADERS* nt;
    IMAGE_IMPORT_DESCRIPTOR* descriptor;
    int patched = 0;
    if (function_ordinal == 52) {
        dns_diagnostics |= 0x1000;
    }
    if (dos == NULL || dos->e_magic != IMAGE_DOS_SIGNATURE) {
        return 0;
    }
    nt = (IMAGE_NT_HEADERS*)(base + dos->e_lfanew);
    if (nt->Signature != IMAGE_NT_SIGNATURE) {
        return 0;
    }
    if (nt->OptionalHeader.DataDirectory[IMAGE_DIRECTORY_ENTRY_IMPORT].VirtualAddress == 0) {
        return 0;
    }
    descriptor = (IMAGE_IMPORT_DESCRIPTOR*)(base + nt->OptionalHeader.DataDirectory[IMAGE_DIRECTORY_ENTRY_IMPORT].VirtualAddress);
    for (; descriptor->Name != 0; descriptor++) {
        IMAGE_THUNK_DATA* names;
        IMAGE_THUNK_DATA* addresses;
        size_t index;
        if (lstrcmpiA((const char*)(base + descriptor->Name), library_name) != 0) {
            continue;
        }
        if (function_ordinal == 52) {
            dns_diagnostics |= 0x2000;
        }
        if (descriptor->OriginalFirstThunk == 0 || descriptor->FirstThunk == 0) {
            continue;
        }
        names = (IMAGE_THUNK_DATA*)(base + descriptor->OriginalFirstThunk);
        addresses = (IMAGE_THUNK_DATA*)(base + descriptor->FirstThunk);
        for (index = 0; names[index].u1.AddressOfData != 0; index++) {
            IMAGE_IMPORT_BY_NAME* imported_name;
            DWORD old_protection;
            DWORD ignored;
            if (IMAGE_SNAP_BY_ORDINAL(names[index].u1.Ordinal)) {
                if (IMAGE_ORDINAL(names[index].u1.Ordinal) != function_ordinal) {
                    continue;
                }
            } else {
                imported_name = (IMAGE_IMPORT_BY_NAME*)(base + names[index].u1.AddressOfData);
                if (strcmp((const char*)imported_name->Name, function_name) != 0) {
                    continue;
                }
            }
            if (function_ordinal == 52) {
                dns_diagnostics |= 0x4000;
            }
            if (!VirtualProtect(&addresses[index].u1.Function, sizeof(ULONG_PTR), PAGE_READWRITE, &old_protection)) {
                if (function_ordinal == 52) {
                    dns_diagnostics |= 0x8000;
                }
                continue;
            }
            addresses[index].u1.Function = (ULONG_PTR)replacement;
            VirtualProtect(&addresses[index].u1.Function, sizeof(ULONG_PTR), old_protection, &ignored);
            patched++;
        }
    }
    return patched;
}

static int patch_import_address(HMODULE module, void* original, void* replacement) {
    BYTE* base = (BYTE*)module;
    IMAGE_DOS_HEADER* dos = (IMAGE_DOS_HEADER*)base;
    IMAGE_NT_HEADERS* nt;
    IMAGE_IMPORT_DESCRIPTOR* descriptor;
    int patched = 0;
    if (original == (void*)original_gethostbyname) {
        dns_diagnostics |= 0x10000;
    }
    if (dos == NULL || dos->e_magic != IMAGE_DOS_SIGNATURE) {
        return 0;
    }
    nt = (IMAGE_NT_HEADERS*)(base + dos->e_lfanew);
    if (nt->Signature != IMAGE_NT_SIGNATURE) {
        return 0;
    }
    if (nt->OptionalHeader.DataDirectory[IMAGE_DIRECTORY_ENTRY_IMPORT].VirtualAddress == 0) {
        return 0;
    }
    descriptor = (IMAGE_IMPORT_DESCRIPTOR*)(base + nt->OptionalHeader.DataDirectory[IMAGE_DIRECTORY_ENTRY_IMPORT].VirtualAddress);
    for (; descriptor->Name != 0; descriptor++) {
        IMAGE_THUNK_DATA* addresses;
        size_t index;
        if (descriptor->FirstThunk == 0) {
            continue;
        }
        addresses = (IMAGE_THUNK_DATA*)(base + descriptor->FirstThunk);
        for (index = 0; addresses[index].u1.Function != 0; index++) {
            DWORD old_protection;
            DWORD ignored;
            if (addresses[index].u1.Function != (ULONG_PTR)original) {
                continue;
            }
            if (original == (void*)original_gethostbyname) {
                dns_diagnostics |= 0x20000;
            }
            if (!VirtualProtect(&addresses[index].u1.Function, sizeof(ULONG_PTR), PAGE_READWRITE, &old_protection)) {
                if (original == (void*)original_gethostbyname) {
                    dns_diagnostics |= 0x40000;
                }
                continue;
            }
            addresses[index].u1.Function = (ULONG_PTR)replacement;
            VirtualProtect(&addresses[index].u1.Function, sizeof(ULONG_PTR), old_protection, &ignored);
            patched++;
        }
    }
    return patched;
}

static BYTE* find_signature(HMODULE module, const BYTE* signature, size_t signature_length) {
    BYTE* base = (BYTE*)module;
    IMAGE_DOS_HEADER* dos = (IMAGE_DOS_HEADER*)base;
    IMAGE_NT_HEADERS* nt;
    IMAGE_SECTION_HEADER* section;
    WORD section_index;
    if (base == NULL || dos->e_magic != IMAGE_DOS_SIGNATURE) {
        return NULL;
    }
    nt = (IMAGE_NT_HEADERS*)(base + dos->e_lfanew);
    if (nt->Signature != IMAGE_NT_SIGNATURE) {
        return NULL;
    }
    section = IMAGE_FIRST_SECTION(nt);
    for (section_index = 0; section_index < nt->FileHeader.NumberOfSections; section_index++, section++) {
        BYTE* start;
        size_t size;
        size_t index;
        if ((section->Characteristics & IMAGE_SCN_MEM_READ) == 0) {
            continue;
        }
        size = section->Misc.VirtualSize;
        if (size < section->SizeOfRawData) {
            size = section->SizeOfRawData;
        }
        if (size < signature_length || section->VirtualAddress >= nt->OptionalHeader.SizeOfImage) {
            continue;
        }
        if (size > nt->OptionalHeader.SizeOfImage - section->VirtualAddress) {
            size = nt->OptionalHeader.SizeOfImage - section->VirtualAddress;
        }
        start = base + section->VirtualAddress;
        for (index = 0; index <= size - signature_length; index++) {
            if (memcmp(start + index, signature, signature_length) == 0) {
                return start + index;
            }
        }
    }
    return NULL;
}

static int patch_jump(void* target, void* replacement) {
    BYTE patch[5];
    int32_t relative;
    DWORD old_protection;
    DWORD ignored;
    if (target == NULL || replacement == NULL) {
        return 0;
    }
    relative = (int32_t)((BYTE*)replacement - ((BYTE*)target + sizeof(patch)));
    patch[0] = 0xE9;
    memcpy(patch + 1, &relative, sizeof(relative));
    if (!VirtualProtect(target, sizeof(patch), PAGE_EXECUTE_READWRITE, &old_protection)) {
        return 0;
    }
    memcpy(target, patch, sizeof(patch));
    FlushInstructionCache(GetCurrentProcess(), target, sizeof(patch));
    VirtualProtect(target, sizeof(patch), old_protection, &ignored);
    return 1;
}

static int patch_call(void* instruction, void* expected_target, void* replacement) {
    BYTE* call = (BYTE*)instruction;
    DWORD old_protection;
    DWORD ignored;
    int32_t current_relative;
    int32_t replacement_relative;
    if (call == NULL || call[0] != 0xE8) {
        return 0;
    }
    memcpy(&current_relative, call + 1, sizeof(current_relative));
    if (call + 5 + current_relative != (BYTE*)expected_target) {
        return 0;
    }
    replacement_relative = (int32_t)((BYTE*)replacement - (call + 5));
    if (!VirtualProtect(call, 5, PAGE_EXECUTE_READWRITE, &old_protection)) {
        return 0;
    }
    memcpy(call + 1, &replacement_relative, sizeof(replacement_relative));
    FlushInstructionCache(GetCurrentProcess(), call, 5);
    VirtualProtect(call, 5, old_protection, &ignored);
    return 1;
}

static int patch_bytes(void* target, const BYTE* replacement, size_t length) {
    DWORD old_protection;
    DWORD ignored;
    if (target == NULL || replacement == NULL || length == 0) {
        return 0;
    }
    if (!VirtualProtect(target, length, PAGE_EXECUTE_READWRITE, &old_protection)) {
        return 0;
    }
    memcpy(target, replacement, length);
    FlushInstructionCache(GetCurrentProcess(), target, length);
    VirtualProtect(target, length, old_protection, &ignored);
    return 1;
}

static int is_locale_letter(wchar_t character) {
    return (character >= L'a' && character <= L'z') ||
        (character >= L'A' && character <= L'Z');
}

static int has_client_locale_argument(void) {
    const wchar_t* command_line = GetCommandLineW();
    const wchar_t* cursor;
    if (command_line == NULL) {
        return 0;
    }
    for (cursor = command_line; *cursor != L'\0'; cursor++) {
        const wchar_t* locale;
        if (cursor != command_line && cursor[-1] != L' ' && cursor[-1] != L'\t') {
            continue;
        }
        if (_wcsnicmp(cursor, L"-locale:", 8) == 0) {
            locale = cursor + 8;
        } else if (_wcsnicmp(cursor, L"--locale:", 9) == 0) {
            locale = cursor + 9;
        } else {
            continue;
        }
        if (is_locale_letter(locale[0]) && is_locale_letter(locale[1]) &&
            locale[2] == L'-' && is_locale_letter(locale[3]) &&
            is_locale_letter(locale[4]) &&
            (locale[5] == L'\0' || locale[5] == L' ' || locale[5] == L'\t')) {
            return 1;
        }
    }
    return 0;
}

static int patch_locale_argument_precedence(HMODULE executable) {
    static const BYTE registry_branch_signature[] = {
        0xE8, 0xCE, 0x11, 0xFC, 0xFF, 0x83, 0xC4, 0x40,
        0x84, 0xC0, 0x74, 0x40, 0x8B, 0x4C, 0x24, 0x0C,
    };
    static const BYTE unconditional_jump[] = {0xEB};
    BYTE* registry_branch;
    if (!has_client_locale_argument()) {
        return 1;
    }
    registry_branch = find_signature(
        executable, registry_branch_signature, sizeof(registry_branch_signature));
    if (registry_branch == NULL) {
        return 0;
    }
    return patch_bytes(registry_branch + 10, unconditional_jump,
        sizeof(unconditional_jump));
}

static int FANG_THISCALL hooked_scaleform_stream_read(void* stream,
    void* destination, int length) {
    /*
     * MapRoomUI.OnUnrankedClicked and UpdatePvPButtonsForLeader both
     * hard-disable PVPMode2V1. Replace those two literal true operands with
     * false. The selected mode and the packaged 2v2/custom party-size gates
     * remain authoritative.
     */
    static const BYTE update_signature[] = {
        0x96, 0x02, 0x00, 0x08, 0xEA, 0x4E, 0x66, 0x9D,
        0x02, 0x00, 0x3A, 0x00, 0x96, 0x04, 0x00, 0x04,
        0x00, 0x08, 0x01, 0x1C, 0x96, 0x02, 0x00, 0x08,
        0x03, 0x4E, 0x96, 0x03, 0x00, 0x09, 0x46, 0x01,
        0x4E, 0x66, 0x9D, 0x02, 0x00, 0x1F, 0x00,
    };
    static const BYTE select_signature[] = {
        0x96, 0x06, 0x00, 0x04, 0x01, 0x08, 0x0E, 0x08,
        0x01, 0x1C, 0x96, 0x02, 0x00, 0x08, 0x03, 0x4E,
        0x96, 0x02, 0x00, 0x08, 0xEA, 0x4E, 0x4F,
    };
    static const BYTE disabled_assignment[] = {
        0x96, 0x04, 0x00, 0x08, 0x71, 0x05, 0x01, 0x4F,
    };
    enum {
        update_assignment_offset = 0x54,
        update_boolean_offset = update_assignment_offset + 6,
        update_patch_span = update_boolean_offset + 1,
        select_assignment_offset = 0x25,
        select_boolean_offset = select_assignment_offset + 6,
        select_patch_span = select_boolean_offset + 1,
    };
    BYTE* payload = (BYTE*)destination;
    int bytes_read = original_scaleform_stream_read(stream, destination, length);
    int patch_count = 0;
    int select_patch_count = 0;
    int update_patch_count = 0;
    int index;
    if (payload == NULL) {
        return bytes_read;
    }
    for (index = 0; index + select_patch_span <= bytes_read; index++) {
        if (payload[index] != select_signature[0] ||
            memcmp(payload + index, select_signature,
                sizeof(select_signature)) != 0 ||
            memcmp(payload + index + select_assignment_offset,
                disabled_assignment, sizeof(disabled_assignment)) != 0) {
            continue;
        }
        payload[index + select_boolean_offset] = 0x00;
        patch_count++;
        select_patch_count++;
    }
    for (index = 0; index + update_patch_span <= bytes_read; index++) {
        if (payload[index] != update_signature[0] ||
            memcmp(payload + index, update_signature,
                sizeof(update_signature)) != 0 ||
            memcmp(payload + index + update_assignment_offset,
                disabled_assignment, sizeof(disabled_assignment)) != 0) {
            continue;
        }
        payload[index + update_boolean_offset] = 0x00;
        patch_count++;
        update_patch_count++;
    }
    if (select_patch_count != 0) {
        trace_client_state("solo_unranked_1v1_select_hook",
            (unsigned int)select_patch_count);
    }
    if (update_patch_count != 0) {
        trace_client_state("solo_unranked_1v1_state_hook",
            (unsigned int)update_patch_count);
    }
    if (patch_count != 0 &&
        InterlockedCompareExchange(&solo_unranked_1v1_patched, 1, 0) == 0) {
        trace_client_state("solo_unranked_1v1_hook", (unsigned int)patch_count);
    }
    return bytes_read;
}

#if defined(__GNUC__) && defined(__i386__)
static void __attribute__((naked)) hooked_map_room_start_game(void) {
    __asm__ __volatile__(
        "pushfl\n\t"
        "pushal\n\t"
        "movl 48(%esp), %eax\n\t"
        "testl %eax, %eax\n\t"
        "jz 1f\n\t"
        "cmpl $0, 8(%eax)\n\t"
        "jne 1f\n\t"
        "cmpl $0x40080000, 12(%eax)\n\t"
        "jne 1f\n\t"
        "cmpl $0, 24(%eax)\n\t"
        "jne 1f\n\t"
        "cmpl $0, 28(%eax)\n\t"
        "jne 1f\n\t"
        "cmpl $0, 40(%eax)\n\t"
        "jne 1f\n\t"
        "cmpl $0x3ff00000, 44(%eax)\n\t"
        "jne 1f\n\t"
        "movl $1, _solo_unranked_1v1_matchmaking_pending\n\t"
        "jmp 2f\n\t"
        "1:\n\t"
        "movl $0, _solo_unranked_1v1_matchmaking_pending\n\t"
        "2:\n\t"
        "popal\n\t"
        "popfl\n\t"
        "jmp *_original_map_room_start_game\n\t");
}
#endif

static int patch_pointer(void** target, void* expected, void* replacement) {
    DWORD old_protection;
    DWORD ignored;
    if (target == NULL || replacement == NULL || *target != expected) {
        return 0;
    }
    if (!VirtualProtect(target, sizeof(void*), PAGE_READWRITE, &old_protection)) {
        return 0;
    }
    *target = replacement;
    VirtualProtect(target, sizeof(void*), old_protection, &ignored);
    return 1;
}

static int patch_network_imports(void) {
    HMODULE modules[2];
    int module_count = 1;
    int dns_patches = 0;
    int connect_patches = 0;
    int trace_patches = 0;
    modules[0] = GetModuleHandleA(NULL);
    modules[1] = GetModuleHandleA("EAWebKit.dll");
    if (modules[1] != NULL && modules[1] != modules[0]) {
        module_count = 2;
    }
    for (int index = 0; index < module_count; index++) {
        dns_patches += patch_import(modules[index], "WS2_32.dll", "gethostbyname", 52, (void*)hooked_gethostbyname);
        connect_patches += patch_import(modules[index], "WS2_32.dll", "connect", 4, (void*)hooked_connect);
        dns_patches += patch_import_address(modules[index], (void*)original_gethostbyname, (void*)hooked_gethostbyname);
        connect_patches += patch_import_address(modules[index], (void*)original_connect, (void*)hooked_connect);
        trace_patches += patch_import(modules[index], "WS2_32.dll", "send", 19, (void*)hooked_send);
        trace_patches += patch_import(modules[index], "WS2_32.dll", "recv", 16, (void*)hooked_recv);
        trace_patches += patch_import(modules[index], "WS2_32.dll", "sendto", 20, (void*)hooked_sendto);
        trace_patches += patch_import(modules[index], "WS2_32.dll", "recvfrom", 17, (void*)hooked_recvfrom);
        trace_patches += patch_import(modules[index], "WS2_32.dll", "closesocket", 3, (void*)hooked_closesocket);
        trace_patches += patch_import(modules[index], "WS2_32.dll", "WSASend", 0, (void*)hooked_wsasend);
        trace_patches += patch_import(modules[index], "WS2_32.dll", "WSARecv", 0, (void*)hooked_wsarecv);
        trace_patches += patch_import(modules[index], "WS2_32.dll", "WSARecvFrom", 0, (void*)hooked_wsarecvfrom);
        trace_patches += patch_import(modules[index], "WS2_32.dll", "WSAGetOverlappedResult", 0, (void*)hooked_wsagetoverlappedresult);
        trace_patches += patch_import_address(modules[index], (void*)original_send, (void*)hooked_send);
        trace_patches += patch_import_address(modules[index], (void*)original_recv, (void*)hooked_recv);
        trace_patches += patch_import_address(modules[index], (void*)original_sendto, (void*)hooked_sendto);
        trace_patches += patch_import_address(modules[index], (void*)original_recvfrom, (void*)hooked_recvfrom);
        trace_patches += patch_import_address(modules[index], (void*)original_closesocket, (void*)hooked_closesocket);
        trace_patches += patch_import_address(modules[index], (void*)original_wsasend, (void*)hooked_wsasend);
        trace_patches += patch_import_address(modules[index], (void*)original_wsarecv, (void*)hooked_wsarecv);
        trace_patches += patch_import_address(modules[index], (void*)original_wsarecvfrom, (void*)hooked_wsarecvfrom);
        trace_patches += patch_import_address(modules[index], (void*)original_wsagetoverlappedresult, (void*)hooked_wsagetoverlappedresult);
        if (original_messageboxw != NULL) {
            trace_patches += patch_import(modules[index], "USER32.dll", "MessageBoxW", 0, (void*)hooked_messageboxw);
            trace_patches += patch_import_address(modules[index], (void*)original_messageboxw, (void*)hooked_messageboxw);
        }
        if (original_messageboxa != NULL) {
            trace_patches += patch_import(modules[index], "USER32.dll", "MessageBoxA", 0, (void*)hooked_messageboxa);
            trace_patches += patch_import_address(modules[index], (void*)original_messageboxa, (void*)hooked_messageboxa);
        }
        if (original_getfileattributesw != NULL) {
            trace_patches += patch_import(modules[index], "KERNEL32.dll", "GetFileAttributesW", 0, (void*)hooked_getfileattributesw);
            trace_patches += patch_import_address(modules[index], (void*)original_getfileattributesw, (void*)hooked_getfileattributesw);
        }
    }
    (void)trace_patches;
    return (dns_patches > 0 ? 0 : (1 | (int)dns_diagnostics)) | (connect_patches > 0 ? 0 : 2);
}

static DWORD WINAPI patch_late_modules(LPVOID parameter) {
    int attempt;
    (void)parameter;
    for (attempt = 0; attempt < 600; attempt++) {
        if (GetModuleHandleA("EAWebKit.dll") != NULL) {
            (void)patch_network_imports();
            return 0;
        }
        Sleep(100);
    }
    return 1;
}

static char darkspin_window_title[128] = "Dark Spin v0.0.0";

static BOOL CALLBACK rename_game_window(HWND window, LPARAM parameter) {
    DWORD process_id = 0;
    char current_title[128];
    BOOL* is_renamed = (BOOL*)parameter;
    GetWindowThreadProcessId(window, &process_id);
    if (process_id != GetCurrentProcessId() || !IsWindowVisible(window) || GetWindow(window, GW_OWNER) != NULL) {
        return TRUE;
    }
    if (GetWindowTextA(window, current_title, sizeof(current_title)) <= 0) {
        return TRUE;
    }
    if (_strnicmp(current_title, "Game", 4) != 0 && _strnicmp(current_title, "Dark Spin", 9) != 0) {
        return TRUE;
    }
    install_chat_wndproc(window);
    if (SetWindowTextA(window, darkspin_window_title)) {
        *is_renamed = TRUE;
    }
    return TRUE;
}

static DWORD WINAPI brand_game_window(LPVOID parameter) {
    int attempt;
    int confirmation;
    BOOL is_renamed = FALSE;
    (void)parameter;
    for (attempt = 0; attempt < 600; attempt++) {
        is_renamed = FALSE;
        EnumWindows(rename_game_window, (LPARAM)&is_renamed);
        if (!is_renamed) {
            Sleep(100);
            continue;
        }
        for (confirmation = 0; confirmation < 3; confirmation++) {
            Sleep(1000);
            is_renamed = FALSE;
            EnumWindows(rename_game_window, (LPARAM)&is_renamed);
        }
        return 0;
    }
    return 1;
}

int fang_install(const char* hostname, unsigned short port, unsigned short party_port,
    const char* trace_path, int skip_intro, int skip_cinematic, const char* jwt,
    const char* window_title) {
    static const BYTE ssl_ctx_signature[] = {0x8B, 0x44, 0x24, 0x04, 0x8B, 0x4C, 0x24, 0x08, 0x8B, 0x54, 0x24, 0x0C, 0x89, 0x88};
    static const BYTE ssl_result_signature[] = {0x8B, 0x44, 0x24, 0x04, 0x8B, 0x80, 0xE0, 0x00, 0x00, 0x00, 0xC3};
    static const BYTE wildcard_signature[] = {0x53, 0x56, 0x8B, 0x74, 0x24, 0x10, 0x57, 0x8B, 0x7C, 0x24, 0x10, 0xEB, 0x03};
    static const BYTE certificate_signature[] = {0x83, 0x7C, 0x24, 0x04, 0x00, 0x56, 0x57, 0x8B, 0xF0};
    static const BYTE secure_connect_signature[] = {0x8B, 0x5C, 0x24, 0x14, 0x8B, 0xC6, 0x89, 0xBE, 0x20, 0x01, 0x00, 0x00, 0xC7, 0x86, 0x24, 0x01, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00};
    static const BYTE force_plaintext[] = {0x31, 0xDB, 0x90, 0x90};
    static const BYTE opening_movie_branch[] = {0x84, 0xC0, 0x74, 0x77, 0x8D, 0x44, 0x24, 0x0C};
    static const BYTE unconditional_jump[] = {0xEB};
    HMODULE executable = GetModuleHandleA(NULL);
    HMODULE winsock = GetModuleHandleA("ws2_32.dll");
    HMODULE user32 = GetModuleHandleA("user32.dll");
    HMODULE kernel32_module = GetModuleHandleA("kernel32.dll");
    int result = 0;
    int alert_patches = 0;
    int login_error_patches = 0;
    int cinematic_trace_patches = 0;
    int scene_trace_patches = 0;
    int snapshot_trace_patches = 0;
    int loot_trace_patches = 0;
    int mana_cost_patches = 0;
    char snapshot_mode[16];
    dns_diagnostics = 0;
    executable_module = executable;
    ZeroMemory(snapshot_mode, sizeof(snapshot_mode));
    GetEnvironmentVariableA(
        "DARKSPIN_SNAPSHOT_MODE", snapshot_mode, (DWORD)sizeof(snapshot_mode));
    ZeroMemory(snapshot_control_path, sizeof(snapshot_control_path));
    GetEnvironmentVariableA(
        "DARKSPIN_SNAPSHOT_CONTROL", snapshot_control_path,
        (DWORD)sizeof(snapshot_control_path));
    InterlockedExchange(&snapshot_capture_enabled,
        (_stricmp(snapshot_mode, "manual") == 0 ||
         _stricmp(snapshot_mode, "auto") == 0) ? 1 : 0);
    InterlockedExchange(&snapshot_auto_probe_enabled,
        _stricmp(snapshot_mode, "auto") == 0 ? 1 : 0);
    if (movement_trace_tls_index == TLS_OUT_OF_INDEXES) {
        movement_trace_tls_index = TlsAlloc();
    }
    GetEnvironmentVariableA("DARKSPIN_CLIENT_RESULT", client_result_path,
        (DWORD)sizeof(client_result_path));
    GetEnvironmentVariableA("DARKSPIN_CLIENT_LAUNCH_ID", client_launch_id,
        (DWORD)sizeof(client_launch_id));
    original_alert = (alert_fn)((BYTE*)executable + 0x3BAC40);
    original_login_error = (login_error_fn)((BYTE*)executable + 0x83FA80);
    original_scene_ready = (cinematic_ready_fn)((BYTE*)executable + 0xF3FF0);
    original_ui_ready = (cinematic_ready_fn)((BYTE*)executable + 0xE6310);
    original_screen_event = (screen_event_fn)((BYTE*)executable + 0x50EAF0);
    original_movie_load = (movie_load_fn)((BYTE*)executable + 0x3ADD40);
    original_movie_manager = (movie_manager_fn)((BYTE*)executable + 0x3B6FD0);
    original_spline_update = (spline_update_fn)((BYTE*)executable + 0x61AFE0);
    original_spline_skip = (spline_skip_fn)((BYTE*)executable + 0x61B840);
    original_camera_registry = (camera_registry_fn)((BYTE*)executable + 0x3AEBB0);
    original_camera_set = (camera_set_fn)((BYTE*)executable + 0x12F0E0);
    original_navigation_update = (navigation_update_fn)((BYTE*)executable + 0x12D600);
    original_party_ready = (party_ready_fn)((BYTE*)executable + 0x119960);
    original_go_to_room = (go_to_room_fn)((BYTE*)executable + 0x12CFD0);
    original_new_player_progress = (new_player_progress_fn)((BYTE*)executable + 0x1187D0);
    original_map_room_start_game = (map_room_start_game_fn)((BYTE*)executable + 0x116100);
    original_online_platform = (online_platform_fn)((BYTE*)executable + 0x826F80);
    original_rooms_bootstrap = (rooms_bootstrap_fn)((BYTE*)executable + 0x828550);
    original_rooms_map_lookup = (rooms_map_lookup_fn)((BYTE*)executable + 0xA1BC10);
    original_scene_asset_lookup = (scene_asset_lookup_fn)((BYTE*)executable + 0x5C5FF0);
    original_creature_asset_lookup = (creature_asset_lookup_fn)((BYTE*)executable + 0x5ECA10);
    original_creature_nested_lookup = (creature_nested_lookup_fn)((BYTE*)executable + 0x5C8E70);
    original_scene_load = (scene_load_fn)((BYTE*)executable + 0xE7480);
    original_scene_change = (scene_change_fn)((BYTE*)executable + 0xF3160);
    original_ability_hud = (ability_hud_fn)((BYTE*)executable + 0x25980);
    original_cooldown_lookup = (cooldown_lookup_fn)((BYTE*)executable + 0x21F50);
    original_cooldown_apply = (cooldown_apply_fn)((BYTE*)executable + 0xD6320);
    original_game_clock_owner = (game_clock_owner_fn)((BYTE*)executable + 0xE4DE0);
    original_game_clock_now = (game_clock_now_fn)((BYTE*)executable + 0x5D2920);
    original_controlled_player = (controlled_player_fn)((BYTE*)executable + 0xE57B0);
    original_clock_root = (clock_root_fn)((BYTE*)executable + 0x5BCBE0);
    original_clock_state = (clock_state_fn)((BYTE*)executable + 0x5BD190);
    original_clock_initialize = (clock_initialize_fn)((BYTE*)executable + 0x5D2830);
    original_ability_available = (ability_available_fn)((BYTE*)executable + 0x5C2A60);
    original_property_vector_read = (property_vector_read_fn)((BYTE*)executable + 0x3B3E50);
    original_resource_manager = (resource_manager_fn)((BYTE*)executable + 0x3AECE0);
    original_reflection_lookup = (reflection_lookup_fn)((BYTE*)executable + 0x5EF610);
    original_server_event_decode = (server_event_decode_fn)((BYTE*)executable + 0x61E1F0);
    original_server_event_validate = (server_event_validate_fn)((BYTE*)executable + 0xDFFB0);
    original_effect_system = (effect_system_fn)((BYTE*)executable + 0x3AEC80);
    original_game_object_registry = (resource_manager_fn)((BYTE*)executable + 0x5BC740);
    original_game_object_lookup = (lookup_method_fn)((BYTE*)executable + 0x5D1730);
    original_message_construct = (message_construct_fn)((BYTE*)executable + 0x687850);
    original_message_read = (message_read_fn)((BYTE*)executable + 0x687BF0);
    original_login_screen_init = (login_screen_init_fn)((BYTE*)executable + 0x114530);
    original_launcher_ready = (launcher_ready_fn)((BYTE*)executable + 0x10E7D0);
    original_local_event = (local_event_fn)((BYTE*)executable + 0xE17E0);
    original_loot_convert = (loot_convert_fn)((BYTE*)executable + 0x5CD7C0);
    original_loot_format = (loot_convert_fn)((BYTE*)executable + 0x3A170);
    original_mana_cost = (mana_cost_fn)((BYTE*)executable + 0x5DD720);
    original_ability_descriptor_lookup =
        (ability_descriptor_lookup_fn)((BYTE*)executable + 0x5DAC00);
    original_combat_input_state = (combat_input_state_fn)((BYTE*)executable + 0xE4E00);
    original_combat_input_reset = (combat_input_reset_fn)((BYTE*)executable + 0xD5EF0);
    original_settings_manager = (settings_manager_fn)((BYTE*)executable + 0x3AEBD0);
    original_game_object_resolve =
        (game_object_resolve_fn)((BYTE*)executable + 0x5D94F0);
    original_player_move_receiver =
        (locomotion_receiver_fn)((BYTE*)executable + 0x1393D0);
    original_locomotion_update_receiver =
        (locomotion_receiver_fn)((BYTE*)executable + 0x139900);
    original_locomotion_unreliable_receiver =
        (locomotion_receiver_fn)((BYTE*)executable + 0x139810);
    original_frame_delta =
        (frame_delta_fn)((BYTE*)executable + 0x5D2960);
    original_sporenet_callback = (sporenet_callback_fn)((BYTE*)executable + 0xA57D0);
    original_scaleform_stream_read =
        (scaleform_stream_read_fn)((BYTE*)executable + 0xA54240);
    original_chat_lookup = (chat_lookup_fn)((BYTE*)executable + 0x25680);
    original_chat_create = (chat_create_fn)((BYTE*)executable + 0xB5D0);
    original_chat_initialize = (chat_initialize_fn)((BYTE*)executable + 0xABA0);
    original_chat_show = (chat_show_fn)((BYTE*)executable + 0xD850);
    original_chat_open = (chat_open_fn)((BYTE*)executable + 0xB8F0);
    original_chat_text_convert = (chat_text_convert_fn)((BYTE*)executable + 0x6D9E40);
    skip_cinematics_enabled = skip_cinematic;
	 if (jwt != NULL && jwt[0] != '\0') {
		 strncpy(jwt_login_token, jwt, sizeof(jwt_login_token) - 1);
		 jwt_login_token[sizeof(jwt_login_token) - 1] = '\0';
	 }
    if (hostname == NULL || hostname[0] == '\0' || port == 0 || party_port == 0 ||
        winsock == NULL) {
        return 0x100;
    }
    if (window_title != NULL && window_title[0] != '\0') {
        strncpy(darkspin_window_title, window_title, sizeof(darkspin_window_title) - 1);
        darkspin_window_title[sizeof(darkspin_window_title) - 1] = '\0';
    }
    {
        HANDLE title_thread = CreateThread(NULL, 0, brand_game_window, NULL, 0, NULL);
        if (title_thread != NULL) {
            CloseHandle(title_thread);
        } else {
            result |= 0x4000;
        }
    }
    strncpy(redirect_hostname, hostname, sizeof(redirect_hostname) - 1);
    redirect_hostname[sizeof(redirect_hostname) - 1] = '\0';
    redirect_port = port;
    redirect_party_port = party_port;
    original_gethostbyname = (gethostbyname_fn)GetProcAddress(winsock, "gethostbyname");
    original_connect = (connect_fn)GetProcAddress(winsock, "connect");
	 original_send = (send_fn)GetProcAddress(winsock, "send");
	 original_recv = (recv_fn)GetProcAddress(winsock, "recv");
	 original_sendto = (sendto_fn)GetProcAddress(winsock, "sendto");
	 original_recvfrom = (recvfrom_fn)GetProcAddress(winsock, "recvfrom");
	 original_closesocket = (closesocket_fn)GetProcAddress(winsock, "closesocket");
	 original_wsasend = (wsasend_fn)GetProcAddress(winsock, "WSASend");
	 original_wsarecv = (wsarecv_fn)GetProcAddress(winsock, "WSARecv");
	 original_wsarecvfrom = (wsarecvfrom_fn)GetProcAddress(winsock, "WSARecvFrom");
	 original_wsagetoverlappedresult = (wsagetoverlappedresult_fn)GetProcAddress(winsock, "WSAGetOverlappedResult");
    original_messageboxw = user32 == NULL ? NULL : (messageboxw_fn)GetProcAddress(user32, "MessageBoxW");
    original_messageboxa = user32 == NULL ? NULL : (messageboxa_fn)GetProcAddress(user32, "MessageBoxA");
    original_getfileattributesw = kernel32_module == NULL ? NULL : (getfileattributesw_fn)GetProcAddress(kernel32_module, "GetFileAttributesW");
    if (original_gethostbyname == NULL || original_connect == NULL) {
        return 0x200;
    }
#if FANG_DIAGNOSTICS
	 if (trace_path != NULL && trace_path[0] != '\0') {
		 InitializeCriticalSection(&trace_lock);
		 trace_lock_ready = 1;
		 trace_file = CreateFileA(trace_path, GENERIC_WRITE, FILE_SHARE_READ | FILE_SHARE_WRITE, NULL, CREATE_ALWAYS, FILE_ATTRIBUTE_NORMAL, NULL);
	 }
#else
    (void)trace_path;
#endif
    if (snapshot_control_path[0] != '\0' && snapshot_keyframe_event == NULL) {
        snapshot_keyframe_event = CreateEventA(NULL, TRUE, FALSE, NULL);
        if (snapshot_keyframe_event == NULL) {
            result |= 0x20000000;
        }
    }
    if (snapshot_control_path[0] != '\0') {
        HANDLE snapshot_thread = CreateThread(
            NULL, 0, watch_snapshot_control, NULL, 0, NULL);
        if (snapshot_thread != NULL) {
            CloseHandle(snapshot_thread);
        } else {
            result |= 0x20000000;
        }
    }
    trace_client_state("snapshot_capture_mode",
        (unsigned int)InterlockedCompareExchange(
            &snapshot_capture_enabled, 0, 0));
    {
        int is_locale_override_installed = patch_locale_argument_precedence(executable);
        trace_client_state("locale_argument_override",
            (unsigned int)is_locale_override_installed);
        if (!is_locale_override_installed) {
            result |= 0x8;
        }
    }
    trace_client_state("solo_unranked_1v1_stream_hook", (unsigned int)patch_pointer(
        (void**)((BYTE*)executable + 0xCE4ED0),
        (void*)original_scaleform_stream_read,
        (void*)hooked_scaleform_stream_read));
#if defined(__GNUC__) && defined(__i386__)
    trace_client_state("solo_unranked_1v1_matchmaking_hook", (unsigned int)patch_pointer(
        (void**)((BYTE*)executable + 0x118403),
        (void*)original_map_room_start_game,
        (void*)hooked_map_room_start_game));
#endif
    {
        int is_sporenet_callback_hooked = patch_pointer(
            (void**)((BYTE*)executable + 0xBD8A6C),
            (void*)original_sporenet_callback,
            (void*)hooked_sporenet_callback);
        trace_client_state("sporenet_callback_hook", (unsigned int)is_sporenet_callback_hooked);
        if (!is_sporenet_callback_hooked) {
            result |= 0x800;
        }
    }
    {
        HANDLE chat_key_thread = CreateThread(NULL, 0, poll_chat_key, NULL, 0, NULL);
        if (chat_key_thread != NULL) {
            CloseHandle(chat_key_thread);
        } else {
            result |= 0x10000000;
        }
    }
    redirect_tls_index = TlsAlloc();
    if (redirect_tls_index == TLS_OUT_OF_INDEXES) {
        return 0x400;
    }
#if FANG_DIAGNOSTICS
    effect_preview_tls_index = TlsAlloc();
    if (effect_preview_tls_index == TLS_OUT_OF_INDEXES) {
        trace_client_state("effect_preview_init", 0);
        result |= 0x40000000;
    } else {
        trace_client_state("effect_preview_init", 1);
    }
#endif
    result |= patch_network_imports();
    {
        HANDLE patch_thread = CreateThread(NULL, 0, patch_late_modules, NULL, 0, NULL);
        if (patch_thread != NULL) {
            CloseHandle(patch_thread);
        } else {
            result |= 0x2000;
        }
    }
    if (!patch_jump(find_signature(executable, ssl_ctx_signature, sizeof(ssl_ctx_signature)), (void*)hooked_ssl_ctx_set_verify)) {
        result |= 0x10;
    }
    if (!patch_jump(find_signature(executable, ssl_result_signature, sizeof(ssl_result_signature)), (void*)hooked_ssl_get_verify_result)) {
        result |= 0x20;
    }
    if (!patch_jump(find_signature(executable, wildcard_signature, sizeof(wildcard_signature)), (void*)hooked_wildcard_match_no_case)) {
        result |= 0x40;
    }
    if (!patch_jump(find_signature(executable, certificate_signature, sizeof(certificate_signature)), (void*)hooked_verify_certificate)) {
        result |= 0x80;
    }
    if (!patch_bytes(find_signature(executable, secure_connect_signature, sizeof(secure_connect_signature)), force_plaintext, sizeof(force_plaintext))) {
        result |= 0x1000;
    }
    if (!patch_call((BYTE*)executable + 0x110BE6, (void*)original_launcher_ready, (void*)hooked_launcher_ready)) {
        result |= 0x2000000;
    }
    {
        int is_chat_command_hooked = patch_call((BYTE*)executable + 0xC998,
            (void*)original_chat_text_convert, (void*)hooked_chat_text_convert);
        trace_client_state("chat_command_hook", (unsigned int)is_chat_command_hooked);
        if (!is_chat_command_hooked) {
            result |= 0x20000000;
        }
    }
    if (skip_intro || skip_cinematic) {
        BYTE* opening_branch = find_signature(executable, opening_movie_branch, sizeof(opening_movie_branch));
        if (opening_branch == NULL || !patch_bytes(opening_branch + 2, unconditional_jump, sizeof(unconditional_jump))) {
            result |= 0x10000;
        }
    }
    if (skip_cinematic) {
        if (!patch_call((BYTE*)executable + 0x131F64, (void*)original_movie_load, (void*)hooked_movie_load)) {
            result |= 0x200000;
        }
        if (!patch_call((BYTE*)executable + 0xF30D8, (void*)original_spline_update, (void*)hooked_spline_update)) {
            result |= 0x400000;
        }
        if (!patch_call((BYTE*)executable + 0x555EE, (void*)original_navigation_update, (void*)hooked_navigation_update)) {
            result |= 0x1000000;
        }
    }
#if FANG_DIAGNOSTICS
    {
        int is_party_ready_hooked = patch_call((BYTE*)executable + 0x115A3F,
            (void*)original_party_ready, (void*)hooked_party_ready);
        trace_client_state("party_navigation_hook", (unsigned int)is_party_ready_hooked);
    }
    cinematic_trace_patches += patch_call((BYTE*)executable + 0x5557A, (void*)original_scene_ready, (void*)hooked_scene_ready);
    cinematic_trace_patches += patch_call((BYTE*)executable + 0x55583, (void*)original_ui_ready, (void*)hooked_ui_ready);
#endif
    if (FANG_DIAGNOSTICS || skip_cinematic) {
        cinematic_trace_patches += patch_call((BYTE*)executable + 0x55401, (void*)original_screen_event, (void*)hooked_screen_event);
        cinematic_trace_patches += patch_call((BYTE*)executable + 0x55567, (void*)original_screen_event, (void*)hooked_screen_event);
        cinematic_trace_patches += patch_call((BYTE*)executable + 0x555CC, (void*)original_screen_event, (void*)hooked_screen_event);
    }
    if (cinematic_trace_patches != (FANG_DIAGNOSTICS ? 5 : (skip_cinematic ? 3 : 0))) {
        result |= 0x100000;
    }
    snapshot_trace_patches += patch_call((BYTE*)executable + 0x68A4D3,
        (void*)original_message_construct, (void*)hooked_message_construct);
    snapshot_trace_patches += patch_call((BYTE*)executable + 0x139447,
        (void*)original_game_object_resolve, (void*)hooked_movement_object_resolve);
    snapshot_trace_patches += patch_call((BYTE*)executable + 0x139886,
        (void*)original_game_object_resolve, (void*)hooked_movement_object_resolve);
    snapshot_trace_patches += patch_call((BYTE*)executable + 0x13997B,
        (void*)original_game_object_resolve, (void*)hooked_movement_object_resolve);
    snapshot_trace_patches += patch_call((BYTE*)executable + 0x13AE74,
        (void*)original_player_move_receiver, (void*)hooked_player_move_receiver);
    snapshot_trace_patches += patch_call((BYTE*)executable + 0x13AE9B,
        (void*)original_locomotion_update_receiver, (void*)hooked_locomotion_update_receiver);
    snapshot_trace_patches += patch_call((BYTE*)executable + 0x13AEA8,
        (void*)original_locomotion_unreliable_receiver,
        (void*)hooked_locomotion_unreliable_receiver);
    snapshot_trace_patches += patch_call((BYTE*)executable + 0x5BF1F6,
        (void*)original_frame_delta, (void*)hooked_frame_delta);
    if (snapshot_trace_patches != 8) {
        result |= 0x4000000;
    }
#if FANG_DIAGNOSTICS
    /* Scene observation and developer recovery hooks are omitted from release
       Fang builds. */
    scene_trace_patches += patch_call((BYTE*)executable + 0x137E4B,
        (void*)original_scene_asset_lookup, (void*)hooked_scene_asset_lookup);
    scene_trace_patches += patch_call((BYTE*)executable + 0x137E37,
        (void*)original_message_read, (void*)hooked_quick_game_read);
    scene_trace_patches += patch_call((BYTE*)executable + 0x1F9FD,
        (void*)original_creature_asset_lookup, (void*)hooked_creature_asset_lookup);
    scene_trace_patches += patch_call((BYTE*)executable + 0x1FA0E,
        (void*)original_creature_nested_lookup, (void*)hooked_creature_nested_lookup);
    scene_trace_patches += patch_call((BYTE*)executable + 0x12B9F9,
        (void*)original_scene_load, (void*)hooked_scene_load);
    scene_trace_patches += patch_call((BYTE*)executable + 0x137F99,
        (void*)original_scene_load, (void*)hooked_scene_load);
    scene_trace_patches += patch_call((BYTE*)executable + 0xE7495,
        (void*)original_scene_change, (void*)hooked_scene_change);
    scene_trace_patches += patch_call((BYTE*)executable + 0x13A5D7,
        (void*)original_cooldown_apply, (void*)hooked_cooldown_apply);
    scene_trace_patches += patch_call((BYTE*)executable + 0x137D30,
        (void*)original_clock_initialize, (void*)hooked_clock_initialize);
    scene_trace_patches += patch_call((BYTE*)executable + 0x5CF595,
        (void*)original_property_vector_read, (void*)hooked_property_vector_read);
    scene_trace_patches += patch_call((BYTE*)executable + 0xEC129,
        (void*)original_ability_available, (void*)hooked_ability_blink_play_available);
    scene_trace_patches += patch_call((BYTE*)executable + 0xEC1A9,
        (void*)original_ability_available, (void*)hooked_ability_blink_stop_available);
    scene_trace_patches += patch_call((BYTE*)executable + 0x21631,
        (void*)original_ability_descriptor_lookup, (void*)hooked_ability_descriptor_lookup);
#endif
#if FANG_DIAGNOSTICS
    if (effect_preview_tls_index != TLS_OUT_OF_INDEXES) {
        int is_decode_hooked = patch_call((BYTE*)executable + 0x139EDB,
            (void*)original_server_event_decode, (void*)hooked_server_event_decode);
        int is_validate_hooked = patch_call((BYTE*)executable + 0x106DB7,
            (void*)original_server_event_validate, (void*)hooked_server_event_validate);
        trace_client_state("effect_preview_decode_hook", (unsigned int)is_decode_hooked);
        trace_client_state("effect_preview_validate_hook", (unsigned int)is_validate_hooked);
        if (!is_decode_hooked || !is_validate_hooked) {
            result |= 0x40000000;
        }
    }
#endif
    if (scene_trace_patches != (FANG_DIAGNOSTICS ? 13 : 0)) {
        result |= 0x4000000;
    }
#if FANG_DIAGNOSTICS
    loot_trace_patches += patch_call((BYTE*)executable + 0x139F12,
        (void*)original_local_event, (void*)hooked_local_event);
    loot_trace_patches += patch_call((BYTE*)executable + 0xE1EBC,
        (void*)original_loot_convert, (void*)hooked_loot_convert);
    loot_trace_patches += patch_call((BYTE*)executable + 0xE1EDD,
        (void*)original_loot_format, (void*)hooked_loot_format);
    loot_trace_patches += patch_call((BYTE*)executable + 0x5CD800,
        (void*)original_creature_nested_lookup, (void*)hooked_loot_resource_lookup);
    loot_trace_patches += patch_call((BYTE*)executable + 0x5CD82F,
        (void*)original_creature_nested_lookup, (void*)hooked_loot_resource_lookup);
    loot_trace_patches += patch_call((BYTE*)executable + 0x5CD89C,
        (void*)original_creature_nested_lookup, (void*)hooked_loot_resource_lookup);
    loot_trace_patches += patch_call((BYTE*)executable + 0x5CD8D2,
        (void*)original_creature_nested_lookup, (void*)hooked_loot_resource_lookup);
    loot_trace_patches += patch_call((BYTE*)executable + 0x5CD90A,
        (void*)original_creature_nested_lookup, (void*)hooked_loot_resource_lookup);
    if (loot_trace_patches != 8) {
        result |= 0x8000000;
    }
    mana_cost_patches += patch_call((BYTE*)executable + 0x39284,
        (void*)original_mana_cost, (void*)hooked_mana_cost);
    mana_cost_patches += patch_call((BYTE*)executable + 0xDEAD5,
        (void*)original_mana_cost, (void*)hooked_mana_cost);
    mana_cost_patches += patch_call((BYTE*)executable + 0xE561D,
        (void*)original_mana_cost, (void*)hooked_mana_cost);
    mana_cost_patches += patch_call((BYTE*)executable + 0x5E0863,
        (void*)original_mana_cost, (void*)hooked_mana_cost);
    mana_cost_patches += patch_call((BYTE*)executable + 0x5E0D6D,
        (void*)original_mana_cost, (void*)hooked_mana_cost);
    mana_cost_patches += patch_call((BYTE*)executable + 0x66824B,
        (void*)original_mana_cost, (void*)hooked_mana_cost);
    trace_client_state("mana_cost_hooks", (unsigned int)mana_cost_patches);
    alert_patches += patch_call((BYTE*)executable + 0x10F47F, (void*)original_alert, (void*)hooked_internal_alert);
    alert_patches += patch_call((BYTE*)executable + 0x10F82B, (void*)original_alert, (void*)hooked_internal_alert);
    alert_patches += patch_call((BYTE*)executable + 0x13589B, (void*)original_alert, (void*)hooked_internal_alert);
    alert_patches += patch_call((BYTE*)executable + 0x135A0F, (void*)original_alert, (void*)hooked_internal_alert);
    alert_patches += patch_call((BYTE*)executable + 0x3E7835, (void*)original_alert, (void*)hooked_internal_alert);
    alert_patches += patch_call((BYTE*)executable + 0x3E7990, (void*)original_alert, (void*)hooked_internal_alert);
    alert_patches += patch_call((BYTE*)executable + 0x3E8CAF, (void*)original_alert, (void*)hooked_internal_alert);
    alert_patches += patch_call((BYTE*)executable + 0x45483F, (void*)original_alert, (void*)hooked_internal_alert);
    alert_patches += patch_call((BYTE*)executable + 0x454AFC, (void*)original_alert, (void*)hooked_internal_alert);
    alert_patches += patch_call((BYTE*)executable + 0x459688, (void*)original_alert, (void*)hooked_internal_alert);
    alert_patches += patch_call((BYTE*)executable + 0x4C9976, (void*)original_alert, (void*)hooked_internal_alert);
    if (alert_patches != 11) {
        result |= 0x4000;
    }
    login_error_patches += patch_call((BYTE*)executable + 0x83FF3B, (void*)original_login_error, (void*)hooked_login_error);
    login_error_patches += patch_call((BYTE*)executable + 0x83FF9B, (void*)original_login_error, (void*)hooked_login_error);
    login_error_patches += patch_call((BYTE*)executable + 0x84033B, (void*)original_login_error, (void*)hooked_login_error);
    login_error_patches += patch_call((BYTE*)executable + 0x84039B, (void*)original_login_error, (void*)hooked_login_error);
    login_error_patches += patch_call((BYTE*)executable + 0x840481, (void*)original_login_error, (void*)hooked_login_error);
    if (login_error_patches != 5) {
        result |= 0x8000;
    }
#endif
    if (jwt_login_token[0] != '\0' &&
        !patch_pointer((void**)((BYTE*)executable + 0xBDF9A4), (void*)original_login_screen_init, (void*)hooked_login_screen_init)) {
        result |= 0x800000;
    }
    return result;
}

__declspec(dllexport) DWORD WINAPI RecapInitializeThread(LPVOID parameter) {
    (void)parameter;
    return (DWORD)GoRecapInitialize();
}

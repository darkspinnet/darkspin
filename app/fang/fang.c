#include "client103/bootstrap.h"
#include "zoom/zoom.h"
#include "editor/editor.h"
#include "display/display.h"
#include "shop/shop.h"
#include "bug/bug.h"
#include "scenario/camera.h"
#include "login/login.h"
#include "mod/mod.h"
#include "hook/patch.h"
#include "client103/camera.h"
#include "client103/image.h"
#include "fang.h"
#include "hook.h"
#include "hook/import.h"
#include "network/network.h"
#include "chat/command.h"
#include "cinematic/cinematic.h"
#include "bug/history.h"
#include "bug/record.h"
#include "scenario/observation.h"
#if FANG_SCENARIO
#include "scenario/renderer.h"
#include "scenario/dispatch.h"
#include "scenario/selector.h"
#include "scenario/primary.h"
#include "scenario/alternate.h"
#include "scenario/classification.h"
#include "scenario/control.h"
#endif

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
static DWORD effect_preview_tls_index = TLS_OUT_OF_INDEXES;
static HANDLE trace_file = INVALID_HANDLE_VALUE;
static CRITICAL_SECTION trace_lock;
static int trace_lock_ready;
static HMODULE executable_module;
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
static void* jwt_login_controller;
static void* jwt_login_controller_type;
static UINT_PTR jwt_login_timer;
static UINT_PTR jwt_login_ui_timer;
static ULONGLONG jwt_login_ui_started_ms;
static unsigned int jwt_login_retry_count;
static ULONGLONG jwt_login_ready_started_ms;
enum { jwt_login_timeout_milliseconds = 30000 };
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
static volatile LONG diagnostic_resource_object_id;
static volatile LONG diagnostic_hit_point_bits;
static volatile LONG diagnostic_power_point_bits;
static volatile LONG diagnostic_resource_mask;
static volatile LONG diagnostic_candidate_resource_object_id;
static volatile LONG diagnostic_candidate_hit_point_bits;
static volatile LONG diagnostic_candidate_power_point_bits;
static volatile LONG diagnostic_candidate_resource_mask;
static volatile LONG snapshot_capture_enabled;
static volatile LONG snapshot_auto_probe_enabled;
static volatile LONG snapshot_frame_sequence;
static volatile LONG snapshot_frame_delta_bits;
static volatile LONG snapshot_frame_time_ms;
static volatile LONG snapshot_keyframe_started_time_ms;
static volatile LONG snapshot_keyframe_completed_time_ms;
static volatile LONG snapshot_pending_network_object_id;
static volatile LONG snapshot_network_object_ids_by_slot[65536];
static volatile LONG snapshot_client_handles_by_slot[65536];
static volatile LONG snapshot_keyframe_pending;
static HANDLE snapshot_keyframe_event;
static DWORD snapshot_last_probe_time;
static DWORD movement_trace_tls_index = TLS_OUT_OF_INDEXES;
static char snapshot_directory[MAX_PATH * 4];
static char snapshot_endpoint[64];
static char snapshot_token[65];

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
static unsigned int chat_open_retry_count;
static DWORD chat_rooms_bootstrap_time;
static WNDPROC original_game_wndproc;
static HWND chat_game_window;

#define CHAT_OPEN_MESSAGE (WM_APP + 0x45)
#define CHAT_RESET_MESSAGE (WM_APP + 0x46)

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

int fang_network_capture_is_enabled(void) {
    return InterlockedCompareExchange(&snapshot_capture_enabled, 0, 0);
}

LONG fang_network_matchmaking_pending(void) {
    return InterlockedCompareExchange(&solo_unranked_1v1_matchmaking_pending, 0, 0);
}

int fang_network_login_is_pending(void) {
    return jwt_login_token[0] != '\0' &&
        InterlockedCompareExchange(&jwt_login_completed, 0, 0) == 0;
}

int fang_record_trace_available(void) {
    return trace_file != INVALID_HANDLE_VALUE && trace_lock_ready;
}

BOOL fang_record_trace_write(const char* contents, DWORD length, DWORD* written) {
    BOOL result;
    EnterCriticalSection(&trace_lock);
    result = WriteFile(trace_file, contents, length, written, NULL);
    LeaveCriticalSection(&trace_lock);
    return result;
}

const char* fang_record_directory(void) {
    return snapshot_directory;
}

LONG fang_record_frame_sequence(void) {
    return InterlockedCompareExchange(&snapshot_frame_sequence, 0, 0);
}

LONG fang_record_frame_delta_bits(void) {
    return InterlockedCompareExchange(&snapshot_frame_delta_bits, 0, 0);
}

LONG fang_record_frame_time_ms(void) {
    return InterlockedCompareExchange(&snapshot_frame_time_ms, 0, 0);
}

LONG fang_record_keyframe_started_time_ms(void) {
    return InterlockedCompareExchange(&snapshot_keyframe_started_time_ms, 0, 0);
}

LONG fang_record_keyframe_completed_time_ms(void) {
    return InterlockedCompareExchange(&snapshot_keyframe_completed_time_ms, 0, 0);
}

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
static void trace_client_message_payload(unsigned char id, const void* data, unsigned int size);
static void trace_client_object_registry(void);
static void trace_client_object_probe_registry(void);
static void trace_snapshot_action_state(void);

static void trace_object_association(const char* kind,
    unsigned int network_object_id, unsigned int client_handle) {
    char line[384];
    DWORD written;
    int length;
    unsigned int slot = client_handle & 0xffffu;
    if (kind == NULL || network_object_id == 0 || client_handle == 0) {
        return;
    }
    length = snprintf(line, sizeof(line),
        "{\"time_ms\":%llu,\"protocol\":\"client_state\","
        "\"kind\":\"%s\",\"thread\":%lu,\"frame_sequence\":%lu,"
        "\"network_object_id\":%u,\"object_id\":%u,"
        "\"handle_slot\":%u,\"handle_generation\":%u}\r\n",
        (unsigned long long)GetTickCount64(), kind,
        (unsigned long)GetCurrentThreadId(),
        (unsigned long)InterlockedCompareExchange(&snapshot_frame_sequence, 0, 0),
        network_object_id, client_handle, slot, client_handle >> 16);
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

static unsigned int mapped_network_object_id(unsigned int client_handle) {
    unsigned int slot = client_handle & 0xffffu;
    if ((unsigned int)InterlockedCompareExchange(
            &snapshot_client_handles_by_slot[slot], 0, 0) != client_handle) {
        return 0;
    }
    return (unsigned int)InterlockedCompareExchange(
        &snapshot_network_object_ids_by_slot[slot], 0, 0);
}

static void remember_object_association(
    unsigned int network_object_id, unsigned int client_handle) {
    unsigned int slot;
    if (network_object_id == 0 || client_handle == 0) {
        return;
    }
    slot = client_handle & 0xffffu;
    InterlockedExchange(
        &snapshot_network_object_ids_by_slot[slot], (LONG)network_object_id);
    InterlockedExchange(&snapshot_client_handles_by_slot[slot], (LONG)client_handle);
    trace_object_association("object_association", network_object_id, client_handle);
}

static void forget_object_association(unsigned int network_object_id) {
    unsigned int slot;
    if (network_object_id == 0) {
        return;
    }
    for (slot = 0; slot < 65536; slot++) {
        unsigned int current = (unsigned int)InterlockedCompareExchange(
            &snapshot_network_object_ids_by_slot[slot], 0, 0);
        if (current != network_object_id) {
            continue;
        }
        {
            unsigned int client_handle = (unsigned int)InterlockedExchange(
                &snapshot_client_handles_by_slot[slot], 0);
            InterlockedExchange(&snapshot_network_object_ids_by_slot[slot], 0);
            trace_object_association(
                "object_disassociation", network_object_id, client_handle);
        }
    }
}

static float __cdecl hooked_frame_delta(void* frame) {
    float delta = original_frame_delta(frame);
    unsigned int delta_bits;
    DWORD probe_time;
    memcpy(&delta_bits, &delta, sizeof(delta_bits));
    InterlockedIncrement(&snapshot_frame_sequence);
    InterlockedExchange(&snapshot_frame_delta_bits, (LONG)delta_bits);
    InterlockedExchange(&snapshot_frame_time_ms, (LONG)GetTickCount());
#if FANG_SCENARIO
    observe_scenario_dispatch_frame();
    fang_control_frame((uint32_t)InterlockedCompareExchange(&snapshot_frame_sequence, 0, 0));
#endif
    if (InterlockedCompareExchange(&snapshot_keyframe_pending, 2, 1) == 1) {
        InterlockedExchange(&snapshot_keyframe_started_time_ms, (LONG)GetTickCount());
        trace_client_object_registry();
        trace_snapshot_action_state();
        InterlockedExchange(&snapshot_keyframe_completed_time_ms, (LONG)GetTickCount());
        if (snapshot_keyframe_event != NULL) {
            SetEvent(snapshot_keyframe_event);
        }
    }
    probe_time = GetTickCount();
    if (InterlockedCompareExchange(&snapshot_auto_probe_enabled, 0, 0) != 0 &&
        fang_history_begin((unsigned int)InterlockedCompareExchange(&snapshot_frame_sequence, 0, 0),
            delta_bits, (unsigned int)InterlockedCompareExchange(&snapshot_frame_time_ms, 0, 0))) {
        trace_snapshot_action_state();
        fang_history_end();
    }
    if (InterlockedCompareExchange(&snapshot_auto_probe_enabled, 0, 0) != 0 &&
        probe_time - snapshot_last_probe_time >= 250u) {
        snapshot_last_probe_time = probe_time;
        trace_client_object_probe_registry();
    }
    return delta;
}

static void apply_snapshot_control(char* contents, char* last_request) {
    char request[256];
    char* mode;
    char* request_line;
    char* buffer_line;
    char* server_time_line;
    unsigned int buffer_ms = 30000;
    unsigned long long server_time_unix_nano = 0;
    unsigned long long client_received_time_ms = 0;
    ZeroMemory(request, sizeof(request));
    mode = strstr(contents, "mode=");
    if (mode != NULL) {
        mode += 5;
    }
    if (mode != NULL && _strnicmp(mode, "auto", 4) == 0) {
        InterlockedExchange(&snapshot_capture_enabled, 1);
        InterlockedExchange(&snapshot_auto_probe_enabled, 1);
    } else if (mode != NULL && _strnicmp(mode, "manual", 6) == 0) {
        InterlockedExchange(&snapshot_capture_enabled, 1);
        InterlockedExchange(&snapshot_auto_probe_enabled, 1);
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
        strncpy(last_request, request, sizeof(request) - 1);
        last_request[sizeof(request) - 1] = '\0';
        client_received_time_ms = GetTickCount64();
        InterlockedExchange(&snapshot_keyframe_started_time_ms, 0);
        InterlockedExchange(&snapshot_keyframe_completed_time_ms, 0);
        {
            const char* keyframe_status = "unavailable";
            if (snapshot_keyframe_event != NULL) {
                DWORD wait_result;
                ResetEvent(snapshot_keyframe_event);
                InterlockedExchange(&snapshot_keyframe_pending, 1);
                wait_result = WaitForSingleObject(snapshot_keyframe_event, 250);
                InterlockedExchange(&snapshot_keyframe_pending, 0);
                if (wait_result == WAIT_OBJECT_0) {
                    keyframe_status = "complete";
                } else if (wait_result == WAIT_TIMEOUT) {
                    keyframe_status = "timeout";
                } else {
                    keyframe_status = "wait_failed";
                }
            }
            fang_trace_arsenal_snapshot(executable_module);
            snapshot_ring_marker(
                "snapshot_boundary", request, server_time_unix_nano,
                buffer_ms, keyframe_status, client_received_time_ms);
        }
        fang_client103_report(fang_history_write);
        fang_hook_report(fang_history_write);
        snapshot_ring_dump(request, buffer_ms);
    }
}

static int snapshot_control_address(struct sockaddr_in* address) {
    const char* port_text;
    char* port_end;
    unsigned long port;
    size_t index;
    if (strncmp(snapshot_endpoint, "127.0.0.1:", 10) != 0 ||
        strlen(snapshot_token) != 64 || snapshot_directory[0] == '\0') {
        return 0;
    }
    for (index = 0; index < 64; index++) {
        if (!isxdigit((unsigned char)snapshot_token[index])) {
            return 0;
        }
    }
    port_text = snapshot_endpoint + 10;
    if (*port_text == '\0') {
        return 0;
    }
    for (index = 0; port_text[index] != '\0'; index++) {
        if (port_text[index] < '0' || port_text[index] > '9') {
            return 0;
        }
    }
    port = strtoul(port_text, &port_end, 10);
    if (*port_end != '\0' || port == 0 || port > 65535) {
        return 0;
    }
    ZeroMemory(address, sizeof(*address));
    address->sin_family = AF_INET;
    address->sin_addr.s_addr = htonl(INADDR_LOOPBACK);
    address->sin_port = htons((unsigned short)port);
    return 1;
}

// Use unhooked Winsock exports so diagnostics never capture their own control traffic.
static DWORD WINAPI watch_snapshot_control(LPVOID parameter) {
    struct sockaddr_in address;
    WSADATA startup;
    char last_request[256];
    char authentication[66];
    (void)parameter;
    if (!snapshot_control_address(&address) || !fang_network_control_is_ready()) {
        return 0;
    }
    if (WSAStartup(MAKEWORD(2, 2), &startup) != 0) {
        return 0;
    }
    ZeroMemory(last_request, sizeof(last_request));
    memcpy(authentication, snapshot_token, 64);
    authentication[64] = '\n';
    authentication[65] = '\0';
    for (;;) {
        SOCKET control = fang_network_control_open(&address);
        char contents[1024];
        size_t used = 0;
        int sent = 0;
        if (control == INVALID_SOCKET) {
            Sleep(500);
            continue;
        }
        while (sent < 65) {
            int count = fang_network_control_send(control, authentication + sent, 65 - sent, 0);
            if (count <= 0) {
                break;
            }
            sent += count;
        }
        while (sent == 65) {
            char incoming[256];
            int index;
            int count = fang_network_control_recv(control, incoming, sizeof(incoming), 0);
            if (count == SOCKET_ERROR && WSAGetLastError() == WSAETIMEDOUT) {
                continue;
            }
            if (count <= 0) {
                break;
            }
            for (index = 0; index < count; index++) {
                if (used >= sizeof(contents) - 1) {
                    sent = 0;
                    break;
                }
                contents[used++] = incoming[index];
                if (used >= 2 && contents[used - 1] == '\n' && contents[used - 2] == '\n') {
                    contents[used] = '\0';
                    apply_snapshot_control(contents, last_request);
                    used = 0;
                }
            }
        }
        fang_network_control_close(control);
        Sleep(500);
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
    caller_rva = fang_trace_caller_rva(caller);
    if (caller_rva >= 5) {
        caller_rva -= 5;
    }
    base = cost == 0.0 ? 0.0 : cost - (double)property * (double)coefficient;
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
            body_digest = fang_trace_digest(body, (int)body_length);
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
    host_vtable = fang_readable_pointer(pending->host) ? *(void***)pending->host : NULL;
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
static VOID CALLBACK jwt_login_ui_callback(HWND window, UINT message, UINT_PTR timer, DWORD time);

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
        "\"message\":\"The game did not finish signing in. Check your connection and try again from Darkspinner.\","
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
    Sleep(jwt_login_timeout_milliseconds);
    if (InterlockedCompareExchange(&jwt_login_completed, 0, 0) != 0) {
        return 0;
    }
#if FANG_SCENARIO
    trace_client_state("scenario_login_completion_unobserved", 1);
    return 0;
#else
    trace_client_state("jwt_login_watchdog_expired", 1);
    if (write_login_failure_result()) {
        ExitProcess(0xD15C0001u);
    }
    return 0;
#endif
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
    if (fang_readable_pointer(movement)) {
        *movement = 0;
        cleared |= 1;
    }
    if (fang_readable_pointer(ability)) {
        *ability = 0;
        cleared |= 2;
    }
    if (fang_readable_pointer(auxiliary)) {
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
    if (fang_readable_pointer(next_token)) {
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
    fang_trace_camera_state();
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
    /* Native sub_9C2A20 drives both the graphics loop (sub_508AB0) and
     * HELIX active/ended announcements (sub_4E2740). Observe the source
     * flag directly so missing presentation is distinguishable from a
     * meter-only update. This never activates Overdrive or plays effects. */
    if (readable_range(player, 4973)) {
        trace_client_state("overdrive_active", player[4676]);
        trace_client_state("overdrive_energy_bits", *(const unsigned int*)(player + 4668));
        trace_client_state("overdrive_drain_bits", *(const unsigned int*)(player + 4672));
        trace_client_state("overdrive_locked", player[4972]);
    }
}

static unsigned int reset_combat_input_state(void) {
    void* state = original_combat_input_state != NULL ? original_combat_input_state() : NULL;
    trace_input_action_state("input_action_reset_before");
    if (original_combat_input_reset == NULL || !fang_readable_pointer(state) ||
        !fang_readable_pointer((const BYTE*)state + 584)) {
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

static LRESULT CALLBACK hooked_game_wndproc(HWND window, UINT message, WPARAM wparam, LPARAM lparam) {
    static const UINT_PTR chat_timer = 0xD45C;
#if FANG_SCENARIO
    /* Scenario input must traverse the installed compatibility handler too,
     * including ordinary Enter-to-open-chat behavior. Synthetic input messages
     * are not the private wake message, so they fall through the control probe. */
    if (fang_control_window_message(window, message, wparam, lparam, hooked_game_wndproc)) return 0;
#endif
    if (InterlockedCompareExchange(&snapshot_capture_enabled, 0, 0) != 0) {
        fang_history_input(message, wparam);
    }
    if (message == WM_KEYDOWN && (wparam == 'V' || wparam == 'v') &&
        (GetKeyState(VK_CONTROL) & 0x8000) != 0) {
        void* chat = original_chat_lookup();
        if (chat != NULL && *((const BYTE*)chat + 41) != 0) {
            unsigned int pasted = paste_chat_clipboard(window);
            trace_client_state("chat_paste", pasted);
            return 0;
        }
    }
    if ((message == WM_KEYDOWN && wparam == VK_RETURN && (lparam & (1L << 30)) == 0 &&
        (GetKeyState(VK_MENU) & 0x8000) == 0) ||
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
    return CallWindowProcA(original_game_wndproc, window, message, wparam, lparam);
}

static void install_chat_wndproc(HWND window) {
    WNDPROC current = (WNDPROC)GetWindowLongPtrA(window, GWLP_WNDPROC);
    if (current == hooked_game_wndproc) {
        chat_game_window = window;
#if FANG_SCENARIO
        fang_control_window_attach(window);
#endif
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
#if FANG_SCENARIO
    fang_control_window_attach(window);
#endif
    trace_client_state("chat_wndproc", 1);
}

static DWORD WINAPI poll_chat_key(LPVOID parameter) {
    LONG is_return_down = 0;
    (void)parameter;
    trace_client_state("chat_key_poll", 1);
    for (;;) {
        HWND window = chat_game_window;
        SHORT return_state = GetAsyncKeyState(VK_RETURN);
        if ((return_state & 0x8000) == 0) {
            is_return_down = 0;
        } else if (is_return_down == 0) {
            is_return_down = 1;
            if (window != NULL && GetForegroundWindow() == window &&
                (GetAsyncKeyState(VK_MENU) & 0x8000) == 0) {
                trace_client_state("chat_enter_poll", 1);
                PostMessageA(window, CHAT_OPEN_MESSAGE, 0, 0);
            }
        }
        /* Native input owns basic-attack repetition and release. Diagnostics
         * must not clear that state based on a physical mouse-button guess. */
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
    if (fang_readable_pointer(hud)) {
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
    if (fang_readable_pointer(entry)) {
        start = *(const uint64_t*)entry;
        end = *(const uint64_t*)((const BYTE*)entry + 8);
        is_active = *((const BYTE*)entry + 16);
    }
    clock_root = original_clock_root();
    clock_owner = original_clock_state(clock_root);
    if (fang_readable_pointer(clock_owner) && fang_readable_pointer(*(void**)clock_owner)) {
        clock_state = (const BYTE*)(*(void**)clock_owner) + 0x30;
    }
    if (fang_readable_pointer(clock_state)) {
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
    unsigned int network_object_id = 0;
    if ((id == 0x91 || id == 0x94 || id == 0x95) && data != NULL && size >= 4) {
        memcpy(&network_object_id, data, sizeof(network_object_id));
        InterlockedExchange(
            &snapshot_pending_network_object_id, (LONG)network_object_id);
    } else if (id == 0x8e && data != NULL && size >= 4) {
        memcpy(&network_object_id, data, sizeof(network_object_id));
        forget_object_association(network_object_id);
    }
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
    if (fang_readable_pointer(clock_owner) && fang_readable_pointer(*(void**)clock_owner)) {
        clock_state = (const BYTE*)(*(void**)clock_owner) + 0x30;
    }
    if (fang_readable_pointer(clock_state)) {
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
    if (!fang_readable_pointer(metadata)) {
        trace_client_state("player_reflection_missing", player_type_id);
        return;
    }
    field = *(const unsigned char* const*)(metadata + 0x08);
    field_count = *(const unsigned int*)(metadata + 0x0C);
    if (!fang_readable_pointer(field) || field_count > 256) {
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
    if (!fang_readable_pointer(manager)) {
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
    char render_position[96] = "null";
    const BYTE* render;
    const char suffix[] = "\"}\r\n";
    int length;
    unsigned int network_object_id;
    if (!readable_range(object, 668)) {
        return;
    }
    // ServerEvent presentation uses the render transform when one exists,
    // rather than the simulation position already captured below.
    render = *(const BYTE* const*)(object + 652);
    if (readable_range(render, 28)) {
        snprintf(render_position, sizeof(render_position), "[%u,%u,%u]",
            *(const unsigned int*)(render + 16),
            *(const unsigned int*)(render + 20),
            *(const unsigned int*)(render + 24));
    }
    network_object_id = mapped_network_object_id(object_id);
    length = snprintf(header, sizeof(header),
        "{\"time_ms\":%llu,\"protocol\":\"client_state\","
        "\"kind\":\"object_memory\",\"phase\":\"snapshot_boundary\","
        "\"thread\":%lu,\"frame_sequence\":%lu,"
        "\"frame_delta_bits\":%lu,\"frame_time_ms\":%lu,\"object_id\":%u,"
        "\"network_object_id\":%u,"
        "\"position_bits\":[%u,%u,%u],\"render_position_bits\":%s,\"object_size\":668,"
        "\"object_hex\":\"",
        (unsigned long long)GetTickCount64(),
        (unsigned long)GetCurrentThreadId(),
        (unsigned long)InterlockedCompareExchange(&snapshot_frame_sequence, 0, 0),
        (unsigned long)InterlockedCompareExchange(&snapshot_frame_delta_bits, 0, 0),
        (unsigned long)InterlockedCompareExchange(&snapshot_frame_time_ms, 0, 0),
        object_id, network_object_id, *(const unsigned int*)(object + 24),
        *(const unsigned int*)(object + 28),
        *(const unsigned int*)(object + 32), render_position);
    if (length <= 0 || (size_t)length >= sizeof(header)) {
        return;
    }
    trace_hex_line(header, length, object, 668, suffix);
}

static void trace_client_object_probe(
    unsigned int object_id, const BYTE* object) {
    char line[1024];
    const BYTE* locomotion;
    FILETIME sample_time;
    ULARGE_INTEGER sample_ticks;
    DWORD written;
    int length;
    if (!readable_range(object, 668)) {
        return;
    }
    locomotion = *(const BYTE* const*)(object + 664);
    if (!readable_range(locomotion, 440)) {
        return;
    }
    GetSystemTimeAsFileTime(&sample_time);
    sample_ticks.LowPart = sample_time.dwLowDateTime;
    sample_ticks.HighPart = sample_time.dwHighDateTime;
    length = snprintf(line, sizeof(line),
        "{\"time_ms\":%llu,\"protocol\":\"client_state\","
        "\"kind\":\"object_probe\",\"phase\":\"automatic\","
        "\"thread\":%lu,\"frame_sequence\":%lu,"
        "\"frame_delta_bits\":%lu,\"frame_time_ms\":%lu,\"object_id\":%u,"
        "\"sample_unix_nano\":%llu,\"position_bits\":[%u,%u,%u],"
        "\"flags\":%u,\"goal_bits\":[%u,%u,%u],"
        "\"partial_goal_bits\":[%u,%u,%u],\"target_object_id\":%u,"
        "\"desired_stop_bits\":%u,\"graphics_component_pointer\":%u,"
        "\"is_graphics_component_present\":%s,\"last_animation_state\":%u,"
        "\"last_animation_play_milliseconds\":%llu}\r\n",
        (unsigned long long)GetTickCount64(),
        (unsigned long)GetCurrentThreadId(),
        (unsigned long)InterlockedCompareExchange(&snapshot_frame_sequence, 0, 0),
        (unsigned long)InterlockedCompareExchange(&snapshot_frame_delta_bits, 0, 0),
        (unsigned long)InterlockedCompareExchange(&snapshot_frame_time_ms, 0, 0),
        object_id, (sample_ticks.QuadPart - 116444736000000000ULL) * 100ULL,
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
        *(const unsigned int*)(locomotion + 416),
        *(const unsigned int*)(object + 652),
        *(const unsigned int*)(object + 652) != 0 ? "true" : "false",
        *(const unsigned int*)(object + 172),
        (unsigned long long)*(const uint64_t*)(object + 184));
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

#if FANG_SCENARIO
static void trace_scenario_registry(const char* status,
    ULONGLONG started_time_ms, unsigned int object_count,
    unsigned int unreadable_slot_count, unsigned int changed_handle_count,
    unsigned long started_message_count) {
    char line[768];
    int length = fang_scenario_registry_line(line, sizeof(line), status,
        started_time_ms, GetTickCount64(),
        (unsigned long)InterlockedCompareExchange(&snapshot_frame_sequence, 0, 0),
        object_count, unreadable_slot_count, changed_handle_count,
        started_message_count);
    if (length <= 0 || (size_t)length >= sizeof(line)) {
        return;
    }
    snapshot_ring_append(line, (unsigned int)length);
}
#endif

static void trace_client_object_registry(void) {
    const BYTE* registry;
    const BYTE* table;
    const BYTE* slots;
    unsigned int capacity;
    unsigned int stride;
    unsigned int index;
    unsigned int captured = 0;
#if FANG_SCENARIO
    ULONGLONG started_time_ms = GetTickCount64();
    unsigned int unreadable_slot_count = 0;
    unsigned int changed_handle_count = 0;
    unsigned long started_message_count = fang_scenario_message_count();
#endif
    if (InterlockedCompareExchange(&snapshot_capture_enabled, 0, 0) == 0 ||
        original_game_object_registry == NULL) {
#if FANG_SCENARIO
        trace_scenario_registry("unavailable", started_time_ms, 0, 0, 0, started_message_count);
#endif
        return;
    }
    registry = (const BYTE*)original_game_object_registry();
    if (!readable_range(registry, 8)) {
#if FANG_SCENARIO
        trace_scenario_registry("unreadable_registry", started_time_ms, 0, 0, 0, started_message_count);
#endif
        return;
    }
    table = *(const BYTE* const*)(registry + 4);
    if (!readable_range(table, 24)) {
#if FANG_SCENARIO
        trace_scenario_registry("unreadable_table", started_time_ms, 0, 0, 0, started_message_count);
#endif
        return;
    }
    slots = *(const BYTE* const*)table;
    capacity = *(const unsigned int*)(table + 16);
    stride = *(const unsigned int*)(table + 20);
    if (slots == NULL || capacity == 0 || capacity > 65536 ||
        stride < 668 || stride > 16384 ||
        (size_t)(capacity - 1) >
            (SIZE_MAX - (size_t)(uintptr_t)slots) / stride) {
#if FANG_SCENARIO
        trace_scenario_registry("invalid_table", started_time_ms, 0, 0, 0, started_message_count);
#endif
        return;
    }
    for (index = 0; index < capacity; index++) {
        const BYTE* object = slots + (size_t)index * stride;
        const BYTE* locomotion;
        unsigned int object_id;
        if (!readable_range(object, 668)) {
#if FANG_SCENARIO
            unreadable_slot_count++;
#endif
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
#if FANG_SCENARIO
        if (!readable_range(object, 668) ||
            *(const unsigned int*)object != object_id) {
            changed_handle_count++;
        }
#endif
    }
    trace_client_state("snapshot_object_count", captured);
#if FANG_SCENARIO
    trace_scenario_registry("observed", started_time_ms, captured,
        unreadable_slot_count, changed_handle_count, started_message_count);
#endif
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
    unsigned int network_object_id = (unsigned int)InterlockedExchange(
        &snapshot_pending_network_object_id, 0);
    trace_client_state("locomotion_resolve_object", object_id);
    if (!readable_range(result, 668)) {
        trace_client_state("locomotion_resolve_missing", object_id);
        return result;
    }
    object = (const BYTE*)result;
    if (network_object_id != 0) {
        remember_object_association(network_object_id, object_id);
    }
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
    InterlockedExchange(&snapshot_pending_network_object_id, 0);
    trace_resolved_movement_context("after_apply");
    reset_movement_trace_context();
    return result;
}

static void* __stdcall hooked_locomotion_update_receiver(void* message) {
    void* result;
    reset_movement_trace_context();
    result = original_locomotion_update_receiver(message);
    InterlockedExchange(&snapshot_pending_network_object_id, 0);
    trace_resolved_movement_context("after_apply");
    reset_movement_trace_context();
    return result;
}

static void* __stdcall hooked_locomotion_unreliable_receiver(void* message) {
    void* result;
    reset_movement_trace_context();
    result = original_locomotion_unreliable_receiver(message);
    InterlockedExchange(&snapshot_pending_network_object_id, 0);
    trace_resolved_movement_context("after_apply");
    reset_movement_trace_context();
    return result;
}

static void observe_controlled_resource_update(
    unsigned char id, const void* data, unsigned int size) {
    const BYTE* payload = (const BYTE*)data;
    unsigned int object_id;
    unsigned int controlled_object_id;
    unsigned int candidate_object_id;
    unsigned int offset = 5;
    unsigned int mask;
    if (id != 0x97 || payload == NULL || size < 5) {
        return;
    }
    memcpy(&object_id, payload, sizeof(object_id));
    controlled_object_id = (unsigned int)InterlockedCompareExchange(
        &diagnostic_controlled_object_id, 0, 0);
    if (object_id == 0) {
        return;
    }
    mask = payload[4];
    candidate_object_id = (unsigned int)InterlockedExchange(
        &diagnostic_candidate_resource_object_id, (LONG)object_id);
    if (candidate_object_id != object_id) {
        InterlockedExchange(&diagnostic_candidate_resource_mask, 0);
    }
    if ((mask & 1u) != 0 && size >= offset + sizeof(unsigned int)) {
        unsigned int hit_point_bits;
        memcpy(&hit_point_bits, payload + offset, sizeof(hit_point_bits));
        InterlockedExchange(&diagnostic_candidate_hit_point_bits, (LONG)hit_point_bits);
        InterlockedOr(&diagnostic_candidate_resource_mask, 1);
        if (object_id == controlled_object_id || controlled_object_id == 0) {
            InterlockedExchange(&diagnostic_hit_point_bits, (LONG)hit_point_bits);
            InterlockedOr(&diagnostic_resource_mask, 1);
        }
        offset += sizeof(unsigned int);
    }
    if ((mask & 2u) != 0 && size >= offset + sizeof(unsigned int)) {
        unsigned int power_point_bits;
        memcpy(&power_point_bits, payload + offset, sizeof(power_point_bits));
        InterlockedExchange(&diagnostic_candidate_power_point_bits, (LONG)power_point_bits);
        InterlockedOr(&diagnostic_candidate_resource_mask, 2);
        if (object_id == controlled_object_id || controlled_object_id == 0) {
            InterlockedExchange(&diagnostic_power_point_bits, (LONG)power_point_bits);
            InterlockedOr(&diagnostic_resource_mask, 2);
        }
    }
    if (object_id == controlled_object_id || controlled_object_id == 0) {
        InterlockedExchange(&diagnostic_resource_object_id, (LONG)object_id);
    }
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
#if FANG_SCENARIO
    if (id >= 0x8C && id <= 0x95 &&
        InterlockedCompareExchange(&snapshot_capture_enabled, 0, 0) != 0) {
        char line[512];
        int length = fang_scenario_message_line(line, sizeof(line), id,
            GetTickCount64(),
            (unsigned long)InterlockedCompareExchange(&snapshot_frame_sequence, 0, 0),
            (unsigned long)GetCurrentThreadId());
        if (length > 0 && (size_t)length < sizeof(line)) {
            snapshot_ring_append(line, (unsigned int)length);
        }
    }
#endif
    if (InterlockedExchange(&action_response_trace_pending, 0) != 0 &&
        original_combat_input_state != NULL) {
        const BYTE* state = (const BYTE*)original_combat_input_state();
        if (fang_readable_pointer(state) && fang_readable_pointer(state + 60)) {
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
    observe_controlled_resource_update(id, data, size);
    if (id == 0xA1) {
        if (data != NULL && size == 9 &&
            *((const unsigned char*)data + 1) == 0x00 &&
            *((const unsigned char*)data + 2) == 0x10 &&
            *((const unsigned char*)data + 3) == 0x09 &&
            *((const unsigned char*)data + 8) == 0xFF) {
            unsigned int controlled_object_id;
            LONG previous_controlled_object_id;
            memcpy(&controlled_object_id, (const BYTE*)data + 4, sizeof(controlled_object_id));
            previous_controlled_object_id = InterlockedExchange(
                &diagnostic_controlled_object_id,
                (LONG)controlled_object_id);
            if ((unsigned int)InterlockedCompareExchange(
                    &diagnostic_candidate_resource_object_id, 0, 0) == controlled_object_id) {
                InterlockedExchange(&diagnostic_resource_object_id, (LONG)controlled_object_id);
                InterlockedExchange(&diagnostic_hit_point_bits,
                    InterlockedCompareExchange(&diagnostic_candidate_hit_point_bits, 0, 0));
                InterlockedExchange(&diagnostic_power_point_bits,
                    InterlockedCompareExchange(&diagnostic_candidate_power_point_bits, 0, 0));
                InterlockedExchange(&diagnostic_resource_mask,
                    InterlockedCompareExchange(&diagnostic_candidate_resource_mask, 0, 0));
            } else if ((unsigned int)InterlockedCompareExchange(
                           &diagnostic_resource_object_id, 0, 0) != controlled_object_id &&
                (unsigned int)previous_controlled_object_id != controlled_object_id) {
                InterlockedExchange(&diagnostic_resource_object_id, 0);
                InterlockedExchange(&diagnostic_resource_mask, 0);
            }
            trace_client_state("combat_controlled_binding", controlled_object_id);
        }
        trace_player_reflection();
    }
    if (id == 0xBA && original_controlled_player != NULL) {
        trace_combat_text_assets();
        trace_combat_text_preferences();
        const BYTE* player = (const BYTE*)original_controlled_player();
        if (fang_readable_pointer(player) && fang_readable_pointer(player + 4664)) {
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
    if (!fang_readable_pointer(registry)) {
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
    if (!fang_readable_pointer(effect_system)) {
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

static int normalize_fang_stat_command(
    const char* text, const char* command_text, char* output, size_t output_capacity) {
    size_t prefix_length;
    unsigned int object_id;
    unsigned int resource_object_id;
    unsigned int hit_point_bits;
    unsigned int power_point_bits;
    unsigned int resource_mask;
    int written;
    if (!fang_readable_pointer(text) || command_text < text || output == NULL || output_capacity == 0) {
        return -1;
    }
    prefix_length = (size_t)(command_text - text);
    if (prefix_length >= output_capacity) {
        return -1;
    }
    if (prefix_length != 0) {
        memcpy(output, text, prefix_length);
    }
    object_id = (unsigned int)InterlockedCompareExchange(
        &diagnostic_controlled_object_id, 0, 0);
    resource_object_id = (unsigned int)InterlockedCompareExchange(
        &diagnostic_resource_object_id, 0, 0);
    hit_point_bits = (unsigned int)InterlockedCompareExchange(
        &diagnostic_hit_point_bits, 0, 0);
    power_point_bits = (unsigned int)InterlockedCompareExchange(
        &diagnostic_power_point_bits, 0, 0);
    resource_mask = (unsigned int)InterlockedCompareExchange(
        &diagnostic_resource_mask, 0, 0);
    if (object_id == 0 || resource_object_id != object_id) {
        resource_mask = 0;
    }
    written = snprintf(output + prefix_length, output_capacity - prefix_length,
        "/stat %u %u %u %u", object_id, hit_point_bits, power_point_bits,
        resource_mask & 3u);
    if (written < 0 || (size_t)written >= output_capacity - prefix_length) {
        return -1;
    }
    return (int)prefix_length + written;
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
    if (command == 29) {
        converted_length = normalize_fang_stat_command(
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

/* The movie may construct/reset input widgets after its native controller
 * initializes. Keep the display-only identity synchronized on the UI thread
 * until login completes, rather than relying on a single pre-frame write. */
static VOID CALLBACK jwt_login_ui_callback(HWND window, UINT message, UINT_PTR timer, DWORD time) {
    unsigned int screen_type;
    (void)window;
    (void)message;
    (void)time;
    if (timer != jwt_login_ui_timer) return;
    if (InterlockedCompareExchange(&jwt_login_completed, 0, 0) != 0 ||
        GetTickCount64() - jwt_login_ui_started_ms >= 2 * jwt_login_timeout_milliseconds ||
        !readable_range(jwt_login_controller, 40)) {
        KillTimer(NULL, timer);
        jwt_login_ui_timer = 0;
        jwt_login_controller = NULL;
        return;
    }
    screen_type = *(unsigned int*)((BYTE*)jwt_login_controller + 28) & 0x8f;
    if (*(void**)jwt_login_controller != jwt_login_controller_type || screen_type < 6 || screen_type > 8) {
        KillTimer(NULL, timer);
        jwt_login_ui_timer = 0;
        jwt_login_controller = NULL;
        return;
    }
    fang_login_processing(jwt_login_controller);
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
    (void)unused;
    if (jwt_login_token[0] != '\0' && readable_range(controller, 106)) {
        /* Build 103 controller+105 enables native remembered-password login.
         * The launcher token is the sole authentication owner for this run. */
        *((BYTE*)controller + 105) = 0;
    }
    original_login_screen_init(controller, manager);
    if (jwt_login_token[0] == '\0' || manager == NULL) {
        return;
    }
    jwt_login_manager = manager;
    jwt_login_controller = controller;
    jwt_login_controller_type = readable_range(controller, sizeof(void*)) ? *(void**)controller : NULL;
    trace_client_state("jwt_login_processing", (unsigned int)fang_login_processing(controller));
    if (jwt_login_ui_timer == 0) {
        jwt_login_ui_started_ms = GetTickCount64();
        jwt_login_ui_timer = SetTimer(NULL, 0, 50, jwt_login_ui_callback);
        if (jwt_login_ui_timer == 0) trace_client_state("jwt_login_ui_timer_failed", GetLastError());
    }
    methods = fang_readable_pointer(manager) ? *(void***)manager : NULL;
    if (fang_readable_pointer(methods)) {
        ready = (login_ready_fn)methods[1];
        submit = (login_submit_fn)methods[2];
        if (ready != NULL && submit != NULL && ready(manager) &&
            InterlockedCompareExchange(&jwt_login_started, 1, 0) == 0) {
            fang_login_processing(controller);
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
    jwt_login_ready_started_ms = GetTickCount64();
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
    if (GetTickCount64() - jwt_login_ready_started_ms >= jwt_login_timeout_milliseconds ||
        jwt_login_token[0] == '\0' || !fang_readable_pointer(manager)) {
        KillTimer(NULL, timer);
        jwt_login_timer = 0;
        trace_client_state("jwt_login_retry_expired", jwt_login_retry_count);
#if !FANG_SCENARIO
        if (jwt_login_token[0] != '\0' && fang_login_not_ready()) {
            ExitProcess(0xD15C0001u);
        }
#endif
        return;
    }
    methods = *(void***)manager;
    if (!fang_readable_pointer(methods)) {
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
    trace_client_state("jwt_login_processing", (unsigned int)fang_login_processing(jwt_login_controller));
    trace_client_state("jwt_login_submit", 1);
    trace_client_state("jwt_login_length", (unsigned int)strlen(jwt_login_token));
    submit(manager, "token@local.invalid", jwt_login_token);
    start_jwt_login_watchdog();
    /* The build-103 submitter queues serialization and may retain the PASS
       pointer after this call returns. Keep the process-private buffer alive
       so the queued login cannot observe a cleared credential. */
}

void fang_history_write(const char* line, unsigned int length) {
    DWORD written;
    if (trace_file == INVALID_HANDLE_VALUE || !trace_lock_ready) return;
    EnterCriticalSection(&trace_lock);
    WriteFile(trace_file, line, length, &written, NULL);
    LeaveCriticalSection(&trace_lock);
    if (InterlockedCompareExchange(&snapshot_capture_enabled, 0, 0) != 0) {
        snapshot_ring_append(line, length);
    }
}

void trace_client_state(const char* kind, unsigned int value) {
    char line[512];
    DWORD written;
    int length;
    if (fang_history_collect(kind, value)) return;
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
    if (count == 0 || count > 256 || !fang_readable_pointer(begin) ||
        !fang_readable_pointer(begin + count - 1)) {
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
    if (!fang_readable_pointer(begin) || !fang_readable_pointer(end) || end <= begin) {
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
    if (!fang_readable_pointer(manager)) {
        return 0;
    }
    manager_method = *(void***)manager;
    if (!fang_readable_pointer(manager_method) || !fang_readable_pointer(manager_method + 11)) {
        return 0;
    }
    lookup = (resource_lookup_fn)manager_method[11];
    if (!fang_readable_pointer((const void*)lookup)) {
        return 0;
    }
    lookup(manager, 0x3B01D7F6, 0x3B01D7F6, &resource);
    if (!fang_readable_pointer(resource)) {
        return 0;
    }
    is_read = original_property_vector_read(resource, 0xC0B32F0F, &count, &value);
    if (is_read) {
        is_traced = trace_xp_threshold_values(value, count);
    }
    resource_method = *(void***)resource;
    if (fang_readable_pointer(resource_method) && fang_readable_pointer(resource_method + 1)) {
        release = (resource_release_fn)resource_method[1];
        if (fang_readable_pointer((const void*)release)) {
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

static void trace_creature_resource_lookup(const char* kind, const void* resource,
    const void* result, unsigned int nested_handle) {
    char line[512];
    DWORD written;
    int length;
    if (InterlockedCompareExchange(&snapshot_capture_enabled, 0, 0) == 0 &&
        (trace_file == INVALID_HANDLE_VALUE || !trace_lock_ready)) {
        return;
    }
    length = snprintf(line, sizeof(line),
        "{\"time_ms\":%llu,\"protocol\":\"client_scene\",\"kind\":\"%s\","
        "\"resource_pointer\":%u,\"result_pointer\":%u,\"nested_handle\":%u}\r\n",
        (unsigned long long)GetTickCount64(), kind,
        (unsigned int)(uintptr_t)resource, (unsigned int)(uintptr_t)result, nested_handle);
    if (length <= 0 || (size_t)length >= sizeof(line)) {
        return;
    }
    if (trace_file != INVALID_HANDLE_VALUE && trace_lock_ready) {
        EnterCriticalSection(&trace_lock);
        WriteFile(trace_file, line, (DWORD)length, &written, NULL);
        LeaveCriticalSection(&trace_lock);
    }
    if (InterlockedCompareExchange(&snapshot_capture_enabled, 0, 0) != 0) {
        snapshot_ring_append(line, (unsigned int)length);
    }
}

static void* __cdecl hooked_creature_asset_lookup(void* resource, void* output) {
    /* Build 103's 9ECA10 tail-jumps to 9C8E70: resource and output arguments.
       Neither the property return nor its nested handle proves model readiness. */
    void* property = original_creature_asset_lookup(resource, output);
    DWORD native_error = GetLastError();
    unsigned int nested_handle = 0;
    if (InterlockedCompareExchange(&snapshot_capture_enabled, 0, 0) == 0 &&
        (trace_file == INVALID_HANDLE_VALUE || !trace_lock_ready)) {
        SetLastError(native_error);
        return property;
    }
    if (readable_range(property, 0xA8)) {
        nested_handle = *(const unsigned int*)((const BYTE*)property + 0xA4);
    }
    trace_creature_resource_lookup("creature_resource_property_lookup", resource,
        property, nested_handle);
    SetLastError(native_error);
    return property;
}

static void* __cdecl hooked_creature_nested_lookup(void* asset, void* output) {
    void* result = original_creature_nested_lookup(asset, output);
    DWORD native_error = GetLastError();
    trace_creature_resource_lookup("creature_resource_nested_lookup", asset, result, 0);
    SetLastError(native_error);
    return result;
}

static void __cdecl hooked_scene_load(void* scene, unsigned int markerset, unsigned int index) {
    unsigned int asset_id = traced_scene_id(scene);
    trace_scene_state("scene_load_request", asset_id, markerset, index, asset_id != 0);
    original_scene_load(scene, markerset, index);
#if FANG_SCENARIO
    fang_control_scene(asset_id, (uint32_t)InterlockedCompareExchange(&snapshot_frame_sequence, 0, 0));
    if (InterlockedCompareExchange(&snapshot_capture_enabled, 0, 0) != 0) {
        char line[512];
        unsigned int load_argument_0 = markerset;
        unsigned int load_argument_1 = index;
        int length = fang_scenario_scene_load_line(line, sizeof(line),
            asset_id, load_argument_0, load_argument_1, GetTickCount64(),
            (unsigned long)InterlockedCompareExchange(&snapshot_frame_sequence, 0, 0),
            (unsigned long)GetCurrentThreadId());
        if (length > 0 && (size_t)length < sizeof(line)) {
            snapshot_ring_append(line, (unsigned int)length);
        }
    }
#endif
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

uintptr_t fang_trace_caller_rva(void* caller) {
    if (executable_module == NULL || caller == NULL) {
        return 0;
    }
    return (uintptr_t)((BYTE*)caller - (BYTE*)executable_module);
}

uint64_t fang_trace_digest(const char* buffer, int length) {
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

static int WINAPI hooked_messageboxw(HWND window, LPCWSTR text, LPCWSTR caption, UINT type) {
    trace_alert_event(text, fang_trace_caller_rva(__builtin_return_address(0)));
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
    trace_alert_event(wide, fang_trace_caller_rva(__builtin_return_address(0)));
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
    uintptr_t caller_rva = fang_trace_caller_rva(__builtin_return_address(0));
    DWORD result = original_getfileattributesw(file_name);
    DWORD error = result == INVALID_FILE_ATTRIBUTES ? GetLastError() : 0;
    if (caller_rva == 0xAF2E85) {
        trace_path_event(file_name, caller_rva, result, error);
    }
    SetLastError(error);
    return result;
}

static void __cdecl hooked_internal_alert(const char* message, int code, int severity) {
    trace_internal_alert_event(message, code, severity, fang_trace_caller_rva(__builtin_return_address(0)));
    original_alert(message, code, severity);
}

static void __cdecl hooked_login_error(unsigned int code, void* target) {
    LONG was_login_completed = InterlockedExchange(&jwt_login_completed, 1);
    trace_login_error_event(code, fang_trace_caller_rva(__builtin_return_address(0)));
#if !FANG_SCENARIO
    if (!was_login_completed && jwt_login_token[0] != '\0' && fang_login_rejected(code)) {
        /* The owning launcher retains the themed error after the client
         * closes, rather than routing an automatic launch to manual login. */
        ExitProcess(0xD15C0002u);
    }
#endif
    (void)was_login_completed;
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
    registry_branch = fang_find_signature(
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
        trace_patches += fang_network_install_module(modules[index], &dns_patches, &connect_patches);
        if (original_messageboxw != NULL) {
            trace_patches += fang_patch_import(modules[index], "USER32.dll", "MessageBoxW", 0, (void*)hooked_messageboxw, NULL);
            trace_patches += fang_patch_import_address(modules[index], (void*)original_messageboxw, (void*)hooked_messageboxw, NULL);
        }
        if (original_messageboxa != NULL) {
            trace_patches += fang_patch_import(modules[index], "USER32.dll", "MessageBoxA", 0, (void*)hooked_messageboxa, NULL);
            trace_patches += fang_patch_import_address(modules[index], (void*)original_messageboxa, (void*)hooked_messageboxa, NULL);
        }
        if (original_getfileattributesw != NULL) {
            trace_patches += fang_patch_import(modules[index], "KERNEL32.dll", "GetFileAttributesW", 0, (void*)hooked_getfileattributesw, NULL);
            trace_patches += fang_patch_import_address(modules[index], (void*)original_getfileattributesw, (void*)hooked_getfileattributesw, NULL);
        }
    }
    (void)trace_patches;
    return (dns_patches > 0 ? 0 : (1 | (int)fang_network_diagnostics())) | (connect_patches > 0 ? 0 : 2);
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
    /* Match the shipped title before branding so the chat handler can attach. */
    if (_strnicmp(current_title, "Darkspore", 9) != 0 && _strnicmp(current_title, "Dark Spin", 9) != 0) {
        return TRUE;
    }
    install_chat_wndproc(window);
    fang_set_display_window(window);
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
    const char* trace_path, int skip_intro, int skip_cinematic,
    const char* jwt, const char* window_title) {
    static const BYTE ssl_ctx_signature[] = {0x8B, 0x44, 0x24, 0x04, 0x8B, 0x4C, 0x24, 0x08, 0x8B, 0x54, 0x24, 0x0C, 0x89, 0x88};
    static const BYTE ssl_result_signature[] = {0x8B, 0x44, 0x24, 0x04, 0x8B, 0x80, 0xE0, 0x00, 0x00, 0x00, 0xC3};
    static const BYTE wildcard_signature[] = {0x53, 0x56, 0x8B, 0x74, 0x24, 0x10, 0x57, 0x8B, 0x7C, 0x24, 0x10, 0xEB, 0x03};
    static const BYTE certificate_signature[] = {0x83, 0x7C, 0x24, 0x04, 0x00, 0x56, 0x57, 0x8B, 0xF0};
    static const BYTE secure_connect_signature[] = {0x8B, 0x5C, 0x24, 0x14, 0x8B, 0xC6, 0x89, 0xBE, 0x20, 0x01, 0x00, 0x00, 0xC7, 0x86, 0x24, 0x01, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00};
    static const BYTE force_plaintext[] = {0x31, 0xDB, 0x90, 0x90};
    HMODULE executable = GetModuleHandleA(NULL);
    HMODULE winsock = GetModuleHandleA("ws2_32.dll");
    int is_network_ready;
    HMODULE user32 = GetModuleHandleA("user32.dll");
    HMODULE kernel32_module = GetModuleHandleA("kernel32.dll");
    int result = 0;
    int is_bootstrap_bound = 1;
    fang_client103_image client_image;
    int alert_patches = 0;
    int login_error_patches = 0;
    int cinematic_trace_patches = 0;
    int scene_trace_patches = 0;
    int snapshot_trace_patches = 0;
    int loot_trace_patches = 0;
    int mana_cost_patches = 0;
    char snapshot_mode[16];
#if FANG_DIAGNOSTICS
    if (trace_path != NULL && trace_path[0] != '\0') {
        InitializeCriticalSection(&trace_lock);
        trace_lock_ready = 1;
        trace_file = CreateFileA(trace_path, GENERIC_WRITE, FILE_SHARE_READ | FILE_SHARE_WRITE,
            NULL, CREATE_ALWAYS, FILE_ATTRIBUTE_NORMAL, NULL);
    }
#else
    (void)trace_path;
#endif
    if (!fang_client103_image_open(executable, 0, &client_image)) {
        fang_client103_report(fang_history_write);
        return 0x8;
    }
    fang_network_reset_diagnostics();
    executable_module = executable;
    ZeroMemory(snapshot_mode, sizeof(snapshot_mode));
    GetEnvironmentVariableA(
        "DARKSPIN_SNAPSHOT_MODE", snapshot_mode, (DWORD)sizeof(snapshot_mode));
    ZeroMemory(snapshot_directory, sizeof(snapshot_directory));
    ZeroMemory(snapshot_endpoint, sizeof(snapshot_endpoint));
    ZeroMemory(snapshot_token, sizeof(snapshot_token));
    if (GetEnvironmentVariableA("DARKSPIN_SNAPSHOT_DIRECTORY", snapshot_directory,
        (DWORD)sizeof(snapshot_directory)) >= sizeof(snapshot_directory)) {
        snapshot_directory[0] = '\0';
    }
    if (GetEnvironmentVariableA("DARKSPIN_SNAPSHOT_ENDPOINT", snapshot_endpoint,
        (DWORD)sizeof(snapshot_endpoint)) >= sizeof(snapshot_endpoint)) {
        snapshot_endpoint[0] = '\0';
    }
    if (GetEnvironmentVariableA("DARKSPIN_SNAPSHOT_TOKEN", snapshot_token,
        (DWORD)sizeof(snapshot_token)) != 64) {
        snapshot_token[0] = '\0';
    }
    InterlockedExchange(&snapshot_capture_enabled,
        (_stricmp(snapshot_mode, "manual") == 0 ||
         _stricmp(snapshot_mode, "auto") == 0) ? 1 : 0);
    InterlockedExchange(&snapshot_auto_probe_enabled,
        (_stricmp(snapshot_mode, "manual") == 0 ||
         _stricmp(snapshot_mode, "auto") == 0) ? 1 : 0);
    if (movement_trace_tls_index == TLS_OUT_OF_INDEXES) {
        movement_trace_tls_index = TlsAlloc();
    }
    GetEnvironmentVariableA("DARKSPIN_CLIENT_RESULT", client_result_path,
        (DWORD)sizeof(client_result_path));
    GetEnvironmentVariableA("DARKSPIN_CLIENT_LAUNCH_ID", client_launch_id,
        (DWORD)sizeof(client_launch_id));
    original_alert = FANG103_BIND(&client_image, &is_bootstrap_bound, alert_fn, original_alert, FANG103_ORIGINAL_ALERT_RVA);
    original_login_error = FANG103_BIND(&client_image, &is_bootstrap_bound, login_error_fn, original_login_error, FANG103_ORIGINAL_LOGIN_ERROR_RVA);
    original_scene_ready = FANG103_BIND(&client_image, &is_bootstrap_bound, cinematic_ready_fn, original_scene_ready, FANG103_ORIGINAL_SCENE_READY_RVA);
    original_ui_ready = FANG103_BIND(&client_image, &is_bootstrap_bound, cinematic_ready_fn, original_ui_ready, FANG103_ORIGINAL_UI_READY_RVA);
    original_screen_event = FANG103_BIND(&client_image, &is_bootstrap_bound, screen_event_fn, original_screen_event, FANG103_ORIGINAL_SCREEN_EVENT_RVA);
    original_movie_load = FANG103_BIND(&client_image, &is_bootstrap_bound, movie_load_fn, original_movie_load, FANG103_ORIGINAL_MOVIE_LOAD_RVA);
    original_movie_manager = FANG103_BIND(&client_image, &is_bootstrap_bound, movie_manager_fn, original_movie_manager, FANG103_ORIGINAL_MOVIE_MANAGER_RVA);
    original_spline_update = FANG103_BIND(&client_image, &is_bootstrap_bound, spline_update_fn, original_spline_update, FANG103_ORIGINAL_SPLINE_UPDATE_RVA);
    original_spline_skip = FANG103_BIND(&client_image, &is_bootstrap_bound, spline_skip_fn, original_spline_skip, FANG103_ORIGINAL_SPLINE_SKIP_RVA);
    original_camera_registry = FANG103_BIND(&client_image, &is_bootstrap_bound, camera_registry_fn, original_camera_registry, FANG103_ORIGINAL_CAMERA_REGISTRY_RVA);
    original_camera_set = FANG103_BIND(&client_image, &is_bootstrap_bound, camera_set_fn, original_camera_set, FANG103_ORIGINAL_CAMERA_SET_RVA);
    original_navigation_update = FANG103_BIND(&client_image, &is_bootstrap_bound, navigation_update_fn, original_navigation_update, FANG103_ORIGINAL_NAVIGATION_UPDATE_RVA);
    original_party_ready = FANG103_BIND(&client_image, &is_bootstrap_bound, party_ready_fn, original_party_ready, FANG103_ORIGINAL_PARTY_READY_RVA);
    original_go_to_room = FANG103_BIND(&client_image, &is_bootstrap_bound, go_to_room_fn, original_go_to_room, FANG103_ORIGINAL_GO_TO_ROOM_RVA);
    original_new_player_progress = FANG103_BIND(&client_image, &is_bootstrap_bound, new_player_progress_fn, original_new_player_progress, FANG103_ORIGINAL_NEW_PLAYER_PROGRESS_RVA);
    original_map_room_start_game = FANG103_BIND(&client_image, &is_bootstrap_bound, map_room_start_game_fn, original_map_room_start_game, FANG103_ORIGINAL_MAP_ROOM_START_GAME_RVA);
    original_online_platform = FANG103_BIND(&client_image, &is_bootstrap_bound, online_platform_fn, original_online_platform, FANG103_ORIGINAL_ONLINE_PLATFORM_RVA);
    original_rooms_bootstrap = FANG103_BIND(&client_image, &is_bootstrap_bound, rooms_bootstrap_fn, original_rooms_bootstrap, FANG103_ORIGINAL_ROOMS_BOOTSTRAP_RVA);
    original_rooms_map_lookup = FANG103_BIND(&client_image, &is_bootstrap_bound, rooms_map_lookup_fn, original_rooms_map_lookup, FANG103_ORIGINAL_ROOMS_MAP_LOOKUP_RVA);
    original_scene_asset_lookup = FANG103_BIND(&client_image, &is_bootstrap_bound, scene_asset_lookup_fn, original_scene_asset_lookup, FANG103_ORIGINAL_SCENE_ASSET_LOOKUP_RVA);
    original_creature_asset_lookup = FANG103_BIND(&client_image, &is_bootstrap_bound, creature_asset_lookup_fn, original_creature_asset_lookup, FANG103_ORIGINAL_CREATURE_ASSET_LOOKUP_RVA);
    original_creature_nested_lookup = FANG103_BIND(&client_image, &is_bootstrap_bound, creature_nested_lookup_fn, original_creature_nested_lookup, FANG103_ORIGINAL_CREATURE_NESTED_LOOKUP_RVA);
    original_scene_load = FANG103_BIND(&client_image, &is_bootstrap_bound, scene_load_fn, original_scene_load, FANG103_ORIGINAL_SCENE_LOAD_RVA);
    original_scene_change = FANG103_BIND(&client_image, &is_bootstrap_bound, scene_change_fn, original_scene_change, FANG103_ORIGINAL_SCENE_CHANGE_RVA);
    original_ability_hud = FANG103_BIND(&client_image, &is_bootstrap_bound, ability_hud_fn, original_ability_hud, FANG103_ORIGINAL_ABILITY_HUD_RVA);
    original_cooldown_lookup = FANG103_BIND(&client_image, &is_bootstrap_bound, cooldown_lookup_fn, original_cooldown_lookup, FANG103_ORIGINAL_COOLDOWN_LOOKUP_RVA);
    original_cooldown_apply = FANG103_BIND(&client_image, &is_bootstrap_bound, cooldown_apply_fn, original_cooldown_apply, FANG103_ORIGINAL_COOLDOWN_APPLY_RVA);
    original_game_clock_owner = FANG103_BIND(&client_image, &is_bootstrap_bound, game_clock_owner_fn, original_game_clock_owner, FANG103_ORIGINAL_GAME_CLOCK_OWNER_RVA);
    original_game_clock_now = FANG103_BIND(&client_image, &is_bootstrap_bound, game_clock_now_fn, original_game_clock_now, FANG103_ORIGINAL_GAME_CLOCK_NOW_RVA);
    original_controlled_player = FANG103_BIND(&client_image, &is_bootstrap_bound, controlled_player_fn, original_controlled_player, FANG103_ORIGINAL_CONTROLLED_PLAYER_RVA);
    original_clock_root = FANG103_BIND(&client_image, &is_bootstrap_bound, clock_root_fn, original_clock_root, FANG103_ORIGINAL_CLOCK_ROOT_RVA);
    original_clock_state = FANG103_BIND(&client_image, &is_bootstrap_bound, clock_state_fn, original_clock_state, FANG103_ORIGINAL_CLOCK_STATE_RVA);
    original_clock_initialize = FANG103_BIND(&client_image, &is_bootstrap_bound, clock_initialize_fn, original_clock_initialize, FANG103_ORIGINAL_CLOCK_INITIALIZE_RVA);
    original_ability_available = FANG103_BIND(&client_image, &is_bootstrap_bound, ability_available_fn, original_ability_available, FANG103_ORIGINAL_ABILITY_AVAILABLE_RVA);
    original_property_vector_read = FANG103_BIND(&client_image, &is_bootstrap_bound, property_vector_read_fn, original_property_vector_read, FANG103_ORIGINAL_PROPERTY_VECTOR_READ_RVA);
    original_resource_manager = FANG103_BIND(&client_image, &is_bootstrap_bound, resource_manager_fn, original_resource_manager, FANG103_ORIGINAL_RESOURCE_MANAGER_RVA);
    original_reflection_lookup = FANG103_BIND(&client_image, &is_bootstrap_bound, reflection_lookup_fn, original_reflection_lookup, FANG103_ORIGINAL_REFLECTION_LOOKUP_RVA);
    original_server_event_decode = FANG103_BIND(&client_image, &is_bootstrap_bound, server_event_decode_fn, original_server_event_decode, FANG103_ORIGINAL_SERVER_EVENT_DECODE_RVA);
    original_server_event_validate = FANG103_BIND(&client_image, &is_bootstrap_bound, server_event_validate_fn, original_server_event_validate, FANG103_ORIGINAL_SERVER_EVENT_VALIDATE_RVA);
    original_effect_system = FANG103_BIND(&client_image, &is_bootstrap_bound, effect_system_fn, original_effect_system, FANG103_ORIGINAL_EFFECT_SYSTEM_RVA);
    original_game_object_registry = FANG103_BIND(&client_image, &is_bootstrap_bound, resource_manager_fn, original_game_object_registry, FANG103_ORIGINAL_GAME_OBJECT_REGISTRY_RVA);
    original_game_object_lookup = FANG103_BIND(&client_image, &is_bootstrap_bound, lookup_method_fn, original_game_object_lookup, FANG103_ORIGINAL_GAME_OBJECT_LOOKUP_RVA);
    original_message_construct = FANG103_BIND(&client_image, &is_bootstrap_bound, message_construct_fn, original_message_construct, FANG103_ORIGINAL_MESSAGE_CONSTRUCT_RVA);
    original_message_read = FANG103_BIND(&client_image, &is_bootstrap_bound, message_read_fn, original_message_read, FANG103_ORIGINAL_MESSAGE_READ_RVA);
    original_login_screen_init = FANG103_BIND(&client_image, &is_bootstrap_bound, login_screen_init_fn, original_login_screen_init, FANG103_ORIGINAL_LOGIN_SCREEN_INIT_RVA);
    original_launcher_ready = FANG103_BIND(&client_image, &is_bootstrap_bound, launcher_ready_fn, original_launcher_ready, FANG103_ORIGINAL_LAUNCHER_READY_RVA);
    original_local_event = FANG103_BIND(&client_image, &is_bootstrap_bound, local_event_fn, original_local_event, FANG103_ORIGINAL_LOCAL_EVENT_RVA);
    original_loot_convert = FANG103_BIND(&client_image, &is_bootstrap_bound, loot_convert_fn, original_loot_convert, FANG103_ORIGINAL_LOOT_CONVERT_RVA);
    original_loot_format = FANG103_BIND(&client_image, &is_bootstrap_bound, loot_convert_fn, original_loot_format, FANG103_ORIGINAL_LOOT_FORMAT_RVA);
    original_mana_cost = FANG103_BIND(&client_image, &is_bootstrap_bound, mana_cost_fn, original_mana_cost, FANG103_ORIGINAL_MANA_COST_RVA);
    original_ability_descriptor_lookup = FANG103_BIND(&client_image, &is_bootstrap_bound, ability_descriptor_lookup_fn, original_ability_descriptor_lookup, FANG103_ORIGINAL_ABILITY_DESCRIPTOR_LOOKUP_RVA);
    original_combat_input_state = FANG103_BIND(&client_image, &is_bootstrap_bound, combat_input_state_fn, original_combat_input_state, FANG103_ORIGINAL_COMBAT_INPUT_STATE_RVA);
    original_combat_input_reset = FANG103_BIND(&client_image, &is_bootstrap_bound, combat_input_reset_fn, original_combat_input_reset, FANG103_ORIGINAL_COMBAT_INPUT_RESET_RVA);
    original_settings_manager = FANG103_BIND(&client_image, &is_bootstrap_bound, settings_manager_fn, original_settings_manager, FANG103_ORIGINAL_SETTINGS_MANAGER_RVA);
    original_game_object_resolve = FANG103_BIND(&client_image, &is_bootstrap_bound, game_object_resolve_fn, original_game_object_resolve, FANG103_ORIGINAL_GAME_OBJECT_RESOLVE_RVA);
    original_player_move_receiver = FANG103_BIND(&client_image, &is_bootstrap_bound, locomotion_receiver_fn, original_player_move_receiver, FANG103_ORIGINAL_PLAYER_MOVE_RECEIVER_RVA);
    original_locomotion_update_receiver = FANG103_BIND(&client_image, &is_bootstrap_bound, locomotion_receiver_fn, original_locomotion_update_receiver, FANG103_ORIGINAL_LOCOMOTION_UPDATE_RECEIVER_RVA);
    original_locomotion_unreliable_receiver = FANG103_BIND(&client_image, &is_bootstrap_bound, locomotion_receiver_fn, original_locomotion_unreliable_receiver, FANG103_ORIGINAL_LOCOMOTION_UNRELIABLE_RECEIVER_RVA);
    original_frame_delta = FANG103_BIND(&client_image, &is_bootstrap_bound, frame_delta_fn, original_frame_delta, FANG103_ORIGINAL_FRAME_DELTA_RVA);
    original_sporenet_callback = FANG103_BIND(&client_image, &is_bootstrap_bound, sporenet_callback_fn, original_sporenet_callback, FANG103_ORIGINAL_SPORENET_CALLBACK_RVA);
    original_scaleform_stream_read = FANG103_BIND(&client_image, &is_bootstrap_bound, scaleform_stream_read_fn, original_scaleform_stream_read, FANG103_ORIGINAL_SCALEFORM_STREAM_READ_RVA);
    original_chat_lookup = FANG103_BIND(&client_image, &is_bootstrap_bound, chat_lookup_fn, original_chat_lookup, FANG103_ORIGINAL_CHAT_LOOKUP_RVA);
    original_chat_create = FANG103_BIND(&client_image, &is_bootstrap_bound, chat_create_fn, original_chat_create, FANG103_ORIGINAL_CHAT_CREATE_RVA);
    original_chat_initialize = FANG103_BIND(&client_image, &is_bootstrap_bound, chat_initialize_fn, original_chat_initialize, FANG103_ORIGINAL_CHAT_INITIALIZE_RVA);
    original_chat_show = FANG103_BIND(&client_image, &is_bootstrap_bound, chat_show_fn, original_chat_show, FANG103_ORIGINAL_CHAT_SHOW_RVA);
    original_chat_open = FANG103_BIND(&client_image, &is_bootstrap_bound, chat_open_fn, original_chat_open, FANG103_ORIGINAL_CHAT_OPEN_RVA);
    original_chat_text_convert = FANG103_BIND(&client_image, &is_bootstrap_bound, chat_text_convert_fn, original_chat_text_convert, FANG103_ORIGINAL_CHAT_TEXT_CONVERT_RVA);
    if (!is_bootstrap_bound) {
        fang_client103_report(fang_history_write);
        return 0x8;
    }
    /* The option now supplies native Escape edges only. Legacy camera/menu
     * acceleration helpers remain disabled and are never installed below. */
    skip_cinematics_enabled = 0;
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
    is_network_ready = fang_network_configure(hostname, port, party_port, winsock);
    original_messageboxw = user32 == NULL ? NULL : (messageboxw_fn)GetProcAddress(user32, "MessageBoxW");
    original_messageboxa = user32 == NULL ? NULL : (messageboxa_fn)GetProcAddress(user32, "MessageBoxA");
    original_getfileattributesw = kernel32_module == NULL ? NULL : (getfileattributesw_fn)GetProcAddress(kernel32_module, "GetFileAttributesW");
    if (!is_network_ready) {
        return 0x200;
    }
#if FANG_DIAGNOSTICS
    if (trace_file != INVALID_HANDLE_VALUE && trace_lock_ready) {
        trace_client_state("exception_observer", (unsigned int)fang_install_exception_trace(trace_file, executable));
    }
#endif
    trace_client_state("camera_bindings", (unsigned int)fang_client103_camera_bind(executable));
    if (snapshot_endpoint[0] != '\0' && snapshot_keyframe_event == NULL) {
        snapshot_keyframe_event = CreateEventA(NULL, TRUE, FALSE, NULL);
        if (snapshot_keyframe_event == NULL) {
            result |= 0x20000000;
        }
    }
    if (snapshot_endpoint[0] != '\0') {
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
#if FANG_SCENARIO
    trace_client_state("scenario_scene_hook", (unsigned int)fang_control_scene_install(executable));
    trace_client_state("scenario_screen_hook", (unsigned int)fang_control_screen_install(executable));
    trace_client_state("scenario_map_hook", (unsigned int)fang_control_map_install(executable));
    trace_client_state("scenario_deployment_hook", (unsigned int)fang_control_deployment_install(executable));
    trace_client_state("scenario_arena_hook", (unsigned int)fang_control_arena_install(executable));
    trace_client_state("scenario_gameplay_hook", (unsigned int)fang_control_gameplay_install(executable));
    if (!install_scenario_renderer_observation(executable)) {
        result |= 0x8;
    }
    if (!install_scenario_dispatch_observation(executable)) {
        result |= 0x8;
    }
    if (!install_scenario_selector_observation(executable)) {
        result |= 0x8;
    }
    if (!install_scenario_primary_observation(executable)) {
        result |= 0x8;
    }
    if (!install_scenario_alternate_observation(executable)) {
        result |= 0x8;
    }
    if (!install_scenario_classification_observation(executable)) {
        result |= 0x8;
    }
#endif
    {
        int is_shop_pricing_installed = fang_install_shop_pricing(executable);
        trace_client_state("shop_pricing_hook", (unsigned int)is_shop_pricing_installed);
        if (is_shop_pricing_installed) {
            int is_shop_ui_installed = fang_install_shop_ui(executable);
            trace_client_state("shop_ui_hook", (unsigned int)is_shop_ui_installed);
            int is_shop_inventory_installed = fang_install_shop_inventory(executable);
            trace_client_state("shop_inventory_hook", (unsigned int)is_shop_inventory_installed);
            int is_editor_item_installed = fang_install_editor_item(executable);
            trace_client_state("editor_item_hook", (unsigned int)is_editor_item_installed);
            if (!is_editor_item_installed) {
                result |= 0x8;
            }
            if (!is_shop_inventory_installed) {
                result |= 0x8;
            }
            if (!is_shop_ui_installed) {
                result |= 0x8;
            }
        }
        if (!is_shop_pricing_installed) {
            result |= 0x8;
        }
    }
    {
        int is_display_hook_installed = fang_install_display_preferences(executable);
        trace_client_state("display_preferences_hook", (unsigned int)is_display_hook_installed);
        if (!is_display_hook_installed) {
            result |= 0x8;
        }
    }
    {
        int is_camera_zoom_installed = fang_install_camera_zoom(executable);
        trace_client_state("camera_zoom_hook", (unsigned int)is_camera_zoom_installed);
        if (!is_camera_zoom_installed) {
            result |= 0x8;
        }
    }
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
    if (!fang_network_allocate_redirect_tls()) {
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
    if (!patch_jump(fang_find_signature(executable, ssl_ctx_signature, sizeof(ssl_ctx_signature)), (void*)hooked_ssl_ctx_set_verify)) {
        result |= 0x10;
    }
    if (!patch_jump(fang_find_signature(executable, ssl_result_signature, sizeof(ssl_result_signature)), (void*)hooked_ssl_get_verify_result)) {
        result |= 0x20;
    }
    if (!patch_jump(fang_find_signature(executable, wildcard_signature, sizeof(wildcard_signature)), (void*)hooked_wildcard_match_no_case)) {
        result |= 0x40;
    }
    if (!patch_jump(fang_find_signature(executable, certificate_signature, sizeof(certificate_signature)), (void*)hooked_verify_certificate)) {
        result |= 0x80;
    }
    if (!patch_bytes(fang_find_signature(executable, secure_connect_signature, sizeof(secure_connect_signature)), force_plaintext, sizeof(force_plaintext))) {
        result |= 0x1000;
    }
    if (!patch_call((BYTE*)executable + FANG103_ORIGINAL_LAUNCHER_READY_CALL_1_RVA, (void*)original_launcher_ready, (void*)hooked_launcher_ready)) {
        result |= 0x2000000;
    }
    {
        int is_chat_command_hooked = patch_call((BYTE*)executable + FANG103_ORIGINAL_CHAT_TEXT_CONVERT_CALL_1_RVA,
            (void*)original_chat_text_convert, (void*)hooked_chat_text_convert);
        trace_client_state("chat_command_hook", (unsigned int)is_chat_command_hooked);
        if (!is_chat_command_hooked) {
            result |= 0x20000000;
        }
    }
    {
        int is_cinematic_hooked = fang_cinematic_install(executable, skip_intro, skip_cinematic);
        trace_client_state("cinematic_escape_hook", (unsigned int)is_cinematic_hooked);
        if (!is_cinematic_hooked && (skip_intro || skip_cinematic)) result |= 0x10000;
    }
#if FANG_DIAGNOSTICS
    {
        int is_party_ready_hooked = patch_call((BYTE*)executable + FANG103_ORIGINAL_PARTY_READY_CALL_1_RVA,
            (void*)original_party_ready, (void*)hooked_party_ready);
        trace_client_state("party_navigation_hook", (unsigned int)is_party_ready_hooked);
    }
    cinematic_trace_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_SCENE_READY_CALL_1_RVA, (void*)original_scene_ready, (void*)hooked_scene_ready);
    cinematic_trace_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_UI_READY_CALL_1_RVA, (void*)original_ui_ready, (void*)hooked_ui_ready);
#endif
    if (FANG_DIAGNOSTICS || skip_cinematic) {
        cinematic_trace_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_SCREEN_EVENT_CALL_1_RVA, (void*)original_screen_event, (void*)hooked_screen_event);
        cinematic_trace_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_SCREEN_EVENT_CALL_2_RVA, (void*)original_screen_event, (void*)hooked_screen_event);
        cinematic_trace_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_SCREEN_EVENT_CALL_3_RVA, (void*)original_screen_event, (void*)hooked_screen_event);
    }
    if (cinematic_trace_patches != (FANG_DIAGNOSTICS ? 5 : (skip_cinematic ? 3 : 0))) {
        result |= 0x100000;
    }
    snapshot_trace_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_MESSAGE_CONSTRUCT_CALL_1_RVA,
        (void*)original_message_construct, (void*)hooked_message_construct);
    snapshot_trace_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_GAME_OBJECT_RESOLVE_CALL_1_RVA,
        (void*)original_game_object_resolve, (void*)hooked_movement_object_resolve);
    snapshot_trace_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_GAME_OBJECT_RESOLVE_CALL_2_RVA,
        (void*)original_game_object_resolve, (void*)hooked_movement_object_resolve);
    snapshot_trace_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_GAME_OBJECT_RESOLVE_CALL_3_RVA,
        (void*)original_game_object_resolve, (void*)hooked_movement_object_resolve);
    snapshot_trace_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_PLAYER_MOVE_RECEIVER_CALL_1_RVA,
        (void*)original_player_move_receiver, (void*)hooked_player_move_receiver);
    snapshot_trace_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_LOCOMOTION_UPDATE_RECEIVER_CALL_1_RVA,
        (void*)original_locomotion_update_receiver, (void*)hooked_locomotion_update_receiver);
    snapshot_trace_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_LOCOMOTION_UNRELIABLE_RECEIVER_CALL_1_RVA,
        (void*)original_locomotion_unreliable_receiver,
        (void*)hooked_locomotion_unreliable_receiver);
    snapshot_trace_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_FRAME_DELTA_CALL_1_RVA,
        (void*)original_frame_delta, (void*)hooked_frame_delta);
    if (snapshot_trace_patches != 8) {
        result |= 0x4000000;
    }
#if FANG_DIAGNOSTICS
    /* Scene observation and developer recovery hooks are omitted from release
       Fang builds. */
    scene_trace_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_SCENE_ASSET_LOOKUP_CALL_1_RVA,
        (void*)original_scene_asset_lookup, (void*)hooked_scene_asset_lookup);
    scene_trace_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_MESSAGE_READ_CALL_1_RVA,
        (void*)original_message_read, (void*)hooked_quick_game_read);
    scene_trace_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_CREATURE_ASSET_LOOKUP_CALL_1_RVA,
        (void*)original_creature_asset_lookup, (void*)hooked_creature_asset_lookup);
    scene_trace_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_CREATURE_NESTED_LOOKUP_CALL_1_RVA,
        (void*)original_creature_nested_lookup, (void*)hooked_creature_nested_lookup);
    scene_trace_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_SCENE_LOAD_CALL_1_RVA,
        (void*)original_scene_load, (void*)hooked_scene_load);
    scene_trace_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_SCENE_LOAD_CALL_2_RVA,
        (void*)original_scene_load, (void*)hooked_scene_load);
    scene_trace_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_SCENE_CHANGE_CALL_1_RVA,
        (void*)original_scene_change, (void*)hooked_scene_change);
    scene_trace_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_COOLDOWN_APPLY_CALL_1_RVA,
        (void*)original_cooldown_apply, (void*)hooked_cooldown_apply);
    scene_trace_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_CLOCK_INITIALIZE_CALL_1_RVA,
        (void*)original_clock_initialize, (void*)hooked_clock_initialize);
    scene_trace_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_PROPERTY_VECTOR_READ_CALL_1_RVA,
        (void*)original_property_vector_read, (void*)hooked_property_vector_read);
    scene_trace_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_ABILITY_AVAILABLE_CALL_1_RVA,
        (void*)original_ability_available, (void*)hooked_ability_blink_play_available);
    scene_trace_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_ABILITY_AVAILABLE_CALL_2_RVA,
        (void*)original_ability_available, (void*)hooked_ability_blink_stop_available);
    scene_trace_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_ABILITY_DESCRIPTOR_LOOKUP_CALL_1_RVA,
        (void*)original_ability_descriptor_lookup, (void*)hooked_ability_descriptor_lookup);
#endif
#if FANG_DIAGNOSTICS
    if (effect_preview_tls_index != TLS_OUT_OF_INDEXES) {
        int is_decode_hooked = patch_call((BYTE*)executable + FANG103_ORIGINAL_SERVER_EVENT_DECODE_CALL_1_RVA,
            (void*)original_server_event_decode, (void*)hooked_server_event_decode);
        int is_validate_hooked = patch_call((BYTE*)executable + FANG103_ORIGINAL_SERVER_EVENT_VALIDATE_CALL_1_RVA,
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
    loot_trace_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_LOCAL_EVENT_CALL_1_RVA,
        (void*)original_local_event, (void*)hooked_local_event);
    loot_trace_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_LOOT_CONVERT_CALL_1_RVA,
        (void*)original_loot_convert, (void*)hooked_loot_convert);
    loot_trace_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_LOOT_FORMAT_CALL_1_RVA,
        (void*)original_loot_format, (void*)hooked_loot_format);
    loot_trace_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_CREATURE_NESTED_LOOKUP_CALL_2_RVA,
        (void*)original_creature_nested_lookup, (void*)hooked_loot_resource_lookup);
    loot_trace_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_CREATURE_NESTED_LOOKUP_CALL_3_RVA,
        (void*)original_creature_nested_lookup, (void*)hooked_loot_resource_lookup);
    loot_trace_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_CREATURE_NESTED_LOOKUP_CALL_4_RVA,
        (void*)original_creature_nested_lookup, (void*)hooked_loot_resource_lookup);
    loot_trace_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_CREATURE_NESTED_LOOKUP_CALL_5_RVA,
        (void*)original_creature_nested_lookup, (void*)hooked_loot_resource_lookup);
    loot_trace_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_CREATURE_NESTED_LOOKUP_CALL_6_RVA,
        (void*)original_creature_nested_lookup, (void*)hooked_loot_resource_lookup);
    if (loot_trace_patches != 8) {
        result |= 0x8000000;
    }
    mana_cost_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_MANA_COST_CALL_1_RVA,
        (void*)original_mana_cost, (void*)hooked_mana_cost);
    mana_cost_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_MANA_COST_CALL_2_RVA,
        (void*)original_mana_cost, (void*)hooked_mana_cost);
    mana_cost_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_MANA_COST_CALL_3_RVA,
        (void*)original_mana_cost, (void*)hooked_mana_cost);
    mana_cost_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_MANA_COST_CALL_4_RVA,
        (void*)original_mana_cost, (void*)hooked_mana_cost);
    mana_cost_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_MANA_COST_CALL_5_RVA,
        (void*)original_mana_cost, (void*)hooked_mana_cost);
    mana_cost_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_MANA_COST_CALL_6_RVA,
        (void*)original_mana_cost, (void*)hooked_mana_cost);
    trace_client_state("mana_cost_hooks", (unsigned int)mana_cost_patches);
    alert_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_ALERT_CALL_1_RVA, (void*)original_alert, (void*)hooked_internal_alert);
    alert_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_ALERT_CALL_2_RVA, (void*)original_alert, (void*)hooked_internal_alert);
    alert_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_ALERT_CALL_3_RVA, (void*)original_alert, (void*)hooked_internal_alert);
    alert_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_ALERT_CALL_4_RVA, (void*)original_alert, (void*)hooked_internal_alert);
    alert_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_ALERT_CALL_5_RVA, (void*)original_alert, (void*)hooked_internal_alert);
    alert_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_ALERT_CALL_6_RVA, (void*)original_alert, (void*)hooked_internal_alert);
    alert_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_ALERT_CALL_7_RVA, (void*)original_alert, (void*)hooked_internal_alert);
    alert_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_ALERT_CALL_8_RVA, (void*)original_alert, (void*)hooked_internal_alert);
    alert_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_ALERT_CALL_9_RVA, (void*)original_alert, (void*)hooked_internal_alert);
    alert_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_ALERT_CALL_10_RVA, (void*)original_alert, (void*)hooked_internal_alert);
    alert_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_ALERT_CALL_11_RVA, (void*)original_alert, (void*)hooked_internal_alert);
    if (alert_patches != 11) {
        result |= 0x4000;
    }
    login_error_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_LOGIN_ERROR_CALL_1_RVA, (void*)original_login_error, (void*)hooked_login_error);
    login_error_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_LOGIN_ERROR_CALL_2_RVA, (void*)original_login_error, (void*)hooked_login_error);
    login_error_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_LOGIN_ERROR_CALL_3_RVA, (void*)original_login_error, (void*)hooked_login_error);
    login_error_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_LOGIN_ERROR_CALL_4_RVA, (void*)original_login_error, (void*)hooked_login_error);
    login_error_patches += patch_call((BYTE*)executable + FANG103_ORIGINAL_LOGIN_ERROR_CALL_5_RVA, (void*)original_login_error, (void*)hooked_login_error);
    if (login_error_patches != 5) {
        result |= 0x8000;
    }
#endif
    if (jwt_login_token[0] != '\0' &&
        !patch_pointer((void**)((BYTE*)executable + FANG103_ORIGINAL_LOGIN_SCREEN_INIT_POINTER_1_RVA), (void*)original_login_screen_init, (void*)hooked_login_screen_init)) {
        result |= 0x800000;
    }
    fang_client103_report(fang_history_write);
    fang_hook_report(fang_history_write);
    return result;
}

__declspec(dllexport) DWORD WINAPI RecapInitializeThread(LPVOID parameter) {
    (void)parameter;
    return (DWORD)GoRecapInitialize();
}

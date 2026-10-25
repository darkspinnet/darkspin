#ifndef DARKSPIN_CONTROL_H
#define DARKSPIN_CONTROL_H
#ifndef FANG_SCENARIO
#define FANG_SCENARIO 0
#endif
#if FANG_SCENARIO
#include <stdint.h>
#include <windows.h>
typedef struct fang_control_window_snapshot {
    uint32_t window_id, process_id, thread_id, width, height;
    uint32_t is_enabled, is_minimized, is_available;
} fang_control_window_snapshot;
int fang_control_window_inspect(fang_control_window_snapshot* snapshot);
typedef struct fang_control_window_receipt {
    uint32_t request_id, status, thread_id;
    uint64_t completed_tick_ms;
    uint32_t processed_character_count;
} fang_control_window_receipt;
typedef struct fang_control_map_state {
    uint32_t is_available, is_loaded, fields;
    uint32_t state, difficulty, game_type, animation_state, animation_difficulty;
    uint32_t difficulty_mode;
    uint32_t is_transactions_processing, are_party_members_ready, is_start_disabled, is_start_visible;
    uint32_t is_campaign_ready, is_previous_ready, is_next_ready;
} fang_control_map_state;
typedef struct fang_control_deployment_state {
    uint32_t is_available, is_loaded, is_ready, is_results_screen, fields;
    uint32_t squad_index, major_difficulty, minor_difficulty;
    uint32_t is_squad_valid, is_accept_disabled, is_accept_visible, are_squad_buttons_disabled;
} fang_control_deployment_state;
typedef struct fang_control_arena_state {
    uint32_t is_available, is_loaded, is_ready, fields, squad_index;
    uint32_t is_squad_valid, is_accept_disabled, is_accept_visible;
} fang_control_arena_state;
int fang_control_arena_install(HMODULE executable);
int fang_control_arena_read(fang_control_arena_state* snapshot);
uint32_t fang_control_arena_accept(fang_control_arena_state* snapshot);
int fang_control_window_arena_result(uint32_t request_id, fang_control_arena_state* snapshot);

typedef struct fang_control_beam_state {
    uint32_t is_available, is_ready, is_requested;
} fang_control_beam_state;
int fang_control_window_beam_result(uint32_t request_id, fang_control_beam_state* snapshot);
typedef struct fang_control_screen_state {
    uint32_t is_available, screen, is_inventory_open, is_browser_open, fields;
    uint32_t is_invocation_attempted, is_invocation_returned;
    int32_t native_screen_id;
    uint64_t owner_generation;
} fang_control_screen_state;
int fang_control_screen_install(HMODULE executable);
uint32_t fang_control_screen(uint32_t request_id, uint32_t screen, int is_action, uint64_t expected_owner_generation, uint32_t scene, uint64_t deadline,
    fang_control_screen_state* snapshot);
int fang_control_window_screen(uint32_t request_id, uint32_t screen, int is_action, uint64_t expected_owner_generation, uint32_t timeout_ms);
int fang_control_window_screen_result(uint32_t request_id, fang_control_screen_state* snapshot);
int fang_control_window_screen_current(uint32_t request_id);
uint32_t fang_control_beam_out(uint32_t scene, uint64_t deadline, int is_action, fang_control_beam_state* snapshot);
int fang_control_window_mission(uint32_t request_id, uint32_t kind, uint32_t timeout_ms);
int fang_control_window_deployment_result(uint32_t request_id, fang_control_deployment_state* snapshot);
int fang_control_window_submit(uint32_t request_id, int32_t x, int32_t y, uint32_t timeout_ms);
int fang_control_window_right_click(uint32_t request_id, int32_t x, int32_t y, uint32_t timeout_ms);
int fang_control_window_ready(uint32_t request_id, uint32_t timeout_ms);
int fang_control_window_scene(uint32_t request_id, uint32_t room, uint32_t timeout_ms);
int fang_control_window_scene_ready(uint32_t request_id, uint32_t room, uint32_t timeout_ms);
int fang_control_window_press(uint32_t request_id, uint32_t key, uint32_t timeout_ms);
int fang_control_window_text(uint32_t request_id, const char* text, uint32_t length, uint32_t timeout_ms);
int fang_control_window_map(uint32_t request_id, uint32_t timeout_ms);
int fang_control_window_editor_cancel(uint32_t request_id, uint32_t timeout_ms);
int fang_control_window_map_result(uint32_t request_id, fang_control_map_state* snapshot);
int fang_control_window_result(uint32_t request_id, fang_control_window_receipt* receipt);
void fang_control_window_cancel(uint32_t request_id);
#include <windows.h>
int fang_control_scene_install(HMODULE executable);
uint32_t fang_control_scene_request(uint32_t room);
uint32_t fang_control_scene_ready(uint32_t room);
int fang_control_editor_install(HMODULE executable);
#define FANG_EDITOR_NODE_LIMIT 4096
typedef struct fang_control_editor_node {
    uint32_t id, command, flags, parent_index, depth;
    float left, top, right, bottom;
} fang_control_editor_node;
typedef struct fang_control_editor_tree {
    uint32_t is_available, is_complete, node_count, owner_generation, is_body_mode, editor_mode, is_mod_editor;
    fang_control_editor_node nodes[FANG_EDITOR_NODE_LIMIT];
} fang_control_editor_tree;
uint32_t fang_control_editor_hierarchy(uint32_t request_id, uint32_t scene, uint64_t deadline, fang_control_editor_tree* snapshot);
int fang_control_window_editor_hierarchy(uint32_t request_id, uint32_t timeout_ms);
int fang_control_window_editor_open(uint32_t request_id, uint32_t hero_noun, uint32_t timeout_ms);
uint32_t fang_mod_editor_open_owned(uint32_t hero_noun);
uint32_t fang_mod_editor_owned_noun(BYTE* context);
int fang_control_window_editor_tree_result(uint32_t request_id, fang_control_editor_tree* snapshot);
int fang_control_window_editor_current(uint32_t request_id);
uint32_t fang_control_editor_ready(void);
uint32_t fang_control_editor_cancel(HWND window, WNDPROC original);
int fang_control_editor_exit_begin(BYTE* editor);
void fang_control_editor_exit_end(int is_entered);
void* fang_control_editor_exit_target(BYTE* base);
int fang_control_map_install(HMODULE executable);
int fang_control_map_read(fang_control_map_state* snapshot);
uint32_t fang_control_map_start(fang_control_map_state* snapshot);
uint32_t fang_control_map_select(uint32_t kind, fang_control_map_state* snapshot);
int fang_control_deployment_install(HMODULE executable);
int fang_control_deployment_read(fang_control_deployment_state* snapshot);
uint32_t fang_control_deployment_continue(fang_control_deployment_state* snapshot);
void fang_control_window_attach(HWND window);
int fang_control_window_message(HWND window, UINT message, WPARAM wparam, LPARAM lparam, WNDPROC original);
typedef struct fang_control_snapshot {
    uint32_t frame_sequence;
    uint32_t frame_thread_id;
    uint64_t frame_tick_ms;
    uint32_t scene_asset_id;
    uint32_t scene_load_return_count;
    uint32_t scene_frame_sequence;
    uint64_t scene_tick_ms;
} fang_control_snapshot;
typedef struct fang_control_camera_snapshot {
    uint32_t is_available, fields, target_handle, is_override_active, mode, follow_mode, zoom_mode;
    uint32_t is_anchor_available, anchor_source, control_mode;
    float center[3], desired[3], anchor[3], offset_anchor[3], source[3];
    float hold_timer, distance, distance_target;
} fang_control_camera_snapshot;
void fang_control_camera_read(fang_control_camera_snapshot* snapshot);

typedef struct fang_control_gameplay_state {
    uint32_t is_available, is_world_active, fields, controlled_handle, picked_handle;
    uint32_t frame_sequence, owner_thread_id;
    float x, y, z;
    fang_control_camera_snapshot camera;
} fang_control_gameplay_state;
typedef struct fang_control_projection {
    uint32_t is_available, is_on_screen, client_width, client_height;
    float x, y;
} fang_control_projection;
typedef struct fang_control_gameplay_click {
    uint32_t is_projected, is_press_poll_observed, is_release_poll_observed, fields, retained_handle;
    uint32_t is_native_ground_query_success;
    float ray_x, ray_y, ray_z, ground_x, ground_y, ground_z;
} fang_control_gameplay_click;
typedef struct fang_control_hero_selection {
    uint32_t requested_slot, is_invocation_attempted, is_query_processed, fields;
    uint32_t previous_controlled_handle, observed_controlled_handle, previous_slot, observed_slot, eligibility_code;
} fang_control_hero_selection;
int fang_control_hero_install(HMODULE executable);
uint32_t fang_control_hero_begin(uint32_t request_id, uint32_t scene, uint32_t handle, uint32_t slot,
    uint64_t deadline, fang_control_hero_selection* snapshot);
uint32_t fang_control_hero_frame(uint32_t request_id, fang_control_hero_selection* snapshot);
void fang_control_hero_cancel(uint32_t request_id);
typedef struct fang_control_active_cast {
    uint32_t action_index, is_query_processed, is_invocation_attempted, is_native_request_attempted, fields;
    int32_t native_result_code;
    uint32_t ability_id, target_handle;
    fang_control_gameplay_click ground;
} fang_control_active_cast;
int fang_control_ability_install(HMODULE executable);
unsigned char fang_control_ability_edge(void* caller, uint32_t action, unsigned char is_pressed);
uint32_t fang_control_ability_begin(uint32_t request_id, uint32_t scene, uint32_t handle, int32_t x, int32_t y,
    uint64_t deadline, fang_control_active_cast* snapshot);
uint32_t fang_control_ability_frame(uint32_t request_id, fang_control_active_cast* snapshot);
void fang_control_ability_cancel(uint32_t request_id);
int fang_control_gameplay_install(HMODULE executable);
void fang_control_gameplay_frame(uint32_t sequence);
int fang_control_gameplay_submit(uint32_t request_id, uint32_t kind, float x, float y, float z, uint32_t timeout_ms, uint32_t hold_ms, uint32_t hero_slot);
int fang_control_gameplay_result(uint32_t request_id, fang_control_window_receipt* receipt,
    fang_control_gameplay_state* snapshot, fang_control_projection* projection, fang_control_gameplay_click* click,
    fang_control_hero_selection* selection, fang_control_active_cast* cast);
void fang_control_gameplay_cancel(uint32_t request_id);
void fang_control_frame(uint32_t sequence);
void fang_control_scene(uint32_t asset_id, uint32_t frame_sequence);
void fang_control_read(fang_control_snapshot* snapshot);
#endif
#endif

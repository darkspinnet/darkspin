#include "scenario/rpc.h"
#if FANG_SCENARIO
__declspec(dllexport) DWORD WINAPI RecapScenarioControlThread(LPVOID parameter) {
 fang_rpc_header* header = parameter; if (!header || header->version != 1) return 1;
 if (!header->deadline_tick_ms || GetTickCount64() >= header->deadline_tick_ms) return 5;
 switch(header->operation) {
case 1: { fang_rpc_cinematic_configure* req = parameter; if(header->size != sizeof(*req)) return 2;
header->result=fang_cinematic_configure(req->mask, req->flags, &req->setting); break; }
case 2: { fang_rpc_control_gameplay_cancel* req = parameter; if(header->size != sizeof(*req)) return 2;
fang_control_gameplay_cancel(req->request_id); header->result=1; break; }
case 3: { fang_rpc_control_gameplay_result* req = parameter; if(header->size != sizeof(*req)) return 2;
header->result=fang_control_gameplay_result(req->request_id, &req->receipt, &req->snapshot, &req->projection, &req->click, &req->selection, &req->cast); break; }
case 4: { fang_rpc_control_gameplay_submit* req = parameter; if(header->size != sizeof(*req)) return 2;
req->timeout_ms=(uint32_t)(header->deadline_tick_ms-GetTickCount64());
if (req->timeout_ms==0 || req->timeout_ms>2000) return 5;
header->result=fang_control_gameplay_submit(req->request_id, req->kind, req->x, req->y, req->z, req->timeout_ms, req->hold_ms, req->hero_slot, req->ability_id); break; }
case 5: { fang_rpc_control_read* req = parameter; if(header->size != sizeof(*req)) return 2;
fang_control_read(&req->snapshot); header->result=1; break; }
case 6: { fang_rpc_control_window_arena_result* req = parameter; if(header->size != sizeof(*req)) return 2;
header->result=fang_control_window_arena_result(req->request_id, &req->snapshot); break; }
case 7: { fang_rpc_control_window_beam_result* req = parameter; if(header->size != sizeof(*req)) return 2;
header->result=fang_control_window_beam_result(req->request_id, &req->snapshot); break; }
case 8: { fang_rpc_control_window_cancel* req = parameter; if(header->size != sizeof(*req)) return 2;
fang_control_window_cancel(req->request_id); header->result=1; break; }
case 9: { fang_rpc_control_window_cinematic* req = parameter; if(header->size != sizeof(*req)) return 2;
req->timeout_ms=(uint32_t)(header->deadline_tick_ms-GetTickCount64());
if (req->timeout_ms==0 || req->timeout_ms>2000) return 5;
header->result=fang_control_window_cinematic(req->request_id, req->timeout_ms); break; }
case 10: { fang_rpc_control_window_cinematic_result* req = parameter; if(header->size != sizeof(*req)) return 2;
header->result=fang_control_window_cinematic_result(req->request_id, &req->snapshot); break; }
case 11: { fang_rpc_control_window_deployment_result* req = parameter; if(header->size != sizeof(*req)) return 2;
header->result=fang_control_window_deployment_result(req->request_id, &req->snapshot); break; }
case 12: { fang_rpc_control_window_editor_cancel* req = parameter; if(header->size != sizeof(*req)) return 2;
req->timeout_ms=(uint32_t)(header->deadline_tick_ms-GetTickCount64());
if (req->timeout_ms==0 || req->timeout_ms>2000) return 5;
header->result=fang_control_window_editor_cancel(req->request_id, req->timeout_ms); break; }
case 13: { fang_rpc_control_window_editor_hierarchy* req = parameter; if(header->size != sizeof(*req)) return 2;
req->timeout_ms=(uint32_t)(header->deadline_tick_ms-GetTickCount64());
if (req->timeout_ms==0 || req->timeout_ms>2000) return 5;
header->result=fang_control_window_editor_hierarchy(req->request_id, req->timeout_ms); break; }
case 14: { fang_rpc_control_window_editor_open* req = parameter; if(header->size != sizeof(*req)) return 2;
req->timeout_ms=(uint32_t)(header->deadline_tick_ms-GetTickCount64());
if (req->timeout_ms==0 || req->timeout_ms>2000) return 5;
header->result=fang_control_window_editor_open(req->request_id, req->hero_noun, req->is_import, req->timeout_ms); break; }
case 15: { fang_rpc_control_window_editor_tree_result* req = parameter; if(header->size != sizeof(*req)) return 2;
header->result=fang_control_window_editor_tree_result(req->request_id, &req->snapshot); break; }
case 16: { fang_rpc_control_window_inspect* req = parameter; if(header->size != sizeof(*req)) return 2;
header->result=fang_control_window_inspect(&req->snapshot); break; }
case 17: { fang_rpc_control_window_map* req = parameter; if(header->size != sizeof(*req)) return 2;
req->timeout_ms=(uint32_t)(header->deadline_tick_ms-GetTickCount64());
if (req->timeout_ms==0 || req->timeout_ms>2000) return 5;
header->result=fang_control_window_map(req->request_id, req->timeout_ms); break; }
case 18: { fang_rpc_control_window_map_result* req = parameter; if(header->size != sizeof(*req)) return 2;
header->result=fang_control_window_map_result(req->request_id, &req->snapshot); break; }
case 19: { fang_rpc_control_window_mission* req = parameter; if(header->size != sizeof(*req)) return 2;
req->timeout_ms=(uint32_t)(header->deadline_tick_ms-GetTickCount64());
if (req->timeout_ms==0 || req->timeout_ms>2000) return 5;
header->result=fang_control_window_mission(req->request_id, req->kind, req->timeout_ms); break; }
case 20: { fang_rpc_control_window_press* req = parameter; if(header->size != sizeof(*req)) return 2;
req->timeout_ms=(uint32_t)(header->deadline_tick_ms-GetTickCount64());
if (req->timeout_ms==0 || req->timeout_ms>2000) return 5;
header->result=fang_control_window_press(req->request_id, req->key, req->timeout_ms); break; }
case 21: { fang_rpc_control_window_ready* req = parameter; if(header->size != sizeof(*req)) return 2;
req->timeout_ms=(uint32_t)(header->deadline_tick_ms-GetTickCount64());
if (req->timeout_ms==0 || req->timeout_ms>2000) return 5;
header->result=fang_control_window_ready(req->request_id, req->timeout_ms); break; }
case 22: { fang_rpc_control_window_result* req = parameter; if(header->size != sizeof(*req)) return 2;
header->result=fang_control_window_result(req->request_id, &req->receipt); break; }
case 23: { fang_rpc_control_window_right_click* req = parameter; if(header->size != sizeof(*req)) return 2;
req->timeout_ms=(uint32_t)(header->deadline_tick_ms-GetTickCount64());
if (req->timeout_ms==0 || req->timeout_ms>2000) return 5;
header->result=fang_control_window_right_click(req->request_id, req->x, req->y, req->timeout_ms); break; }
case 24: { fang_rpc_control_window_scene* req = parameter; if(header->size != sizeof(*req)) return 2;
req->timeout_ms=(uint32_t)(header->deadline_tick_ms-GetTickCount64());
if (req->timeout_ms==0 || req->timeout_ms>2000) return 5;
header->result=fang_control_window_scene(req->request_id, req->room, req->timeout_ms); break; }
case 25: { fang_rpc_control_window_scene_ready* req = parameter; if(header->size != sizeof(*req)) return 2;
req->timeout_ms=(uint32_t)(header->deadline_tick_ms-GetTickCount64());
if (req->timeout_ms==0 || req->timeout_ms>2000) return 5;
header->result=fang_control_window_scene_ready(req->request_id, req->room, req->timeout_ms); break; }
case 26: { fang_rpc_control_window_screen* req = parameter; if(header->size != sizeof(*req)) return 2;
req->timeout_ms=(uint32_t)(header->deadline_tick_ms-GetTickCount64());
if (req->timeout_ms==0 || req->timeout_ms>2000) return 5;
header->result=fang_control_window_screen(req->request_id, req->screen, req->is_action, req->expected_owner_generation, req->timeout_ms); break; }
case 27: { fang_rpc_control_window_screen_result* req = parameter; if(header->size != sizeof(*req)) return 2;
header->result=fang_control_window_screen_result(req->request_id, &req->snapshot); break; }
case 28: { fang_rpc_control_window_submit* req = parameter; if(header->size != sizeof(*req)) return 2;
req->timeout_ms=(uint32_t)(header->deadline_tick_ms-GetTickCount64());
if (req->timeout_ms==0 || req->timeout_ms>2000) return 5;
header->result=fang_control_window_submit(req->request_id, req->x, req->y, req->timeout_ms); break; }
case 29: { fang_rpc_control_window_text* req = parameter; if(header->size != sizeof(*req)) return 2;
req->timeout_ms=(uint32_t)(header->deadline_tick_ms-GetTickCount64());
if (req->timeout_ms==0 || req->timeout_ms>2000) return 5;
if (req->length > sizeof(req->text)) return 3;
header->result=fang_control_window_text(req->request_id, req->text, req->length, req->timeout_ms); break; }
default: return 4;
 } return 0;
}
#endif

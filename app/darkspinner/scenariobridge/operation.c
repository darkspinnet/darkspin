#include "proxy.h"
#include <stdlib.h>
#include <string.h>
int fang_cinematic_configure(uint32_t mask, uint32_t flags, fang_cinematic_setting* setting) {
 fang_rpc_cinematic_configure* req = calloc(1,sizeof(*req));
 if (!req) return 0;
 req->header.version=1; req->header.size=sizeof(*req); req->header.operation=1;
req->mask=mask;
req->flags=flags;
int is_ok=fang_proxy_call(&req->header);
if (is_ok && setting) *setting=req->setting;
int result=is_ok ? req->header.result : 0; free(req);
return result;
}
void fang_control_gameplay_cancel(uint32_t request_id) {
 fang_rpc_control_gameplay_cancel* req = calloc(1,sizeof(*req));
 if (!req) return;
 req->header.version=1; req->header.size=sizeof(*req); req->header.operation=2;
req->request_id=request_id;
int is_ok=fang_proxy_call(&req->header);
int result=is_ok ? req->header.result : 0; free(req);
(void)result;
}
int fang_control_gameplay_result(uint32_t request_id, fang_control_window_receipt* receipt,
    fang_control_gameplay_state* snapshot, fang_control_projection* projection, fang_control_gameplay_click* click,
    fang_control_hero_selection* selection, fang_control_active_cast* cast) {
 fang_rpc_control_gameplay_result* req = calloc(1,sizeof(*req));
 if (!req) return 0;
 req->header.version=1; req->header.size=sizeof(*req); req->header.operation=3;
req->request_id=request_id;
int is_ok=fang_proxy_call(&req->header);
if (is_ok && receipt) *receipt=req->receipt;
if (is_ok && snapshot) *snapshot=req->snapshot;
if (is_ok && projection) *projection=req->projection;
if (is_ok && click) *click=req->click;
if (is_ok && selection) *selection=req->selection;
if (is_ok && cast) *cast=req->cast;
int result=is_ok ? req->header.result : 0; free(req);
return result;
}
int fang_control_gameplay_submit(uint32_t request_id, uint32_t kind, float x, float y, float z, uint32_t timeout_ms, uint32_t hold_ms, uint32_t hero_slot, uint32_t ability_id) {
 fang_rpc_control_gameplay_submit* req = calloc(1,sizeof(*req));
 if (!req) return 0;
 req->header.version=1; req->header.size=sizeof(*req); req->header.operation=4;
req->request_id=request_id;
req->kind=kind;
req->x=x;
req->y=y;
req->z=z;
req->timeout_ms=timeout_ms;
req->header.deadline_tick_ms=GetTickCount64()+timeout_ms;
req->hold_ms=hold_ms;
req->hero_slot=hero_slot;
req->ability_id=ability_id;
int is_ok=fang_proxy_call(&req->header);
int result=is_ok ? req->header.result : 0; free(req);
return result;
}
void fang_control_read(fang_control_snapshot* snapshot) {
 fang_rpc_control_read* req = calloc(1,sizeof(*req));
 if (!req) return;
 req->header.version=1; req->header.size=sizeof(*req); req->header.operation=5;
int is_ok=fang_proxy_call(&req->header);
if (is_ok && snapshot) *snapshot=req->snapshot;
int result=is_ok ? req->header.result : 0; free(req);
(void)result;
}
int fang_control_window_arena_result(uint32_t request_id, fang_control_arena_state* snapshot) {
 fang_rpc_control_window_arena_result* req = calloc(1,sizeof(*req));
 if (!req) return 0;
 req->header.version=1; req->header.size=sizeof(*req); req->header.operation=6;
req->request_id=request_id;
int is_ok=fang_proxy_call(&req->header);
if (is_ok && snapshot) *snapshot=req->snapshot;
int result=is_ok ? req->header.result : 0; free(req);
return result;
}
int fang_control_window_beam_result(uint32_t request_id, fang_control_beam_state* snapshot) {
 fang_rpc_control_window_beam_result* req = calloc(1,sizeof(*req));
 if (!req) return 0;
 req->header.version=1; req->header.size=sizeof(*req); req->header.operation=7;
req->request_id=request_id;
int is_ok=fang_proxy_call(&req->header);
if (is_ok && snapshot) *snapshot=req->snapshot;
int result=is_ok ? req->header.result : 0; free(req);
return result;
}
void fang_control_window_cancel(uint32_t request_id) {
 fang_rpc_control_window_cancel* req = calloc(1,sizeof(*req));
 if (!req) return;
 req->header.version=1; req->header.size=sizeof(*req); req->header.operation=8;
req->request_id=request_id;
int is_ok=fang_proxy_call(&req->header);
int result=is_ok ? req->header.result : 0; free(req);
(void)result;
}
int fang_control_window_cinematic(uint32_t request_id, uint32_t timeout_ms) {
 fang_rpc_control_window_cinematic* req = calloc(1,sizeof(*req));
 if (!req) return 0;
 req->header.version=1; req->header.size=sizeof(*req); req->header.operation=9;
req->request_id=request_id;
req->timeout_ms=timeout_ms;
req->header.deadline_tick_ms=GetTickCount64()+timeout_ms;
int is_ok=fang_proxy_call(&req->header);
int result=is_ok ? req->header.result : 0; free(req);
return result;
}
int fang_control_window_cinematic_result(uint32_t request_id, fang_control_cinematic_snapshot* snapshot) {
 fang_rpc_control_window_cinematic_result* req = calloc(1,sizeof(*req));
 if (!req) return 0;
 req->header.version=1; req->header.size=sizeof(*req); req->header.operation=10;
req->request_id=request_id;
int is_ok=fang_proxy_call(&req->header);
if (is_ok && snapshot) *snapshot=req->snapshot;
int result=is_ok ? req->header.result : 0; free(req);
return result;
}
int fang_control_window_deployment_result(uint32_t request_id, fang_control_deployment_state* snapshot) {
 fang_rpc_control_window_deployment_result* req = calloc(1,sizeof(*req));
 if (!req) return 0;
 req->header.version=1; req->header.size=sizeof(*req); req->header.operation=11;
req->request_id=request_id;
int is_ok=fang_proxy_call(&req->header);
if (is_ok && snapshot) *snapshot=req->snapshot;
int result=is_ok ? req->header.result : 0; free(req);
return result;
}
int fang_control_window_editor_cancel(uint32_t request_id, uint32_t timeout_ms) {
 fang_rpc_control_window_editor_cancel* req = calloc(1,sizeof(*req));
 if (!req) return 0;
 req->header.version=1; req->header.size=sizeof(*req); req->header.operation=12;
req->request_id=request_id;
req->timeout_ms=timeout_ms;
req->header.deadline_tick_ms=GetTickCount64()+timeout_ms;
int is_ok=fang_proxy_call(&req->header);
int result=is_ok ? req->header.result : 0; free(req);
return result;
}
int fang_control_window_editor_hierarchy(uint32_t request_id, uint32_t timeout_ms) {
 fang_rpc_control_window_editor_hierarchy* req = calloc(1,sizeof(*req));
 if (!req) return 0;
 req->header.version=1; req->header.size=sizeof(*req); req->header.operation=13;
req->request_id=request_id;
req->timeout_ms=timeout_ms;
req->header.deadline_tick_ms=GetTickCount64()+timeout_ms;
int is_ok=fang_proxy_call(&req->header);
int result=is_ok ? req->header.result : 0; free(req);
return result;
}
int fang_control_window_editor_open(uint32_t request_id, uint32_t hero_noun, int is_import, uint32_t timeout_ms) {
 fang_rpc_control_window_editor_open* req = calloc(1,sizeof(*req));
 if (!req) return 0;
 req->header.version=1; req->header.size=sizeof(*req); req->header.operation=14;
req->request_id=request_id;
req->hero_noun=hero_noun;
req->is_import=is_import;
req->timeout_ms=timeout_ms;
req->header.deadline_tick_ms=GetTickCount64()+timeout_ms;
int is_ok=fang_proxy_call(&req->header);
int result=is_ok ? req->header.result : 0; free(req);
return result;
}
int fang_control_window_editor_tree_result(uint32_t request_id, fang_control_editor_tree* snapshot) {
 fang_rpc_control_window_editor_tree_result* req = calloc(1,sizeof(*req));
 if (!req) return 0;
 req->header.version=1; req->header.size=sizeof(*req); req->header.operation=15;
req->request_id=request_id;
int is_ok=fang_proxy_call(&req->header);
if (is_ok && snapshot) *snapshot=req->snapshot;
int result=is_ok ? req->header.result : 0; free(req);
return result;
}
int fang_control_window_inspect(fang_control_window_snapshot* snapshot) {
 fang_rpc_control_window_inspect* req = calloc(1,sizeof(*req));
 if (!req) return 0;
 req->header.version=1; req->header.size=sizeof(*req); req->header.operation=16;
int is_ok=fang_proxy_call(&req->header);
if (is_ok && snapshot) *snapshot=req->snapshot;
int result=is_ok ? req->header.result : 0; free(req);
return result;
}
int fang_control_window_map(uint32_t request_id, uint32_t timeout_ms) {
 fang_rpc_control_window_map* req = calloc(1,sizeof(*req));
 if (!req) return 0;
 req->header.version=1; req->header.size=sizeof(*req); req->header.operation=17;
req->request_id=request_id;
req->timeout_ms=timeout_ms;
req->header.deadline_tick_ms=GetTickCount64()+timeout_ms;
int is_ok=fang_proxy_call(&req->header);
int result=is_ok ? req->header.result : 0; free(req);
return result;
}
int fang_control_window_map_result(uint32_t request_id, fang_control_map_state* snapshot) {
 fang_rpc_control_window_map_result* req = calloc(1,sizeof(*req));
 if (!req) return 0;
 req->header.version=1; req->header.size=sizeof(*req); req->header.operation=18;
req->request_id=request_id;
int is_ok=fang_proxy_call(&req->header);
if (is_ok && snapshot) *snapshot=req->snapshot;
int result=is_ok ? req->header.result : 0; free(req);
return result;
}
int fang_control_window_mission(uint32_t request_id, uint32_t kind, uint32_t timeout_ms) {
 fang_rpc_control_window_mission* req = calloc(1,sizeof(*req));
 if (!req) return 0;
 req->header.version=1; req->header.size=sizeof(*req); req->header.operation=19;
req->request_id=request_id;
req->kind=kind;
req->timeout_ms=timeout_ms;
req->header.deadline_tick_ms=GetTickCount64()+timeout_ms;
int is_ok=fang_proxy_call(&req->header);
int result=is_ok ? req->header.result : 0; free(req);
return result;
}
int fang_control_window_press(uint32_t request_id, uint32_t key, uint32_t timeout_ms) {
 fang_rpc_control_window_press* req = calloc(1,sizeof(*req));
 if (!req) return 0;
 req->header.version=1; req->header.size=sizeof(*req); req->header.operation=20;
req->request_id=request_id;
req->key=key;
req->timeout_ms=timeout_ms;
req->header.deadline_tick_ms=GetTickCount64()+timeout_ms;
int is_ok=fang_proxy_call(&req->header);
int result=is_ok ? req->header.result : 0; free(req);
return result;
}
int fang_control_window_ready(uint32_t request_id, uint32_t timeout_ms) {
 fang_rpc_control_window_ready* req = calloc(1,sizeof(*req));
 if (!req) return 0;
 req->header.version=1; req->header.size=sizeof(*req); req->header.operation=21;
req->request_id=request_id;
req->timeout_ms=timeout_ms;
req->header.deadline_tick_ms=GetTickCount64()+timeout_ms;
int is_ok=fang_proxy_call(&req->header);
int result=is_ok ? req->header.result : 0; free(req);
return result;
}
int fang_control_window_result(uint32_t request_id, fang_control_window_receipt* receipt) {
 fang_rpc_control_window_result* req = calloc(1,sizeof(*req));
 if (!req) return 0;
 req->header.version=1; req->header.size=sizeof(*req); req->header.operation=22;
req->request_id=request_id;
int is_ok=fang_proxy_call(&req->header);
if (is_ok && receipt) *receipt=req->receipt;
int result=is_ok ? req->header.result : 0; free(req);
return result;
}
int fang_control_window_right_click(uint32_t request_id, int32_t x, int32_t y, uint32_t timeout_ms) {
 fang_rpc_control_window_right_click* req = calloc(1,sizeof(*req));
 if (!req) return 0;
 req->header.version=1; req->header.size=sizeof(*req); req->header.operation=23;
req->request_id=request_id;
req->x=x;
req->y=y;
req->timeout_ms=timeout_ms;
req->header.deadline_tick_ms=GetTickCount64()+timeout_ms;
int is_ok=fang_proxy_call(&req->header);
int result=is_ok ? req->header.result : 0; free(req);
return result;
}
int fang_control_window_scene(uint32_t request_id, uint32_t room, uint32_t timeout_ms) {
 fang_rpc_control_window_scene* req = calloc(1,sizeof(*req));
 if (!req) return 0;
 req->header.version=1; req->header.size=sizeof(*req); req->header.operation=24;
req->request_id=request_id;
req->room=room;
req->timeout_ms=timeout_ms;
req->header.deadline_tick_ms=GetTickCount64()+timeout_ms;
int is_ok=fang_proxy_call(&req->header);
int result=is_ok ? req->header.result : 0; free(req);
return result;
}
int fang_control_window_scene_ready(uint32_t request_id, uint32_t room, uint32_t timeout_ms) {
 fang_rpc_control_window_scene_ready* req = calloc(1,sizeof(*req));
 if (!req) return 0;
 req->header.version=1; req->header.size=sizeof(*req); req->header.operation=25;
req->request_id=request_id;
req->room=room;
req->timeout_ms=timeout_ms;
req->header.deadline_tick_ms=GetTickCount64()+timeout_ms;
int is_ok=fang_proxy_call(&req->header);
int result=is_ok ? req->header.result : 0; free(req);
return result;
}
int fang_control_window_screen(uint32_t request_id, uint32_t screen, int is_action, uint64_t expected_owner_generation, uint32_t timeout_ms) {
 fang_rpc_control_window_screen* req = calloc(1,sizeof(*req));
 if (!req) return 0;
 req->header.version=1; req->header.size=sizeof(*req); req->header.operation=26;
req->request_id=request_id;
req->screen=screen;
req->is_action=is_action;
req->expected_owner_generation=expected_owner_generation;
req->timeout_ms=timeout_ms;
req->header.deadline_tick_ms=GetTickCount64()+timeout_ms;
int is_ok=fang_proxy_call(&req->header);
int result=is_ok ? req->header.result : 0; free(req);
return result;
}
int fang_control_window_screen_result(uint32_t request_id, fang_control_screen_state* snapshot) {
 fang_rpc_control_window_screen_result* req = calloc(1,sizeof(*req));
 if (!req) return 0;
 req->header.version=1; req->header.size=sizeof(*req); req->header.operation=27;
req->request_id=request_id;
int is_ok=fang_proxy_call(&req->header);
if (is_ok && snapshot) *snapshot=req->snapshot;
int result=is_ok ? req->header.result : 0; free(req);
return result;
}
int fang_control_window_submit(uint32_t request_id, int32_t x, int32_t y, uint32_t timeout_ms) {
 fang_rpc_control_window_submit* req = calloc(1,sizeof(*req));
 if (!req) return 0;
 req->header.version=1; req->header.size=sizeof(*req); req->header.operation=28;
req->request_id=request_id;
req->x=x;
req->y=y;
req->timeout_ms=timeout_ms;
req->header.deadline_tick_ms=GetTickCount64()+timeout_ms;
int is_ok=fang_proxy_call(&req->header);
int result=is_ok ? req->header.result : 0; free(req);
return result;
}
int fang_control_window_text(uint32_t request_id, const char* text, uint32_t length, uint32_t timeout_ms) {
 fang_rpc_control_window_text* req = calloc(1,sizeof(*req));
 if (!req) return 0;
 req->header.version=1; req->header.size=sizeof(*req); req->header.operation=29;
req->request_id=request_id;
req->length=length;
req->timeout_ms=timeout_ms;
req->header.deadline_tick_ms=GetTickCount64()+timeout_ms;
if (length > sizeof(req->text)) { free(req); return 0; }
memcpy(req->text,text,length);
int is_ok=fang_proxy_call(&req->header);
int result=is_ok ? req->header.result : 0; free(req);
return result;
}

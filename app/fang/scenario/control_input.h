#ifndef DARKSPIN_CONTROL_INPUT_H
#define DARKSPIN_CONTROL_INPUT_H
#include "scenario/control.h"
#if FANG_SCENARIO
int fang_control_input_install(HMODULE executable);
int fang_control_input_press(void* caller, int key, int is_pressed);
int fang_control_input_begin(uint32_t request_id, uint32_t scene, uint32_t handle, int32_t x, int32_t y, uint64_t deadline, uint32_t hold_ms);
uint32_t fang_control_input_frame(uint32_t request_id, uint32_t scene, uint32_t handle, int is_world_active, fang_control_gameplay_click* snapshot);
void fang_control_input_cancel(uint32_t request_id);
int fang_control_input_aim_begin(uint32_t request_id, uint32_t scene, uint32_t handle, int32_t x, int32_t y, uint64_t deadline);
int fang_control_input_aim_read(uint32_t request_id, fang_control_gameplay_click* snapshot);
#endif
#endif

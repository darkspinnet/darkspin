#ifndef FANG_PROXY_H
#define FANG_PROXY_H
#include <stdlib.h>
#include "scenario/rpc.h"
typedef struct fang_proxy_target fang_proxy_target;
fang_proxy_target* fang_proxy_create(uintptr_t process, uintptr_t entry, uint32_t process_id, const char* trace);
void fang_proxy_retain(fang_proxy_target* target);
void fang_proxy_release(fang_proxy_target* target);
void fang_proxy_bind(fang_proxy_target* target);
uint32_t fang_proxy_pid(void);
const char* fang_proxy_trace(void);
int fang_proxy_call(fang_rpc_header* request);
#endif

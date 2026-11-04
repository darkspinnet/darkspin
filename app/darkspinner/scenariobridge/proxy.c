#include "proxy.h"
#include <string.h>

// These allocations and remote-call handles belong to Darkspinner. The DLL
// receives a bounded pointer-free native request, never a Go heap pointer.
struct fang_proxy_target {
    HANDLE process;
    uintptr_t entry;
    uint32_t process_id;
    LONG references;
    char trace[32768];
};
static __thread fang_proxy_target* current_target;

fang_proxy_target* fang_proxy_create(uintptr_t process, uintptr_t entry, uint32_t process_id, const char* trace) {
    fang_proxy_target* target = calloc(1, sizeof(*target));
    if (!target) return NULL;
    if (!DuplicateHandle(GetCurrentProcess(), (HANDLE)process, GetCurrentProcess(),
        &target->process, 0, FALSE, DUPLICATE_SAME_ACCESS)) { free(target); return NULL; }
    target->entry=entry; target->process_id=process_id; target->references=1;
    if (strlen(trace) >= sizeof(target->trace)) { CloseHandle(target->process); free(target); return NULL; }
    strcpy(target->trace, trace);
    return target;
}
void fang_proxy_retain(fang_proxy_target* target) { InterlockedIncrement(&target->references); }
void fang_proxy_release(fang_proxy_target* target) {
    if (InterlockedDecrement(&target->references)) return;
    CloseHandle(target->process);
    free(target);
}
void fang_proxy_bind(fang_proxy_target* target) {
    fang_proxy_target* previous=current_target;
    if (target) fang_proxy_retain(target);
    current_target=target;
    if (previous) fang_proxy_release(previous);
}
uint32_t fang_proxy_pid(void) { return current_target ? current_target->process_id : 0; }
const char* fang_proxy_trace(void) { return current_target ? current_target->trace : ""; }

typedef struct pending_call { HANDLE process, thread; void* address; } pending_call;
static DWORD WINAPI release_pending(LPVOID parameter) {
    pending_call* pending=parameter;
    DWORD status=WaitForSingleObject(pending->thread, INFINITE);
    if (status == WAIT_OBJECT_0) VirtualFreeEx(pending->process, pending->address, 0, MEM_RELEASE);
    CloseHandle(pending->thread); CloseHandle(pending->process); free(pending);
    return 0;
}

int fang_proxy_call(fang_rpc_header* request) {
    fang_proxy_target* target=current_target;
    SIZE_T count=0;
    DWORD status, code=1;
    int is_ok=0;
    pending_call* pending;
    HANDLE reaper;
    if (!target || request->size < sizeof(*request) || request->size > 256*1024) return 0;
    if (!request->deadline_tick_ms) request->deadline_tick_ms=GetTickCount64()+2000;
    pending=calloc(1,sizeof(*pending));
    if (!pending) return 0;
    if (!DuplicateHandle(GetCurrentProcess(), target->process, GetCurrentProcess(),
        &pending->process,0,FALSE,DUPLICATE_SAME_ACCESS)) { free(pending); return 0; }
    pending->address=VirtualAllocEx(pending->process,NULL,request->size,MEM_COMMIT|MEM_RESERVE,PAGE_READWRITE);
    if (!pending->address) goto cleanup;
    if (!WriteProcessMemory(pending->process,pending->address,request,request->size,&count) || count != request->size) goto cleanup;
    pending->thread=CreateRemoteThread(pending->process,NULL,0,(LPTHREAD_START_ROUTINE)target->entry,pending->address,0,NULL);
    if (!pending->thread) goto cleanup;
    status=WaitForSingleObject(pending->thread,5000);
    if (status != WAIT_OBJECT_0) {
        // Never release memory while the remote thread could still use it.
        reaper=CreateThread(NULL,0,release_pending,pending,0,NULL);
        if (reaper) CloseHandle(reaper);
        else OutputDebugStringA("Darkspinner: pending scenario call retained until process exit\n");
        return 0;
    }
    if (!GetExitCodeThread(pending->thread,&code) || code != 0) goto cleanup;
    is_ok=ReadProcessMemory(pending->process,pending->address,request,request->size,&count) && count==request->size;
cleanup:
    if (pending->thread) CloseHandle(pending->thread);
    if (pending->address) VirtualFreeEx(pending->process,pending->address,0,MEM_RELEASE);
    CloseHandle(pending->process); free(pending);
    return is_ok;
}

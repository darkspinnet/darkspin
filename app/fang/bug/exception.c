#include "bug/bug.h"
#include "fang.h"

#include <stdint.h>
#include <stdio.h>

static HANDLE exception_trace = INVALID_HANDLE_VALUE;
static PVOID exception_observer;
static volatile LONG exception_trace_active;
static uintptr_t exception_executable_base;
static uintptr_t exception_fang_base;

// Walk address-space metadata without allocating or touching region contents.
// Aggregate free bytes cannot reveal fragmentation in the 32-bit client.
static SIZE_T largest_free_region(DWORD* region_count, DWORD* scan_error) {
    SYSTEM_INFO system;
    MEMORY_BASIC_INFORMATION region;
    uintptr_t address;
    uintptr_t limit;
    SIZE_T largest = 0;
    DWORD query_count = 0;
    GetSystemInfo(&system);
    address = (uintptr_t)system.lpMinimumApplicationAddress;
    limit = (uintptr_t)system.lpMaximumApplicationAddress;
    *region_count = 0;
    *scan_error = 0;
    while (address < limit) {
        uintptr_t next;
        SIZE_T queried;
        if (++query_count > 65536) {
            *scan_error = ERROR_MORE_DATA;
            break;
        }
        queried = VirtualQuery((LPCVOID)address, &region, sizeof(region));
        if (queried != sizeof(region)) {
            *scan_error = GetLastError();
            if (*scan_error == 0) *scan_error = ERROR_INVALID_DATA;
            break;
        }
        if (region.State == MEM_FREE) {
            (*region_count)++;
            if (region.RegionSize > largest) largest = region.RegionSize;
        }
        next = (uintptr_t)region.BaseAddress + region.RegionSize;
        if (next <= address) {
            *scan_error = ERROR_ARITHMETIC_OVERFLOW;
            break;
        }
        address = next;
    }
    return largest;
}

// Read bounded diagnostic bytes without dereferencing a possibly corrupt HUD.
// Keep this independent of the normal trace lock and allocation paths.
static void trace_fault_memory(const char* role, uintptr_t address, SIZE_T size) {
    static const char digits[] = "0123456789abcdef";
    BYTE payload[64] = {0};
    char payload_hex[129];
    char line[512];
    SIZE_T bytes_read = 0;
    DWORD read_error = 0;
    DWORD written = 0;
    BOOL is_read;
    BOOL is_written;
    SIZE_T index;
    int length;

    if (size > sizeof(payload)) return;
    is_read = ReadProcessMemory(GetCurrentProcess(), (LPCVOID)address,
        payload, size, &bytes_read);
    if (!is_read) read_error = GetLastError();
    if (bytes_read > size) bytes_read = size;
    for (index = 0; index < bytes_read; index++) {
        payload_hex[index * 2] = digits[payload[index] >> 4];
        payload_hex[index * 2 + 1] = digits[payload[index] & 15];
    }
    payload_hex[bytes_read * 2] = '\0';
    length = snprintf(line, sizeof(line),
        "{\"time_ms\":%lu,\"protocol\":\"client_exception\","
        "\"kind\":\"catalyst_hud_fault_memory\",\"thread\":%lu,"
        "\"role\":\"%s\",\"address\":\"0x%llx\","
        "\"requested_bytes\":%llu,\"read_bytes\":%llu,"
        "\"is_read\":%s,\"read_error\":%lu,\"payload_hex\":\"%s\"}\r\n",
        GetTickCount(), GetCurrentThreadId(), role,
        (unsigned long long)address, (unsigned long long)size,
        (unsigned long long)bytes_read, is_read ? "true" : "false",
        read_error, payload_hex);
    if (length <= 0 || (size_t)length >= sizeof(line)) return;
    is_written = WriteFile(exception_trace, line, (DWORD)length, &written, NULL);
    if (!is_written || written != (DWORD)length)
        OutputDebugStringA("Fang: catalyst HUD fault trace write failed\n");
}

static void trace_catalyst_hud_fault(const CONTEXT* context,
    const uintptr_t* stack_words, SIZE_T stack_bytes) {
#if !defined(_WIN64)
    // Build 103: 54D240 pushes EDI and reserves 0x34 bytes before 54D277.
    // The return address is stack word 14; createGrid's four GFx values are
    // addressed by word 18. 41CEDF is the return in MaxisHUDCatalysts init.
    if (context->Eip != exception_executable_base + 0x14D277 ||
        context->Edi != 0 || stack_bytes < 21 * sizeof(stack_words[0]) ||
        stack_words[14] != exception_executable_base + 0x1CEDF ||
        stack_words[17] != exception_executable_base + 0xBCEA08 ||
        stack_words[19] != 4) return;

    // The caller retains the component in EDI (saved at stack word 0) and
    // its second GFx value in ESI. Verify that relation before sampling it.
    if (context->Esi >= 24 && stack_words[0] == context->Esi - 24)
        trace_fault_memory("component", stack_words[0], 64);
    trace_fault_memory("invoke_arguments", stack_words[18], 64);
#else
    (void)context;
    (void)stack_words;
    (void)stack_bytes;
#endif
}

// First-chance observation only: preserve the original exception and let the
// client's handlers (or Windows) decide whether it is fatal. Avoid the normal
// trace lock and Go callbacks: the fault may have interrupted either of them.
// Reads/modifies: samples first-chance fault state, writes diagnostics, and updates the reentrancy guard before continuing exception search.
static LONG CALLBACK observe_exception(EXCEPTION_POINTERS* exception) {
    EXCEPTION_RECORD* record;
    CONTEXT* context;
    MEMORY_BASIC_INFORMATION region;
    MEMORYSTATUSEX memory_status = {0};
    uintptr_t instruction;
    uintptr_t stack_pointer;
    uintptr_t frame_pointer;
    uintptr_t allocation_base = 0;
    uintptr_t access_kind = 0;
    uintptr_t access_address = 0;
    uintptr_t stack_words[64];
    SIZE_T stack_bytes = 0;
    SIZE_T region_size;
    DWORD written = 0;
    char line[4096];
    int length;
    size_t index;
    BOOL is_read;
    BOOL is_written;
    BOOL is_memory_status_available;
    DWORD memory_status_error = 0;
    DWORD free_region_count = 0;
    DWORD virtual_scan_error = 0;
    SIZE_T largest_free_region_bytes;

    if (exception == NULL || exception->ExceptionRecord == NULL ||
        exception->ContextRecord == NULL || exception_trace == INVALID_HANDLE_VALUE) {
        return EXCEPTION_CONTINUE_SEARCH;
    }
    record = exception->ExceptionRecord;
    if (record->ExceptionCode != EXCEPTION_ACCESS_VIOLATION &&
        record->ExceptionCode != EXCEPTION_IN_PAGE_ERROR &&
        record->ExceptionCode != EXCEPTION_ILLEGAL_INSTRUCTION) {
        return EXCEPTION_CONTINUE_SEARCH;
    }
    if (InterlockedCompareExchange(&exception_trace_active, 1, 0) != 0) {
        return EXCEPTION_CONTINUE_SEARCH;
    }
    context = exception->ContextRecord;
#if defined(_WIN64)
    instruction = context->Rip;
    stack_pointer = context->Rsp;
    frame_pointer = context->Rbp;
#else
    instruction = context->Eip;
    stack_pointer = context->Esp;
    frame_pointer = context->Ebp;
#endif
    region_size = VirtualQuery((LPCVOID)instruction, &region, sizeof(region));
    if (region_size == sizeof(region)) {
        allocation_base = (uintptr_t)region.AllocationBase;
    }
    if (record->NumberParameters >= 2) {
        access_kind = record->ExceptionInformation[0];
        access_address = record->ExceptionInformation[1];
    }
    is_read = ReadProcessMemory(GetCurrentProcess(), (LPCVOID)stack_pointer,
        stack_words, sizeof(stack_words), &stack_bytes);
    if (!is_read) {
        // Partial reads still contain useful stack words; no raw dereference
        // is safe here, particularly if the fault itself involved the stack.
        stack_bytes -= stack_bytes % sizeof(stack_words[0]);
    }
    memory_status.dwLength = sizeof(memory_status);
    is_memory_status_available = GlobalMemoryStatusEx(&memory_status);
    if (!is_memory_status_available) {
        memory_status_error = GetLastError();
    }
    largest_free_region_bytes = largest_free_region(&free_region_count, &virtual_scan_error);
    length = snprintf(line, sizeof(line),
        "{\"time_ms\":%lu,\"protocol\":\"client_exception\","
        "\"kind\":\"first_chance\",\"thread\":%lu,\"code\":\"0x%08lx\","
        "\"instruction\":\"0x%llx\",\"allocation_base\":\"0x%llx\","
        "\"executable_base\":\"0x%llx\",\"fang_base\":\"0x%llx\","
        "\"access_kind\":%llu,\"access_address\":\"0x%llx\","
        "\"stack_pointer\":\"0x%llx\",\"frame_pointer\":\"0x%llx\","
        "\"is_memory_status_available\":%s,\"memory_status_error\":%lu,"
        "\"total_virtual_bytes\":%llu,\"available_virtual_bytes\":%llu,"
        "\"available_physical_bytes\":%llu,\"available_commit_bytes\":%llu,"
        "\"largest_free_region_bytes\":%llu,\"free_region_count\":%lu,"
        "\"is_virtual_scan_complete\":%s,\"virtual_scan_error\":%lu,"
#if !defined(_WIN64)
        "\"eax\":\"0x%lx\",\"ebx\":\"0x%lx\",\"ecx\":\"0x%lx\","
        "\"edx\":\"0x%lx\",\"esi\":\"0x%lx\",\"edi\":\"0x%lx\","
#endif
        "\"stack_words\":[",
        GetTickCount(), GetCurrentThreadId(), record->ExceptionCode,
        (unsigned long long)instruction, (unsigned long long)allocation_base,
        (unsigned long long)exception_executable_base,
        (unsigned long long)exception_fang_base,
        (unsigned long long)access_kind, (unsigned long long)access_address,
        (unsigned long long)stack_pointer, (unsigned long long)frame_pointer,
        is_memory_status_available ? "true" : "false", memory_status_error,
        (unsigned long long)memory_status.ullTotalVirtual,
        (unsigned long long)memory_status.ullAvailVirtual,
        (unsigned long long)memory_status.ullAvailPhys,
        (unsigned long long)memory_status.ullAvailPageFile,
        (unsigned long long)largest_free_region_bytes, free_region_count,
        virtual_scan_error == 0 ? "true" : "false", virtual_scan_error
#if !defined(_WIN64)
        , context->Eax, context->Ebx, context->Ecx,
        context->Edx, context->Esi, context->Edi
#endif
    );
    if (length <= 0 || (size_t)length >= sizeof(line) - 5) {
        InterlockedExchange(&exception_trace_active, 0);
        return EXCEPTION_CONTINUE_SEARCH;
    }
    for (index = 0; index < stack_bytes / sizeof(stack_words[0]); index++) {
        int added = snprintf(line + length, sizeof(line) - (size_t)length,
            "%s\"0x%llx\"", index == 0 ? "" : ",",
            (unsigned long long)stack_words[index]);
        if (added <= 0 || (size_t)added >= sizeof(line) - (size_t)length - 5) {
            InterlockedExchange(&exception_trace_active, 0);
            return EXCEPTION_CONTINUE_SEARCH;
        }
        length += added;
    }
    line[length++] = ']';
    line[length++] = '}';
    line[length++] = '\r';
    line[length++] = '\n';
    is_written = WriteFile(exception_trace, line, (DWORD)length, &written, NULL);
    if (!is_written || written != (DWORD)length) {
        OutputDebugStringA("Fang: exception trace write failed\n");
    }
    trace_catalyst_hud_fault(context, stack_words, stack_bytes);
    InterlockedExchange(&exception_trace_active, 0);
    return EXCEPTION_CONTINUE_SEARCH;
}

// Modifies: stores trace metadata and registers a first-chance vectored exception observer.
int fang_install_exception_trace(HANDLE trace, HMODULE executable) {
    MEMORY_BASIC_INFORMATION region;
    SIZE_T region_size;
    if (trace == INVALID_HANDLE_VALUE || trace == NULL) {
        return 0;
    }
    if (exception_observer != NULL) {
        return 1;
    }
    exception_trace = trace;
    exception_executable_base = (uintptr_t)executable;
    region_size = VirtualQuery((LPCVOID)observe_exception, &region, sizeof(region));
    if (region_size == sizeof(region)) {
        exception_fang_base = (uintptr_t)region.AllocationBase;
    }
    exception_observer = AddVectoredExceptionHandler(1, observe_exception);
    return exception_observer != NULL;
}

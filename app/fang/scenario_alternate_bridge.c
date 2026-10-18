#include "scenario_alternate.h"
#if FANG_SCENARIO
#if !defined(__GNUC__) || !defined(__i386__)
#error scenario alternate bridge requires GNU x86 assembly
#endif

// Exact selected-call ABI: ESI entry, owner/set/ordinal stack words, plain ret.
// This body has no C statement/prologue, SEH registration or native exception
// handler. An original exceptional unwind follows its original FS:0 chain;
// it never visits a diagnostic normal-return path. No lock/heap lease is held
// across the original. The parent S/arguments remain in their original slots.
// Actual emitted ABI, probes/XSTATE and native SEH compatibility need review
// before installation is executed; these comments are not compiled proof.
__attribute__((naked, noinline)) void __cdecl scenario_alternate_bridge(void) {
    __asm__ volatile(
        ".intel_syntax noprefix\n"
        // Preserve flags before every arithmetic/feature/probe operation.
        "pushfd\n"
        "pushad\n"
        "lea eax, [esp+36]\n"       // S, with original pushad block at S-36.
        "test DWORD PTR [eax-4096], eax\n"
        "test DWORD PTR [eax-8192], eax\n"
        "test DWORD PTR [eax-9264], eax\n" // Includes lower 48 temporary bytes.
        "sub esp, 9180\n"           // F=S-9216.
        "mov ebp, esp\n"
        "mov DWORD PTR [ebp+8728], eax\n"
        "mov DWORD PTR [ebp+8324], 0\n" // No post telemetry on a bypass/cap.
        "mov eax, DWORD PTR [_scenario_alternate_mode]\n"
        "mov DWORD PTR [ebp+8724], eax\n"
        "mov eax, DWORD PTR [_scenario_alternate_mask_low]\n"
        "mov DWORD PTR [ebp+8712], eax\n"
        "mov eax, DWORD PTR [_scenario_alternate_mask_high]\n"
        "mov DWORD PTR [ebp+8716], eax\n"
        "mov eax, DWORD PTR [_scenario_alternate_state_size]\n"
        "mov DWORD PTR [ebp+8720], eax\n"
        "call .Lalternate_validate\n"
        "test eax, eax\n"
        "jz .Lalternate_before_bypass\n"
        "call .Lalternate_save_state\n"
        // C gets a temporary default FP environment only after full capture.
        "cld\n"
        "fninit\n"
        "ldmxcsr DWORD PTR [_scenario_alternate_default_mxcsr]\n"
        "and esp, -16\n"
        "push DWORD PTR [ebp+9228]\n" // Ordinal, raw set, saved original ESI.
        "push DWORD PTR [ebp+9224]\n"
        "push DWORD PTR [ebp+9184]\n"
        "push ebp\n"
        "call _scenario_alternate_before\n"
        "mov esp, ebp\n"
        "call .Lalternate_restore_state\n"
        "jmp .Lalternate_forward\n"

        ".Lalternate_before_bypass:\n"
        "lock or DWORD PTR [_scenario_alternate_state_loss], 32\n"
        // Integer-only bypass has not touched FP/XSTATE or LastError.
        ".Lalternate_forward:\n"
        "mov eax, DWORD PTR [ebp+9220]\n"
        "mov DWORD PTR [ebp-12], eax\n"
        "mov eax, DWORD PTR [ebp+9224]\n"
        "mov DWORD PTR [ebp-8], eax\n"
        "mov eax, DWORD PTR [ebp+9228]\n"
        "mov DWORD PTR [ebp-4], eax\n"
        "mov ecx, 8\n"
        ".Lalternate_input_register_copy:\n"
        "mov eax, DWORD PTR [ebp+9180+ecx*4]\n"
        "mov DWORD PTR [ebp-48+ecx*4], eax\n"
        "dec ecx\n"
        "jns .Lalternate_input_register_copy\n"
        "lea esp, [ebp-48]\n"
        "popad\n"                 // Deliberately skips stored pushad ESP.
        "popfd\n"                 // ESP=F-12, no flag-changing cleanup.
        "call DWORD PTR [_scenario_alternate_original]\n" // Exactly once.

        // Original plain ret leaves ESP=F-12; capture flags/GPR immediately.
        "pushfd\n"
        "pushad\n"                // Returned block at F-48..F-13.
        "lea ebp, [esp+48]\n"
        "mov ecx, 8\n"
        ".Lalternate_return_register_copy:\n"
        "mov eax, DWORD PTR [ebp-48+ecx*4]\n"
        "mov DWORD PTR [ebp+8768+ecx*4], eax\n"
        "dec ecx\n"
        "jns .Lalternate_return_register_copy\n"
        "mov esp, ebp\n"
        "cmp DWORD PTR [ebp+8324], 0\n"
        "je .Lalternate_return\n"   // Cap/bypass: no post C/state change.
        "call .Lalternate_validate\n"
        "test eax, eax\n"
        "jz .Lalternate_after_bypass\n"
        "call .Lalternate_save_state\n"
        "cld\n"
        "fninit\n"
        "ldmxcsr DWORD PTR [_scenario_alternate_default_mxcsr]\n"
        "and esp, -16\n"
        "sub esp, 12\n"
        "push ebp\n"
        "call _scenario_alternate_after\n"
        "mov esp, ebp\n"
        "call .Lalternate_restore_state\n"
        "jmp .Lalternate_return\n"
        ".Lalternate_after_bypass:\n"
        "lock or DWORD PTR [_scenario_alternate_state_loss], 32\n"
        // No post C: preserve actual native XSTATE and leave entry unmatched.
        ".Lalternate_return:\n"
        "mov ecx, 8\n"
        ".Lalternate_parent_register_copy:\n"
        "mov eax, DWORD PTR [ebp+8768+ecx*4]\n"
        "mov DWORD PTR [ebp+9180+ecx*4], eax\n"
        "dec ecx\n"
        "jns .Lalternate_parent_register_copy\n"
        "lea esp, [ebp+9180]\n"
        "popad\n"
        "popfd\n"                 // ESP=S; returned EAX/flags untouched.
        "ret\n"                   // Parent retains its original add ESP,12.

        // No C/SIMD: validate current CPU/OS mode/mask/size before capture.
        ".Lalternate_validate:\n"
        "xor eax, eax\n"
        "cpuid\n"
        "cmp eax, 1\n"
        "jb .Lalternate_invalid\n"
        "mov esi, eax\n"
        "mov eax, 1\n"
        "cpuid\n"
        "and edx, 0x03000000\n"
        "cmp edx, 0x03000000\n"
        "jne .Lalternate_invalid\n"
        "cmp DWORD PTR [ebp+8724], 1\n"
        "jne .Lalternate_validate_legacy\n"
        "and ecx, 0x0c000000\n"
        "cmp ecx, 0x0c000000\n"
        "jne .Lalternate_invalid\n"
        "cmp esi, 13\n"
        "jb .Lalternate_invalid\n"
        "xor ecx, ecx\n"
        "xgetbv\n"
        "cmp eax, DWORD PTR [ebp+8712]\n"
        "jne .Lalternate_invalid\n"
        "cmp edx, DWORD PTR [ebp+8716]\n"
        "jne .Lalternate_invalid\n"
        "mov eax, 13\n"
        "xor ecx, ecx\n"
        "cpuid\n"
        "cmp ebx, DWORD PTR [ebp+8720]\n"
        "jne .Lalternate_invalid\n"
        "cmp ebx, 8192\n"
        "ja .Lalternate_invalid\n"
        "cmp ebx, 576\n"
        "jb .Lalternate_invalid\n"
        "jmp .Lalternate_valid\n"
        ".Lalternate_validate_legacy:\n"
        "cmp DWORD PTR [ebp+8724], 2\n"
        "jne .Lalternate_invalid\n"
        "test ecx, 0x08000000\n"    // No enabled extended-state fallback.
        "jnz .Lalternate_invalid\n"
        ".Lalternate_valid:\n"
        "mov eax, 1\n"
        "ret\n"
        ".Lalternate_invalid:\n"
        "xor eax, eax\n"
        "ret\n"

        ".Lalternate_save_state:\n"
        "lea edi, [ebp+63]\n"
        "and edi, -64\n"
        "mov ecx, 2047\n"
        "xor eax, eax\n"
        ".Lalternate_state_zero:\n"
        "mov DWORD PTR [edi+ecx*4], eax\n" // MOV only, independent of DF.
        "dec ecx\n"
        "jns .Lalternate_state_zero\n"
        "cmp DWORD PTR [ebp+8724], 1\n"
        "jne .Lalternate_save_legacy\n"
        "mov eax, DWORD PTR [ebp+8712]\n"
        "mov edx, DWORD PTR [ebp+8716]\n"
        "xsave [edi]\n"
        "ret\n"
        ".Lalternate_save_legacy:\n"
        "fxsave [edi]\n"
        "ret\n"

        ".Lalternate_restore_state:\n"
        "lea edi, [ebp+63]\n"
        "and edi, -64\n"
        "cmp DWORD PTR [ebp+8724], 1\n"
        "jne .Lalternate_restore_legacy\n"
        "mov eax, DWORD PTR [ebp+8712]\n"
        "mov edx, DWORD PTR [ebp+8716]\n"
        "xrstor [edi]\n"
        "ret\n"
        ".Lalternate_restore_legacy:\n"
        "fxrstor [edi]\n"
        "ret\n"
        ".att_syntax prefix\n"
    );
}
#endif

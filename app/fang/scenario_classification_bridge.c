#include "scenario_classification.h"
#if FANG_SCENARIO
#if !defined(__GNUC__) || !defined(__i386__)
#error scenario classification bridge requires GNU x86 assembly
#endif

// Exact cdecl ABI: one entry stack word, plain ret; parent pops4.
// This body has no C statement/prologue, SEH registration or native exception
// handler. An original exceptional unwind follows its original FS:0 chain;
// it never visits a diagnostic normal-return path. No lock/heap lease is held
// across the original. The parent S/arguments remain in their original slots.
// Actual emitted ABI, probes/XSTATE and native SEH compatibility need review
// before installation is executed; these comments are not compiled proof.
__attribute__((naked, noinline)) void __cdecl scenario_classification_bridge(void) {
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
        "mov eax, DWORD PTR [_scenario_classification_mode]\n"
        "mov DWORD PTR [ebp+8724], eax\n"
        "mov eax, DWORD PTR [_scenario_classification_mask_low]\n"
        "mov DWORD PTR [ebp+8712], eax\n"
        "mov eax, DWORD PTR [_scenario_classification_mask_high]\n"
        "mov DWORD PTR [ebp+8716], eax\n"
        "mov eax, DWORD PTR [_scenario_classification_state_size]\n"
        "mov DWORD PTR [ebp+8720], eax\n"
        "call .Lclassification_validate\n"
        "test eax, eax\n"
        "jz .Lclassification_before_bypass\n"
        "call .Lclassification_save_state\n"
        // C gets a temporary default FP environment only after full capture.
        "cld\n"
        "fninit\n"
        "ldmxcsr DWORD PTR [_scenario_classification_default_mxcsr]\n"
        "and esp, -16\n"
        "sub esp, 8\n"
        "push DWORD PTR [ebp+9220]\n" // Actual original one-word entry argument.
        "push ebp\n"
        "call _scenario_classification_before\n"
        "mov esp, ebp\n"
        "call .Lclassification_restore_state\n"
        "jmp .Lclassification_forward\n"

        ".Lclassification_before_bypass:\n"
        "lock or DWORD PTR [_scenario_classification_state_loss], 32\n"
        // Integer-only bypass has not touched FP/XSTATE or LastError.
        ".Lclassification_forward:\n"
        "mov eax, DWORD PTR [ebp+9220]\n"
        "mov DWORD PTR [ebp-4], eax\n"
        "mov ecx, 8\n"
        ".Lclassification_input_register_copy:\n"
        "mov eax, DWORD PTR [ebp+9180+ecx*4]\n"
        "mov DWORD PTR [ebp-40+ecx*4], eax\n"
        "dec ecx\n"
        "jns .Lclassification_input_register_copy\n"
        "lea esp, [ebp-40]\n"
        "popad\n"                 // Deliberately skips stored pushad ESP.
        "popfd\n"                 // ESP=F-4, no flag-changing cleanup.
        "call DWORD PTR [_scenario_classification_original]\n" // Exactly once.

        // Original plain ret leaves ESP=F-4; capture flags/GPR immediately.
        "pushfd\n"
        "pushad\n"                // Returned block at F-40..F-5.
        "lea ebp, [esp+40]\n"
        "mov ecx, 8\n"
        ".Lclassification_return_register_copy:\n"
        "mov eax, DWORD PTR [ebp-40+ecx*4]\n"
        "mov DWORD PTR [ebp+8768+ecx*4], eax\n"
        "dec ecx\n"
        "jns .Lclassification_return_register_copy\n"
        "mov esp, ebp\n"
        "cmp DWORD PTR [ebp+8324], 0\n"
        "je .Lclassification_return\n"   // Cap/bypass: no post C/state change.
        "call .Lclassification_validate\n"
        "test eax, eax\n"
        "jz .Lclassification_after_bypass\n"
        "call .Lclassification_save_state\n"
        "cld\n"
        "fninit\n"
        "ldmxcsr DWORD PTR [_scenario_classification_default_mxcsr]\n"
        "and esp, -16\n"
        "sub esp, 12\n"
        "push ebp\n"
        "call _scenario_classification_after\n"
        "mov esp, ebp\n"
        "call .Lclassification_restore_state\n"
        "jmp .Lclassification_return\n"
        ".Lclassification_after_bypass:\n"
        "lock or DWORD PTR [_scenario_classification_state_loss], 32\n"
        // No post C: preserve actual native XSTATE and leave entry unmatched.
        ".Lclassification_return:\n"
        "mov ecx, 8\n"
        ".Lclassification_parent_register_copy:\n"
        "mov eax, DWORD PTR [ebp+8768+ecx*4]\n"
        "mov DWORD PTR [ebp+9180+ecx*4], eax\n"
        "dec ecx\n"
        "jns .Lclassification_parent_register_copy\n"
        "lea esp, [ebp+9180]\n"
        "popad\n"
        "popfd\n"                 // ESP=S; returned EAX/flags untouched.
        "ret\n"                   // Parent retains its original add ESP,4.

        // No C/SIMD: validate current CPU/OS mode/mask/size before capture.
        ".Lclassification_validate:\n"
        "xor eax, eax\n"
        "cpuid\n"
        "cmp eax, 1\n"
        "jb .Lclassification_invalid\n"
        "mov esi, eax\n"
        "mov eax, 1\n"
        "cpuid\n"
        "and edx, 0x03000000\n"
        "cmp edx, 0x03000000\n"
        "jne .Lclassification_invalid\n"
        "cmp DWORD PTR [ebp+8724], 1\n"
        "jne .Lclassification_validate_legacy\n"
        "and ecx, 0x0c000000\n"
        "cmp ecx, 0x0c000000\n"
        "jne .Lclassification_invalid\n"
        "cmp esi, 13\n"
        "jb .Lclassification_invalid\n"
        "xor ecx, ecx\n"
        "xgetbv\n"
        "cmp eax, DWORD PTR [ebp+8712]\n"
        "jne .Lclassification_invalid\n"
        "cmp edx, DWORD PTR [ebp+8716]\n"
        "jne .Lclassification_invalid\n"
        "mov eax, 13\n"
        "xor ecx, ecx\n"
        "cpuid\n"
        "cmp ebx, DWORD PTR [ebp+8720]\n"
        "jne .Lclassification_invalid\n"
        "cmp ebx, 8192\n"
        "ja .Lclassification_invalid\n"
        "cmp ebx, 576\n"
        "jb .Lclassification_invalid\n"
        "jmp .Lclassification_valid\n"
        ".Lclassification_validate_legacy:\n"
        "cmp DWORD PTR [ebp+8724], 2\n"
        "jne .Lclassification_invalid\n"
        "test ecx, 0x08000000\n"    // No enabled extended-state fallback.
        "jnz .Lclassification_invalid\n"
        ".Lclassification_valid:\n"
        "mov eax, 1\n"
        "ret\n"
        ".Lclassification_invalid:\n"
        "xor eax, eax\n"
        "ret\n"

        ".Lclassification_save_state:\n"
        "lea edi, [ebp+63]\n"
        "and edi, -64\n"
        "mov ecx, 2047\n"
        "xor eax, eax\n"
        ".Lclassification_state_zero:\n"
        "mov DWORD PTR [edi+ecx*4], eax\n" // MOV only, independent of DF.
        "dec ecx\n"
        "jns .Lclassification_state_zero\n"
        "cmp DWORD PTR [ebp+8724], 1\n"
        "jne .Lclassification_save_legacy\n"
        "mov eax, DWORD PTR [ebp+8712]\n"
        "mov edx, DWORD PTR [ebp+8716]\n"
        "xsave [edi]\n"
        "ret\n"
        ".Lclassification_save_legacy:\n"
        "fxsave [edi]\n"
        "ret\n"

        ".Lclassification_restore_state:\n"
        "lea edi, [ebp+63]\n"
        "and edi, -64\n"
        "cmp DWORD PTR [ebp+8724], 1\n"
        "jne .Lclassification_restore_legacy\n"
        "mov eax, DWORD PTR [ebp+8712]\n"
        "mov edx, DWORD PTR [ebp+8716]\n"
        "xrstor [edi]\n"
        "ret\n"
        ".Lclassification_restore_legacy:\n"
        "fxrstor [edi]\n"
        "ret\n"
        ".att_syntax prefix\n"
    );
}
#endif

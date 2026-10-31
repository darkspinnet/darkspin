#include "chat/ui.h"
#include "hook.h"

typedef struct chat_value {
    void* object_interface;
    unsigned int type;
    union { double number; const char* string; void* object; } data;
} chat_value;

typedef unsigned char (FANG_THISCALL* chat_invoke_fn)(chat_value*,
    const char*, chat_value*, const chat_value*, unsigned int);

static chat_invoke_fn invoke_chat;

/* Build 103: 40B3C0 initializes channel +48 to 9 (Lobby). 40A8A0 changes
 * it to Party (7) or General (8) on scene transitions. These two cdecl call
 * sites only select the default channel; explicit /party and /whisper
 * commands keep their native routing. SetChannelData also updates the movie. */
static void __cdecl select_lobby_chat(BYTE* chat, int scene) {
    chat_value arguments[3] = {0};
    (void)scene;
    if (!readable_range(chat, 52) || !chat[40]) return;
    arguments[0].type = 3;
    arguments[0].data.number = 9;
    arguments[1].type = 4;
    arguments[1].data.string = "";
    arguments[2].type = 2;
    arguments[2].data.number = 0;
    *(unsigned char*)&arguments[2].data = 1;
    if (invoke_chat((chat_value*)(chat + 24), "SetChannelData", NULL, arguments, 3)) {
        *(unsigned int*)(chat + 48) = 9;
        trace_client_state("chat_default_lobby", 1);
    }
}

int fang_install_lobby_chat(BYTE* base) {
    void* native_select = base + 0xA8A0;
    invoke_chat = (chat_invoke_fn)(base + 0x15A0);
    if (!patch_call(base + 0x55339, native_select, select_lobby_chat)) return 0;
    if (!patch_call(base + 0x554C0, native_select, select_lobby_chat)) {
        if (!patch_call(base + 0x55339, select_lobby_chat, native_select))
            trace_client_state("chat_default_rollback_failed", 1);
        return 0;
    }
    return 1;
}

#include "login/login.h"
#include "hook.h"
#include <limits.h>

typedef struct login_value {
    void* object_interface;
    unsigned int type;
    union { double number; const char* string; void* object; } data;
} login_value;
typedef char login_value_size[sizeof(login_value) == 16 ? 1 : -1];
typedef unsigned char (FANG_THISCALL* login_member_fn)(void*, void*,
    const char*, login_value*, int);
typedef unsigned char (FANG_THISCALL* login_invoke_fn)(login_value*,
    const char*, login_value*, const login_value*, unsigned int);
typedef void (FANG_THISCALL* login_release_fn)(login_value*);

static void login_account_text(char* account, unsigned int capacity) {
    WCHAR account_wide[256];
    DWORD length = GetEnvironmentVariableW(L"DARKSPIN_CLIENT_ACCOUNT", account_wide,
        sizeof(account_wide) / sizeof(account_wide[0]));
    if (length > 0 && length < sizeof(account_wide) / sizeof(account_wide[0]) &&
        WideCharToMultiByte(CP_UTF8, 0, account_wide, -1, account, capacity, NULL, NULL) > 0) {
        return;
    }
    lstrcpynA(account, "Darkspinner", (int)capacity);
}

static int login_member(login_member_fn get, login_value* parent,
    const char* name, login_value* result) {
    unsigned int type = parent->type & 0x8f;
    if (type < 6 || type > 8 || !readable_range(parent->object_interface, sizeof(void*))) {
        return 0;
    }
    return get(parent->object_interface, parent->data.object, name, result, type == 8);
}

static int login_property(login_member_fn set, login_value* parent,
    const char* name, login_value* property) {
    unsigned int type = parent->type & 0x8f;
    if (type < 6 || type > 8 || !readable_range(parent->object_interface, sizeof(void*))) return 0;
    return set(parent->object_interface, parent->data.object, name, property, type == 8);
}

/* Build 103 LoginScreen.swf, resource 0x69e51800: reproduce only the
 * presentation half of OnLoginClicked. Calling that handler itself would
 * submit the empty form a second time and expose the token to Flash fields.
 * Native DisplayError and scene teardown retain their normal ownership. */
int fang_login_processing(void* controller) {
    BYTE* base = (BYTE*)GetModuleHandleW(NULL);
    login_value* screen;
    login_value* data;
    login_value root = {0}, content = {0}, frame = {0}, current_frame = {0}, no_submit = {0}, gdata = {0};
    login_member_fn get;
    login_member_fn set;
    login_invoke_fn invoke;
    login_release_fn release;
    int is_started = 0;
    unsigned int fields = 0;
    static unsigned int previous_fields = UINT_MAX;
    if (!base || !readable_range(controller, 40)) {
        return 0;
    }
    get = (login_member_fn)(base + 0x14dae0);
    set = (login_member_fn)(base + 0x14b510);
    invoke = (login_invoke_fn)(base + 0x15a0);
    release = (login_release_fn)(base + 0x17c0);
    /* sub_514530 invokes the LoginScreen instance at controller+24. */
    screen = (login_value*)((BYTE*)controller + 24);
    if (!login_member(get, screen, "mRoot", &root) ||
        !login_member(get, &root, "loginMasterContentMC", &content)) {
        goto done;
    }
    data = screen;
    if (login_member(get, &root, "gData", &gdata) &&
        (gdata.type & 0x8f) >= 6 && (gdata.type & 0x8f) <= 8) {
        data = &gdata;
    }
    /* Changing the frame can reconstruct the input controls. Populate their
     * current instances AFTER the jump, not the discarded pre-jump objects. */
    frame.type = 3;
    frame.data.number = 90;
    is_started = login_member(get, &content, "_currentframe", &current_frame) &&
        (current_frame.type & 0x8f) == 3 && current_frame.data.number == frame.data.number;
    if (!is_started) is_started = invoke(&content, "gotoAndStop", NULL, &frame, 1);
    /* Installed build 103 OnLoginSuccessful is an empty function. Reuse it
     * for this instance's form handler so a later native reset cannot submit
     * placeholder credentials even if it re-enables a widget. The manager's
     * token submitter is separate and remains unchanged. */
    if (login_member(get, data, "OnLoginSuccessful", &no_submit)) {
        login_property(set, data, "OnLoginClicked", &no_submit);
    }
    {
        char account[1024];
        login_value credentials[2] = {{0}, {0}};
        static const char* names[] = {"AccountInputMC", "PasswordInputMC", "btnLogin",
            "CreateAccountMC", "ForgotPasswordMC", "RememberAccountMC"};
        unsigned int index;
        login_account_text(account, sizeof(account));
        credentials[0].type = 4;
        credentials[0].data.string = account;
        credentials[1].type = 4;
        credentials[1].data.string = "launcher-auth";
        /* Preserve what OnLoginDialogDisplayed copies into rebuilt controls.
         * Unlike FillSavedLoginCredentials, this does not move keyboard focus. */
        login_property(set, data, "mAccountText", &credentials[0]);
        login_property(set, data, "mPasswordText", &credentials[1]);
        for (index = 0; index < sizeof(names) / sizeof(names[0]); index++) {
            login_value input = {0}, disabled = {0}, text_field = {0};
            if (login_member(get, &content, names[index], &input)) {
                disabled.type = 2;
                *(unsigned char*)&disabled.data = 1;
                login_property(set, &input, "disabled", &disabled);
                if (index < 2) {
                    if (index == 1) login_property(set, &input, "password", &disabled);
                    if (login_property(set, &input, "text", &credentials[index])) fields |= 1u << index;
                    invoke(&input, "validateNow", NULL, NULL, 0);
                    if (login_member(get, &input, "textField", &text_field)) {
                        if (index == 1) login_property(set, &text_field, "password", &disabled);
                        login_property(set, &text_field, "text", &credentials[index]);
                    }
                }
            }
            release(&text_field);
            release(&input);
        }
    }
    if (fields != previous_fields) {
        trace_client_state("jwt_login_display_fields", fields);
        previous_fields = fields;
    }
    if (is_started) {
        /* Retain the native processing state, but show the populated read-only
         * fields as launcher identity. Manual login/registration stay hidden. */
        is_started = invoke(data, "OnProcessingDisplayed", NULL, NULL, 0);
        if (is_started) {
            static const char* names[] = {"AccountTitleMC", "AccountInputMC", "PasswordTitleMC", "PasswordInputMC"};
            unsigned int index;
            login_value visible = {0};
            visible.type = 2;
            *(unsigned char*)&visible.data = 1;
            for (index = 0; index < sizeof(names) / sizeof(names[0]); index++) {
                login_value input = {0};
                if (login_member(get, &content, names[index], &input)) {
                    login_property(set, &input, "visible", &visible);
                }
                release(&input);
            }
        }
    }
done:
    release(&gdata);
    release(&no_submit);
    release(&current_frame);
    release(&content);
    release(&root);
    return is_started;
}

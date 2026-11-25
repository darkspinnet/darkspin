#include "fang.h"
#include "hook/text.h"
#include <shellapi.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <wchar.h>
#define FANG_TRANSLATION 1
#include "fang_generated.h"

// Environment strings are copied once; credentials are erased after installation.
static char* environment_text(const char* name) {
    DWORD capacity = GetEnvironmentVariableA(name, NULL, 0), length;
    char* text;
    if (!capacity) capacity = 1;
    text = (char*)calloc(capacity, 1);
    if (!text) return NULL;
    length = GetEnvironmentVariableA(name, text, capacity);
    if (length >= capacity) { free(text); return NULL; }
    return text;
}

__declspec(dllexport) DWORD WINAPI RecapInitializeThread(LPVOID parameter) {
    char *address, *trace, *jwt, *version, *intro, *cinematic, *separator, *end;
    char title[256];
    const char* host = "localhost";
    unsigned long port = 42127;
    int result;
    (void)parameter;
    fang_initialize_translation();
    address = environment_text("DARKSPIN_SERVER_ADDRESS");
    trace = environment_text("DARKSPIN_CLIENT_TRACE");
    jwt = environment_text("DARKSPIN_LAUNCH_JWT");
    version = environment_text("DARKSPINNER_VERSION");
    intro = environment_text("DARKSPIN_SKIP_INTRO");
    cinematic = environment_text("DARKSPIN_SKIP_CINEMATIC");
    SetEnvironmentVariableA("DARKSPIN_LAUNCH_JWT", NULL);
    if (!address || !trace || !jwt || !version || !intro || !cinematic) {
        result = -1;
        goto cleanup;
    }
    separator = strrchr(address, ':');
    if (separator && separator[1]) {
        unsigned long parsed = strtoul(separator+1, &end, 10);
        if (!*end && parsed > 0 && parsed < 65535) {
            *separator = 0;
            if (address[0] == '[' && separator > address+1 && separator[-1] == ']') {
                separator[-1] = 0;
                host = address+1;
                port = parsed;
            } else if (!strchr(address, ':')) { host = address; port = parsed; }
        }
    }
    snprintf(title, sizeof(title), "Dark Spin v%s", *version ? version : "0.0.0");
    result = fang_install(host, (unsigned short)port, (unsigned short)(port+1), trace,
        !strcmp(intro,"1"), !strcmp(cinematic,"1"), jwt, title);
cleanup:
    if (jwt) { SecureZeroMemory(jwt, strlen(jwt)); free(jwt); }
    free(address); free(trace); free(version); free(intro); free(cinematic);
    return (DWORD)result;
}

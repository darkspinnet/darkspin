#include "login/login.h"
#include "hook.h"

#include <stdio.h>
#include <string.h>

/* Observe only the existing build-103 Blaze receive path. TCP reads can split
 * the 12-byte header or coalesce multiple frames, including an error body.
 * REAS/MESG are Darkspin extensions, not native client error-text fields. */
static SRWLOCK login_failure_lock = SRWLOCK_INIT;
static SOCKET login_socket = INVALID_SOCKET;
static BYTE login_header[12];
static unsigned int login_header_length;
static unsigned int login_body_length;
static unsigned int login_body_received;
static BYTE login_body[2048];
static char login_reason[96];
static char login_message[512];

// Modifies: decodes a bounded TDF string into an output buffer and advances the caller's offset.
static int login_string(const BYTE* bytes, unsigned int size,
    unsigned int* offset, char* output, unsigned int capacity) {
    unsigned int length, shift = 6, count = 0;
    BYTE part;
    if (*offset >= size) return 0;
    part = bytes[(*offset)++];
    if (part & 0x40) return 0;
    length = part & 0x3f;
    while (part & 0x80) {
        if (*offset >= size || ++count > 3) return 0;
        part = bytes[(*offset)++];
        length |= (unsigned int)(part & 0x7f) << shift;
        shift += 7;
    }
    if (!length || length > capacity || length > size - *offset ||
        bytes[*offset + length - 1] != 0 ||
        memchr(bytes + *offset, 0, length - 1) != NULL) return 0;
    memcpy(output, bytes + *offset, length);
    *offset += length;
    return 1;
}

// Modifies: caches valid reason and message strings from the assembled login error body.
static void login_error_body(void) {
    unsigned int offset = 0;
    char reason[sizeof(login_reason)] = {0};
    char message[sizeof(login_message)] = {0};
    while (offset < login_body_length) {
        const BYTE* tag;
        char* text;
        unsigned int capacity;
        if (login_body_length - offset < 4) return;
        tag = login_body + offset;
        if (tag[3] != 1) return;
        if (tag[0] == 0xca && tag[1] == 0x58 && tag[2] == 0x73 && !reason[0]) {
            text = reason;
            capacity = sizeof(reason);
        } else if (tag[0] == 0xb6 && tag[1] == 0x5c && tag[2] == 0xe7 && !message[0]) {
            text = message;
            capacity = sizeof(message);
        } else {
            return;
        }
        offset += 4;
        if (!login_string(login_body, login_body_length, &offset, text, capacity)) return;
    }
    if (reason[0] && message[0]) {
        memcpy(login_reason, reason, sizeof(reason));
        memcpy(login_message, message, sizeof(message));
    }
}

// Modifies: assembles received Blaze frames and caches server login rejection details.
void fang_login_receive(SOCKET socket, const char* bytes, unsigned int size) {
    unsigned int offset = 0;
    AcquireSRWLockExclusive(&login_failure_lock);
    if (login_socket != socket) {
        login_socket = socket;
        login_header_length = 0;
        login_body_received = 0;
        login_body_length = 0;
        login_reason[0] = 0;
        login_message[0] = 0;
    }
    while (offset < size) {
        unsigned int count;
        if (login_header_length < sizeof(login_header)) {
            count = size - offset;
            if (count > sizeof(login_header) - login_header_length) {
                count = (unsigned int)sizeof(login_header) - login_header_length;
            }
            memcpy(login_header + login_header_length, bytes + offset, count);
            login_header_length += count;
            offset += count;
            if (login_header_length != sizeof(login_header)) continue;
            login_body_length = ((unsigned int)login_header[0] << 8) | login_header[1];
            login_body_received = 0;
        }
        count = size - offset;
        if (count > login_body_length - login_body_received) {
            count = login_body_length - login_body_received;
        }
        if (login_body_length <= sizeof(login_body)) {
            memcpy(login_body + login_body_received, bytes + offset, count);
        }
        login_body_received += count;
        offset += count;
        if (login_body_received != login_body_length) continue;
        if (login_body_length <= sizeof(login_body) &&
            login_header[2] == 0 && login_header[3] == 1 &&
            login_header[4] == 0 && login_header[5] == 0x28 &&
            (login_header[8] >> 4) == 3) {
            login_error_body();
        }
        login_header_length = 0;
    }
    ReleaseSRWLockExclusive(&login_failure_lock);
}

// Modifies: writes escaped login text to a bounded JSON output buffer.
static int login_json_text(const char* text, char* output, size_t capacity) {
    static const char hex[] = "0123456789abcdef";
    size_t used = 0;
    while (*text) {
        unsigned char character = (unsigned char)*text++;
        size_t count = character < 0x20 ? 6 : (character == '"' || character == '\\' ? 2 : 1);
        if (used + count >= capacity) return 0;
        if (character < 0x20) {
            output[used++] = '\\'; output[used++] = 'u';
            output[used++] = '0'; output[used++] = '0';
            output[used++] = hex[character >> 4]; output[used++] = hex[character & 15];
        } else {
            if (count == 2) output[used++] = '\\';
            output[used++] = (char)character;
        }
    }
    output[used] = 0;
    return 1;
}

// Modifies: atomically writes a login failure result file for the launcher.
static int login_failure_result(const char* reason, const char* message) {
    char path[2048], launch_id[64], temporary[2100], document[4608];
    char escaped_reason[576], escaped_message[3072];
    DWORD length, written;
    HANDLE output;
    int count, is_written;
    unsigned int index;
    length = GetEnvironmentVariableA("DARKSPIN_CLIENT_RESULT", path, sizeof(path));
    if (!length || length >= sizeof(path)) return 0;
    length = GetEnvironmentVariableA("DARKSPIN_CLIENT_LAUNCH_ID", launch_id, sizeof(launch_id));
    if (length != 32) return 0;
    for (index = 0; index < length; index++) {
        if (!((launch_id[index] >= '0' && launch_id[index] <= '9') ||
            (launch_id[index] >= 'a' && launch_id[index] <= 'f'))) return 0;
    }
    if (!login_json_text(reason, escaped_reason, sizeof(escaped_reason)) ||
        !login_json_text(message, escaped_message, sizeof(escaped_message))) return 0;
    count = snprintf(temporary, sizeof(temporary), "%s.%lu.tmp", path, (unsigned long)GetCurrentProcessId());
    if (count <= 0 || count >= (int)sizeof(temporary)) return 0;
    count = snprintf(document, sizeof(document),
        "{\"pid\":%lu,\"launch_id\":\"%s\",\"stage\":\"login\","
        "\"reason\":\"%s\",\"message\":\"%s\",\"time_ms\":%llu}\r\n",
        (unsigned long)GetCurrentProcessId(), launch_id, escaped_reason, escaped_message,
        (unsigned long long)GetTickCount64());
    if (count <= 0 || count >= (int)sizeof(document)) return 0;
    output = CreateFileA(temporary, GENERIC_WRITE, 0, NULL, CREATE_ALWAYS, FILE_ATTRIBUTE_NORMAL, NULL);
    if (output == INVALID_HANDLE_VALUE) return 0;
    is_written = WriteFile(output, document, (DWORD)count, &written, NULL) &&
        written == (DWORD)count && FlushFileBuffers(output);
    CloseHandle(output);
    if (!is_written || !MoveFileExA(temporary, path, MOVEFILE_REPLACE_EXISTING | MOVEFILE_WRITE_THROUGH)) {
        DeleteFileA(temporary);
        return 0;
    }
    return 1;
}

// Reads/modifies: reads cached rejection details and writes a launcher failure result with a fallback message.
int fang_login_rejected(unsigned int code) {
    char reason[sizeof(login_reason)], message[sizeof(login_message)];
    AcquireSRWLockShared(&login_failure_lock);
    memcpy(reason, login_reason, sizeof(reason));
    memcpy(message, login_message, sizeof(message));
    ReleaseSRWLockShared(&login_failure_lock);
    if (!message[0]) {
        snprintf(reason, sizeof(reason), "login-rejected");
        snprintf(message, sizeof(message),
            "The game could not sign in (error 0x%08X). Check the server connection and sign in again from Darkspinner.", code);
    }
    return login_failure_result(reason, message);
}

// Modifies: writes a launcher failure result for a login service timeout.
int fang_login_not_ready(void) {
    return login_failure_result("login-service-timeout",
        "The game could not reach the login service in time. Check the server connection and try again.");
}

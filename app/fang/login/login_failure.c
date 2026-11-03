#include "login/login.h"
#include "hook.h"

#include <stdio.h>
#include <string.h>

/* Observe only the existing build-103 Blaze receive path. TCP reads can split
 * the 12-byte header or coalesce multiple frames, including an error body.
 * REAS/MESG are Darkspin extensions, not native client error-text fields. */
static SRWLOCK login_failure_lock = SRWLOCK_INIT;
typedef struct login_failure_stream {
    SOCKET socket;
    int is_used;
    BYTE header[12];
    unsigned int header_length;
    unsigned int body_length;
    unsigned int body_received;
    BYTE body[2048];
} login_failure_stream;

static login_failure_stream login_streams[16];
static unsigned int login_stream_cursor;
static char login_reason[96];
static char login_message[512];

/* The caller holds login_failure_lock. Keep bounded per-socket framing:
 * another HTTP/Blaze connection must not discard a partial login reply. */
static login_failure_stream* login_stream_for_socket(SOCKET socket) {
    unsigned int index;
    login_failure_stream* stream;
    for (index = 0; index < 16; index++) {
        if (login_streams[index].is_used && login_streams[index].socket == socket)
            return &login_streams[index];
    }
    for (index = 0; index < 16; index++) {
        if (!login_streams[index].is_used) break;
    }
    if (index == 16) {
        index = login_stream_cursor;
        login_stream_cursor = (login_stream_cursor + 1) % 16;
    }
    stream = &login_streams[index];
    ZeroMemory(stream, sizeof(*stream));
    stream->socket = socket;
    stream->is_used = 1;
    return stream;
}

// Retire framing before Winsock can reuse this handle for another connection.
// Keep a completed rejection cached until native login-error dispatch reads it.
void fang_login_forget_socket(SOCKET socket) {
    unsigned int index;
    AcquireSRWLockExclusive(&login_failure_lock);
    for (index = 0; index < 16; index++) {
        if (login_streams[index].is_used && login_streams[index].socket == socket) {
            ZeroMemory(&login_streams[index], sizeof(login_streams[index]));
            break;
        }
    }
    ReleaseSRWLockExclusive(&login_failure_lock);
}

// The launcher failure result must already be flushed before this call.
// ExitProcess runs third-party DLL detach handlers after stopping other
// threads, which can crash or deadlock while a login request is pending.
void fang_login_terminate(unsigned int exit_code) {
    if (!TerminateProcess(GetCurrentProcess(), exit_code)) {
        DWORD error = GetLastError();
        trace_client_state("login_terminate_failed", error);
    }
}

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
static void login_error_body(const login_failure_stream* stream) {
    unsigned int offset = 0;
    char reason[sizeof(login_reason)] = {0};
    char message[sizeof(login_message)] = {0};
    while (offset < stream->body_length) {
        const BYTE* tag;
        char* text;
        unsigned int capacity;
        if (stream->body_length - offset < 4) return;
        tag = stream->body + offset;
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
        if (!login_string(stream->body, stream->body_length, &offset, text, capacity)) return;
    }
    if (reason[0] && message[0]) {
        memcpy(login_reason, reason, sizeof(reason));
        memcpy(login_message, message, sizeof(message));
    }
}

// Modifies: assembles received Blaze frames and caches server login rejection details.
void fang_login_receive(SOCKET socket, const char* bytes, unsigned int size) {
    unsigned int offset = 0;
    login_failure_stream* stream;
    AcquireSRWLockExclusive(&login_failure_lock);
    stream = login_stream_for_socket(socket);
    while (offset < size) {
        unsigned int count;
        if (stream->header_length < sizeof(stream->header)) {
            count = size - offset;
            if (count > sizeof(stream->header) - stream->header_length) {
                count = (unsigned int)sizeof(stream->header) - stream->header_length;
            }
            memcpy(stream->header + stream->header_length, bytes + offset, count);
            stream->header_length += count;
            offset += count;
            if (stream->header_length != sizeof(stream->header)) continue;
            stream->body_length = ((unsigned int)stream->header[0] << 8) | stream->header[1];
            stream->body_received = 0;
        }
        count = size - offset;
        if (count > stream->body_length - stream->body_received) {
            count = stream->body_length - stream->body_received;
        }
        if (stream->body_length <= sizeof(stream->body)) {
            memcpy(stream->body + stream->body_received, bytes + offset, count);
        }
        stream->body_received += count;
        offset += count;
        if (stream->body_received != stream->body_length) continue;
        if (stream->body_length <= sizeof(stream->body) &&
            stream->header[2] == 0 && stream->header[3] == 1 &&
            stream->header[4] == 0 && stream->header[5] == 0x28 &&
            (stream->header[8] >> 4) == 3) {
            login_reason[0] = 0;
            login_message[0] = 0;
            login_error_body(stream);
        }
        stream->header_length = 0;
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

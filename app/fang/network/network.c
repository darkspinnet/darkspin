#include "network/network.h"
#include "hook/import.h"
#include "hook/trace.h"
#include "bug/record.h"
#include "login/login.h"
#include "mod/mod.h"

#include <limits.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

typedef struct hostent* (WSAAPI* gethostbyname_fn)(const char* name);
typedef int (WSAAPI* connect_fn)(SOCKET socket, const struct sockaddr* name, int name_length);
typedef int (WSAAPI* send_fn)(SOCKET socket, const char* buffer, int length, int flags);
typedef int (WSAAPI* recv_fn)(SOCKET socket, char* buffer, int length, int flags);
typedef int (WSAAPI* sendto_fn)(SOCKET socket, const char* buffer, int length, int flags, const struct sockaddr* destination, int destination_length);
typedef int (WSAAPI* recvfrom_fn)(SOCKET socket, char* buffer, int length, int flags, struct sockaddr* source, int* source_length);
typedef int (WSAAPI* closesocket_fn)(SOCKET socket);
typedef int (WSAAPI* wsasend_fn)(SOCKET socket, LPWSABUF buffers, DWORD count, LPDWORD sent, DWORD flags, LPWSAOVERLAPPED overlapped, LPWSAOVERLAPPED_COMPLETION_ROUTINE completion);
typedef int (WSAAPI* wsarecv_fn)(SOCKET socket, LPWSABUF buffers, DWORD count, LPDWORD received, LPDWORD flags, LPWSAOVERLAPPED overlapped, LPWSAOVERLAPPED_COMPLETION_ROUTINE completion);
typedef int (WSAAPI* wsarecvfrom_fn)(SOCKET socket, LPWSABUF buffers, DWORD count, LPDWORD received, LPDWORD flags, struct sockaddr* source, LPINT source_length, LPWSAOVERLAPPED overlapped, LPWSAOVERLAPPED_COMPLETION_ROUTINE completion);
typedef BOOL (WSAAPI* wsagetoverlappedresult_fn)(SOCKET socket, LPWSAOVERLAPPED overlapped, LPDWORD transferred, BOOL wait, LPDWORD flags);
typedef struct redirect_tls {
    struct hostent host;
    struct in_addr address;
    char* addresses[2];
} redirect_tls;

static gethostbyname_fn original_gethostbyname;
static connect_fn original_connect;
static send_fn original_send;
static recv_fn original_recv;
static sendto_fn original_sendto;
static recvfrom_fn original_recvfrom;
static closesocket_fn original_closesocket;
static wsasend_fn original_wsasend;
static wsarecv_fn original_wsarecv;
static wsarecvfrom_fn original_wsarecvfrom;
static wsagetoverlappedresult_fn original_wsagetoverlappedresult;
static DWORD redirect_tls_index = TLS_OUT_OF_INDEXES;
static char redirect_hostname[256] = "localhost";
static unsigned short redirect_port = 42127;
static unsigned short redirect_party_port = 42128;
static const unsigned char party_envelope[] = {'D', 'S', 'P', 'G', 1, 1, 0, 0};
#define PARTY_SOCKET_LIMIT 64
static SRWLOCK party_socket_lock = SRWLOCK_INIT;
static SOCKET party_sockets[PARTY_SOCKET_LIMIT];
static unsigned int party_socket_count;
typedef struct party_receive {
    SOCKET socket;
    LPWSAOVERLAPPED overlapped;
    struct sockaddr* source;
    LPINT source_length;
} party_receive;
static party_receive party_receives[PARTY_SOCKET_LIMIT];
static unsigned int party_receive_count;
static unsigned int dns_diagnostics;
static int is_party_socket(SOCKET socket);

// Modifies: writes an IPv4 address and port, or empty defaults, to output parameters.
static void trace_endpoint(const struct sockaddr* address, char* host, size_t host_length, unsigned short* port) {
    const struct sockaddr_in* ipv4;
    uint32_t value;
    if (host_length > 0) {
        host[0] = '\0';
    }
    *port = 0;
    if (address == NULL || address->sa_family != AF_INET) {
        return;
    }
    ipv4 = (const struct sockaddr_in*)address;
    value = ntohl(ipv4->sin_addr.s_addr);
    snprintf(host, host_length, "%u.%u.%u.%u",
        (unsigned int)((value >> 24) & 0xff),
        (unsigned int)((value >> 16) & 0xff),
        (unsigned int)((value >> 8) & 0xff),
        (unsigned int)(value & 0xff));
    *port = ntohs(ipv4->sin_port);
}

// Modifies: formats and writes a socket diagnostic record to the active trace.
static void trace_socket_event(const char* kind, SOCKET socket, const struct sockaddr* address, uintptr_t caller_rva, int requested, int result, int error, uint64_t digest) {
    char line[768];
    char host[64];
    unsigned short port;
    DWORD written;
    int length;
    if (!fang_record_trace_available()) {
        return;
    }
    trace_endpoint(address, host, sizeof(host), &port);
    length = snprintf(line, sizeof(line),
        "{\"time_ms\":%llu,\"protocol\":\"socket\",\"kind\":\"%s\",\"thread\":%lu,\"socket\":%llu,\"caller_rva\":\"0x%llx\",\"remote\":\"%s\",\"port\":%u,\"requested\":%d,\"result\":%d,\"error\":%d,\"digest\":\"%016llx\"}\r\n",
        (unsigned long long)GetTickCount(), kind, (unsigned long)GetCurrentThreadId(),
        (unsigned long long)(uintptr_t)socket, (unsigned long long)caller_rva,
        host, (unsigned int)port, requested, result, error, (unsigned long long)digest);
    if (length <= 0) {
        return;
    }
    if ((size_t)length > sizeof(line)) {
        length = (int)sizeof(line);
    }
    fang_record_trace_write(line, (DWORD)length, &written);
}

// Reads: queries whether a socket uses datagrams.
static int is_datagram_socket(SOCKET socket) {
    int socket_type = 0;
    int length = sizeof(socket_type);
    if (getsockopt(socket, SOL_SOCKET, SO_TYPE, (char*)&socket_type, &length) != 0) {
        return 0;
    }
    return socket_type == SOCK_DGRAM;
}

// Reads/modifies: filters captured gameplay datagrams and records their payloads in the trace and snapshot ring.
static void trace_raknet_payload(const char* direction, const char* kind, SOCKET socket,
    const struct sockaddr* address, const char* buffer, int length) {
    char header[640];
    char host[64];
    const char suffix[] = "\"}\r\n";
    unsigned short port;
    int header_length;
    int is_party;
    if (fang_network_capture_is_enabled() == 0 ||
        buffer == NULL || length <= 0 || length > 65535 ||
        !is_datagram_socket(socket)) {
        return;
    }
    trace_endpoint(address, host, sizeof(host), &port);
    is_party = is_party_socket(socket) || port == redirect_party_port;
    if (port != redirect_port && port != redirect_party_port && !is_party) {
        return;
    }
    header_length = snprintf(header, sizeof(header),
        "{\"time_ms\":%llu,\"protocol\":\"client_raknet\","
        "\"kind\":\"datagram\",\"direction\":\"%s\",\"call\":\"%s\","
        "\"thread\":%lu,\"frame_sequence\":%lu,\"socket\":%llu,"
        "\"remote\":\"%s\",\"port\":%u,"
        "\"is_party\":%s,\"size\":%d,\"payload_hex\":\"",
        (unsigned long long)GetTickCount(), direction, kind,
        (unsigned long)GetCurrentThreadId(),
        (unsigned long)fang_record_frame_sequence(),
        (unsigned long long)(uintptr_t)socket, host, (unsigned int)port,
        is_party ? "true" : "false", length);
    if (header_length <= 0) {
        return;
    }
    trace_hex_line(
        header, header_length, (const BYTE*)buffer,
        (unsigned int)length, suffix);
}

// Modifies: writes a socket's peer address and length, clearing the length on failure.
static void peer_address(SOCKET socket, struct sockaddr_storage* storage, int* length) {
    *length = sizeof(*storage);
    memset(storage, 0, sizeof(*storage));
    if (getpeername(socket, (struct sockaddr*)storage, length) != 0) {
        *length = 0;
    }
}

// Modifies: allocates and stores per-thread DNS redirect storage when needed.
static redirect_tls* get_redirect_tls(void) {
    redirect_tls* data;
    if (redirect_tls_index == TLS_OUT_OF_INDEXES) {
        return NULL;
    }
    data = (redirect_tls*)TlsGetValue(redirect_tls_index);
    if (data != NULL) {
        return data;
    }
    data = (redirect_tls*)HeapAlloc(GetProcessHeap(), HEAP_ZERO_MEMORY, sizeof(redirect_tls));
    if (data == NULL) {
        return NULL;
    }
    if (!TlsSetValue(redirect_tls_index, data)) {
        HeapFree(GetProcessHeap(), 0, data);
        return NULL;
    }
    return data;
}

// Modifies: resolves the configured redirect host and populates a per-thread IPv4 host result.
static struct hostent* WSAAPI hooked_gethostbyname(const char* name) {
    struct hostent* resolved;
    redirect_tls* data;
    if (name == NULL) {
        return original_gethostbyname(name);
    }
    resolved = original_gethostbyname(redirect_hostname);
    if (resolved == NULL || resolved->h_addr_list == NULL || resolved->h_addr_list[0] == NULL) {
        return NULL;
    }
    data = get_redirect_tls();
    if (data == NULL) {
        return NULL;
    }
    memcpy(&data->address, resolved->h_addr_list[0], sizeof(data->address));
    data->addresses[0] = (char*)&data->address;
    data->addresses[1] = NULL;
    data->host.h_name = NULL;
    data->host.h_aliases = NULL;
    data->host.h_addrtype = AF_INET;
    data->host.h_length = sizeof(data->address);
    data->host.h_addr_list = data->addresses;
    return &data->host;
}

// Reads: checks an address's IPv4 family, length, and network-order port.
static int is_ipv4_port(const struct sockaddr* address, int length, unsigned short port) {
    const struct sockaddr_in* ipv4;
    if (address == NULL || length < (int)sizeof(struct sockaddr_in) ||
        address->sa_family != AF_INET) {
        return 0;
    }
    ipv4 = (const struct sockaddr_in*)address;
    return ipv4->sin_port == htons(port);
}

// Modifies: writes the configured redirect host's resolved IPv4 address to the caller's output.
static int redirect_ipv4(struct in_addr* address) {
    struct hostent* resolved;
    if (address == NULL) {
        return 0;
    }
    resolved = original_gethostbyname(redirect_hostname);
    if (resolved == NULL || resolved->h_addr_list == NULL || resolved->h_addr_list[0] == NULL) {
        return 0;
    }
    memcpy(address, resolved->h_addr_list[0], sizeof(*address));
    return 1;
}

// Modifies: adds a socket to the bounded party socket registry.
static void remember_party_socket(SOCKET socket) {
    unsigned int index;
    AcquireSRWLockExclusive(&party_socket_lock);
    for (index = 0; index < party_socket_count; index++) {
        if (party_sockets[index] == socket) {
            ReleaseSRWLockExclusive(&party_socket_lock);
            return;
        }
    }
    if (party_socket_count < PARTY_SOCKET_LIMIT) {
        party_sockets[party_socket_count] = socket;
        party_socket_count++;
    }
    ReleaseSRWLockExclusive(&party_socket_lock);
}

// Reads: checks whether a socket is registered for party traffic.
static int is_party_socket(SOCKET socket) {
    unsigned int index;
    int is_party = 0;
    AcquireSRWLockShared(&party_socket_lock);
    for (index = 0; index < party_socket_count; index++) {
        if (party_sockets[index] == socket) {
            is_party = 1;
            break;
        }
    }
    ReleaseSRWLockShared(&party_socket_lock);
    return is_party;
}

static void restore_party_source(SOCKET socket, struct sockaddr* source, int source_length);

// Modifies: removes a socket and its pending receive records from party bookkeeping.
static void forget_party_socket(SOCKET socket) {
    unsigned int index;
    AcquireSRWLockExclusive(&party_socket_lock);
    for (index = 0; index < party_socket_count; index++) {
        if (party_sockets[index] != socket) {
            continue;
        }
        party_socket_count--;
        party_sockets[index] = party_sockets[party_socket_count];
        break;
    }
    index = 0;
    while (index < party_receive_count) {
        if (party_receives[index].socket != socket) {
            index++;
            continue;
        }
        party_receive_count--;
        party_receives[index] = party_receives[party_receive_count];
    }
    ReleaseSRWLockExclusive(&party_socket_lock);
}

// Modifies: stores source output pointers for a pending overlapped party receive.
static void remember_party_receive(SOCKET socket, LPWSAOVERLAPPED overlapped,
    struct sockaddr* source, LPINT source_length) {
    unsigned int index;
    if (overlapped == NULL || source == NULL || source_length == NULL) {
        return;
    }
    AcquireSRWLockExclusive(&party_socket_lock);
    for (index = 0; index < party_receive_count; index++) {
        if (party_receives[index].socket == socket &&
            party_receives[index].overlapped == overlapped) {
            party_receives[index].source = source;
            party_receives[index].source_length = source_length;
            ReleaseSRWLockExclusive(&party_socket_lock);
            return;
        }
    }
    if (party_receive_count < PARTY_SOCKET_LIMIT) {
        party_receives[party_receive_count].socket = socket;
        party_receives[party_receive_count].overlapped = overlapped;
        party_receives[party_receive_count].source = source;
        party_receives[party_receive_count].source_length = source_length;
        party_receive_count++;
    }
    ReleaseSRWLockExclusive(&party_socket_lock);
}

// Modifies: removes a completed receive record and restores its reported party source port.
static void complete_party_receive(SOCKET socket, LPWSAOVERLAPPED overlapped) {
    unsigned int index;
    struct sockaddr* source = NULL;
    LPINT source_length = NULL;
    AcquireSRWLockExclusive(&party_socket_lock);
    for (index = 0; index < party_receive_count; index++) {
        if (party_receives[index].socket != socket ||
            party_receives[index].overlapped != overlapped) {
            continue;
        }
        source = party_receives[index].source;
        source_length = party_receives[index].source_length;
        party_receive_count--;
        party_receives[index] = party_receives[party_receive_count];
        break;
    }
    ReleaseSRWLockExclusive(&party_socket_lock);
    if (source != NULL && source_length != NULL) {
        restore_party_source(socket, source, *source_length);
    }
}

// Modifies: allocates a party-prefixed datagram, writes its length, and sets Winsock errors on failure.
static char* envelop_party_datagram(const char* buffer, int length, int* wrapped_length) {
    char* wrapped;
    size_t envelope_length = sizeof(party_envelope);
    if (buffer == NULL || length < 0 || length > INT_MAX - (int)envelope_length) {
        WSASetLastError(WSAEINVAL);
        return NULL;
    }
    *wrapped_length = length + (int)envelope_length;
    wrapped = (char*)HeapAlloc(GetProcessHeap(), 0, (SIZE_T)*wrapped_length);
    if (wrapped == NULL) {
        WSASetLastError(WSAENOBUFS);
        return NULL;
    }
    memcpy(wrapped, party_envelope, envelope_length);
    memcpy(wrapped + envelope_length, buffer, (size_t)length);
    return wrapped;
}

// Reads: converts an enveloped send count back to the original payload count.
static int party_send_result(int result, int original_length) {
    if (result == SOCKET_ERROR) {
        return SOCKET_ERROR;
    }
    if (result == original_length + (int)sizeof(party_envelope)) {
        return original_length;
    }
    if (result <= (int)sizeof(party_envelope)) {
        return 0;
    }
    return result - (int)sizeof(party_envelope);
}

// Modifies: rewrites a registered party receive's source port to the client-visible party port.
static void restore_party_source(SOCKET socket, struct sockaddr* source, int source_length) {
    struct sockaddr_in* ipv4;
    if (!is_party_socket(socket) ||
        !is_ipv4_port(source, source_length, redirect_port)) {
        return;
    }
    ipv4 = (struct sockaddr_in*)source;
    ipv4->sin_port = htons(redirect_party_port);
}

// Modifies: connects through redirected endpoints, tracks party sockets, and logs the connection result.
static int WSAAPI hooked_connect(SOCKET socket, const struct sockaddr* name, int name_length) {
    struct sockaddr_in redirected;
    const struct sockaddr* effective = name;
    int result;
    int error;
    int is_party_destination = 0;
    if (name != NULL && name_length >= (int)sizeof(struct sockaddr_in) && name->sa_family == AF_INET) {
        memcpy(&redirected, name, sizeof(redirected));
        if (fang_redirect_mod_address(&redirected, redirect_port, redirect_party_port)) {
            effective = (const struct sockaddr*)&redirected;
        }
        if (redirected.sin_port == htons(80)) {
            redirected.sin_port = htons(redirect_port);
            redirect_ipv4(&redirected.sin_addr);
            effective = (const struct sockaddr*)&redirected;
        } else if (redirected.sin_port == htons(redirect_party_port)) {
            is_party_destination = 1;
            redirected.sin_port = htons(redirect_port);
            redirect_ipv4(&redirected.sin_addr);
            effective = (const struct sockaddr*)&redirected;
        } else if (redirected.sin_port == htons(redirect_port)) {
            redirect_ipv4(&redirected.sin_addr);
            effective = (const struct sockaddr*)&redirected;
        }
    }
    result = original_connect(socket, effective, name_length);
    error = result == SOCKET_ERROR ? WSAGetLastError() : 0;
    if (result != SOCKET_ERROR && is_party_destination) {
        remember_party_socket(socket);
    }
    trace_socket_event("connect", socket, effective, fang_trace_caller_rva(__builtin_return_address(0)), name_length, result, error, 0);
    WSASetLastError(error);
    return result;
}

// Modifies: replaces matching encoded matchmaking string bytes and returns the replacement count.
static int patch_matchmaking_string(BYTE* payload, int length,
    const char* key, char expected, char replacement) {
    size_t key_length = strlen(key);
    int patch_count = 0;
    int index;
    if (payload == NULL || key_length >= 63 || length <= 0) {
        return 0;
    }
    for (index = 0; index + (int)key_length + 5 <= length; index++) {
        BYTE* entry = payload + index;
        if (entry[0] != (BYTE)(key_length + 1) ||
            memcmp(entry + 1, key, key_length) != 0 ||
            entry[key_length + 1] != 0 ||
            entry[key_length + 2] != 2 ||
            entry[key_length + 3] != (BYTE)expected ||
            entry[key_length + 4] != 0) {
            continue;
        }
        entry[key_length + 3] = (BYTE)replacement;
        patch_count++;
    }
    return patch_count;
}

// Modifies: changes pending matchmaking frames to two players and one-player teams and logs replacements.
static int patch_solo_unranked_1v1_matchmaking(BYTE* buffer, int length) {
    int offset = 0;
    int patch_count = 0;
    if (buffer == NULL || length < 12 ||
        fang_network_matchmaking_pending() == 0) {
        return 0;
    }
    while (offset + 12 <= length) {
        BYTE* frame = buffer + offset;
        int payload_length = (int)frame[0] << 8 | (int)frame[1];
        int frame_length = 12 + payload_length;
        int expected_count;
        int team_count;
        if (frame_length > length - offset) {
            break;
        }
        if (frame[2] != 0 || frame[3] != 4 ||
            frame[4] != 0 || frame[5] != 13 || (frame[8] >> 4) != 0) {
            offset += frame_length;
            continue;
        }
        expected_count = patch_matchmaking_string(
            frame + 12, payload_length, "ExpectedPlayerCount", '4', '4');
        team_count = patch_matchmaking_string(
            frame + 12, payload_length, "TeamSize", '2', '2');
        if (expected_count == 0 || team_count == 0) {
            offset += frame_length;
            continue;
        }
        patch_count += patch_matchmaking_string(
            frame + 12, payload_length, "ExpectedPlayerCount", '4', '2');
        patch_count += patch_matchmaking_string(
            frame + 12, payload_length, "TeamSize", '2', '1');
        offset += frame_length;
    }
    if (patch_count != 0) {
        trace_client_state("solo_unranked_1v1_matchmaking", (unsigned int)patch_count);
    }
    return patch_count;
}

// Modifies: sends redirected party envelopes or adjusted matchmaking data and records socket and payload diagnostics.
static int WSAAPI hooked_send(SOCKET socket, const char* buffer, int length, int flags) {
    struct sockaddr_storage remote;
    int remote_length;
    int wrapped_length = 0;
    char* wrapped = NULL;
    char* matchmaking = NULL;
    const char* effective = buffer;
    int result;
    if (!is_party_socket(socket) && buffer != NULL && length > 0 &&
        fang_network_matchmaking_pending() != 0) {
        matchmaking = (char*)HeapAlloc(GetProcessHeap(), 0, (SIZE_T)length);
        if (matchmaking != NULL) {
            memcpy(matchmaking, buffer, (size_t)length);
            if (patch_solo_unranked_1v1_matchmaking((BYTE*)matchmaking, length) != 0) {
                effective = matchmaking;
            }
        }
    }
    if (is_party_socket(socket)) {
        wrapped = envelop_party_datagram(buffer, length, &wrapped_length);
        if (wrapped == NULL) {
            return SOCKET_ERROR;
        }
        result = original_send(socket, wrapped, wrapped_length, flags);
    } else {
        result = original_send(socket, effective, length, flags);
    }
    int error = result == SOCKET_ERROR ? WSAGetLastError() : 0;
    if (matchmaking != NULL) {
        HeapFree(GetProcessHeap(), 0, matchmaking);
    }
    if (wrapped != NULL) {
        HeapFree(GetProcessHeap(), 0, wrapped);
        result = party_send_result(result, length);
    }
    peer_address(socket, &remote, &remote_length);
    trace_socket_event("send", socket, remote_length > 0 ? (struct sockaddr*)&remote : NULL, fang_trace_caller_rva(__builtin_return_address(0)), length, result, error, fang_trace_digest(buffer, result));
    trace_raknet_payload("client_to_server", "send", socket,
        remote_length > 0 ? (struct sockaddr*)&remote : NULL, buffer, result);
    WSASetLastError(error);
    return result;
}

// Modifies: receives into the caller's buffer, assembles login diagnostics, and records socket and payload traces.
static int WSAAPI hooked_recv(SOCKET socket, char* buffer, int length, int flags) {
    struct sockaddr_storage remote;
    int remote_length;
    int result = original_recv(socket, buffer, length, flags);
    int error = result == SOCKET_ERROR ? WSAGetLastError() : 0;
    /* Build 103's cSocketChannel receive wrapper returns at RVA 0xa4e0d2.
     * Observe complete TCP bytes before native login error dispatch; never
     * consume peeked data or change the bytes delivered to the client. */
    if (result > 0 && (flags & MSG_PEEK) == 0 && fang_network_login_is_pending() &&
        fang_trace_caller_rva(__builtin_return_address(0)) == 0xa4e0d2) {
        fang_login_receive(socket, buffer, (unsigned int)result);
    }
    peer_address(socket, &remote, &remote_length);
    if (result != 1 && error != WSAEWOULDBLOCK) {
        trace_socket_event("recv", socket, remote_length > 0 ? (struct sockaddr*)&remote : NULL, fang_trace_caller_rva(__builtin_return_address(0)), length, result, error, fang_trace_digest(buffer, result));
        trace_raknet_payload("server_to_client", "recv", socket,
            remote_length > 0 ? (struct sockaddr*)&remote : NULL, buffer, result);
    }
    WSASetLastError(error);
    return result;
}

// Modifies: redirects and envelopes party datagrams, tracks sockets, sends data, and records diagnostics.
static int WSAAPI hooked_sendto(SOCKET socket, const char* buffer, int length, int flags, const struct sockaddr* destination, int destination_length) {
    struct sockaddr_in redirected;
    struct sockaddr_in mod_destination;
    const struct sockaddr* effective = destination;
    int wrapped_length = 0;
    char* wrapped = NULL;
    int result;
    if (destination != NULL && destination_length >= (int)sizeof(mod_destination) && destination->sa_family == AF_INET) {
        memcpy(&mod_destination, destination, sizeof(mod_destination));
        if (fang_redirect_mod_address(&mod_destination, redirect_port, redirect_party_port)) {
            destination = (const struct sockaddr*)&mod_destination;
            effective = destination;
        }
    }
    if (is_ipv4_port(destination, destination_length, redirect_party_port)) {
        remember_party_socket(socket);
        memcpy(&redirected, destination, sizeof(redirected));
        redirected.sin_port = htons(redirect_port);
        redirect_ipv4(&redirected.sin_addr);
        effective = (const struct sockaddr*)&redirected;
        wrapped = envelop_party_datagram(buffer, length, &wrapped_length);
        if (wrapped == NULL) {
            return SOCKET_ERROR;
        }
        result = original_sendto(socket, wrapped, wrapped_length, flags, effective, destination_length);
    } else if (is_ipv4_port(destination, destination_length, redirect_port)) {
        memcpy(&redirected, destination, sizeof(redirected));
        redirect_ipv4(&redirected.sin_addr);
        effective = (const struct sockaddr*)&redirected;
        result = original_sendto(socket, buffer, length, flags, effective, destination_length);
    } else {
        result = original_sendto(socket, buffer, length, flags, destination, destination_length);
    }
    int error = result == SOCKET_ERROR ? WSAGetLastError() : 0;
    if (wrapped != NULL) {
        HeapFree(GetProcessHeap(), 0, wrapped);
        result = party_send_result(result, length);
    }
    trace_socket_event("sendto", socket, effective, fang_trace_caller_rva(__builtin_return_address(0)), length, result, error, fang_trace_digest(buffer, result));
    trace_raknet_payload(
        "client_to_server", "sendto", socket, effective, buffer, result);
    WSASetLastError(error);
    return result;
}

// Modifies: receives datagrams into outputs, restores party source ports, and records diagnostics.
static int WSAAPI hooked_recvfrom(SOCKET socket, char* buffer, int length, int flags, struct sockaddr* source, int* source_length) {
    int result = original_recvfrom(socket, buffer, length, flags, source, source_length);
    int error = result == SOCKET_ERROR ? WSAGetLastError() : 0;
    if (result != SOCKET_ERROR && source_length != NULL) {
        restore_party_source(socket, source, *source_length);
    }
    trace_socket_event("recvfrom", socket, source, fang_trace_caller_rva(__builtin_return_address(0)), length, result, error, fang_trace_digest(buffer, result));
    trace_raknet_payload(
        "server_to_client", "recvfrom", socket, source, buffer, result);
    WSASetLastError(error);
    return result;
}

// Modifies: removes party bookkeeping and closes the native socket.
static int WSAAPI hooked_closesocket(SOCKET socket) {
    int result;
    forget_party_socket(socket);
    result = original_closesocket(socket);
    return result;
}

// Reads: totals the lengths of Winsock buffers.
static DWORD wsabuf_length(LPWSABUF buffers, DWORD count) {
    DWORD total = 0;
    DWORD index;
    for (index = 0; index < count; index++) {
        total += buffers[index].len;
    }
    return total;
}

// Modifies: gathers completed Winsock buffer bytes and records the captured datagram payload.
static void trace_wsabuf_payload(const char* direction, const char* kind,
    SOCKET socket, const struct sockaddr* address, LPWSABUF buffers,
    DWORD count, DWORD completed) {
    BYTE* payload;
    DWORD copied = 0;
    DWORD index;
    if (buffers == NULL || count == 0 || completed == 0 || completed > 65535) {
        return;
    }
    payload = (BYTE*)HeapAlloc(GetProcessHeap(), 0, (SIZE_T)completed);
    if (payload == NULL) {
        return;
    }
    for (index = 0; index < count && copied < completed; index++) {
        DWORD remaining = completed - copied;
        DWORD copy_length = buffers[index].len < remaining ? buffers[index].len : remaining;
        if (copy_length > 0 && buffers[index].buf != NULL) {
            memcpy(payload + copied, buffers[index].buf, copy_length);
            copied += copy_length;
        }
    }
    if (copied == completed) {
        trace_raknet_payload(
            direction, kind, socket, address, (const char*)payload, (int)completed);
    }
    HeapFree(GetProcessHeap(), 0, payload);
}

// Modifies: adjusts pending matchmaking buffers, invokes native sending, and records diagnostics and completion output.
static int WSAAPI hooked_wsasend(SOCKET socket, LPWSABUF buffers, DWORD count, LPDWORD sent, DWORD flags, LPWSAOVERLAPPED overlapped, LPWSAOVERLAPPED_COMPLETION_ROUTINE completion) {
    struct sockaddr_storage remote;
    int remote_length;
    DWORD total = wsabuf_length(buffers, count);
    BYTE* matchmaking = NULL;
    DWORD offset = 0;
    DWORD index;
    if (!is_party_socket(socket) && buffers != NULL && total > 0 &&
        fang_network_matchmaking_pending() != 0) {
        matchmaking = (BYTE*)HeapAlloc(GetProcessHeap(), 0, (SIZE_T)total);
        if (matchmaking != NULL) {
            for (index = 0; index < count; index++) {
                memcpy(matchmaking + offset, buffers[index].buf, buffers[index].len);
                offset += buffers[index].len;
            }
            if (patch_solo_unranked_1v1_matchmaking(matchmaking, (int)total) != 0) {
                offset = 0;
                for (index = 0; index < count; index++) {
                    memcpy(buffers[index].buf, matchmaking + offset, buffers[index].len);
                    offset += buffers[index].len;
                }
            }
            HeapFree(GetProcessHeap(), 0, matchmaking);
        }
    }
    int result = original_wsasend(socket, buffers, count, sent, flags, overlapped, completion);
    int error = result == SOCKET_ERROR ? WSAGetLastError() : 0;
    int completed = result == 0 && sent != NULL ? (int)*sent : result;
    peer_address(socket, &remote, &remote_length);
    trace_socket_event("WSASend", socket, remote_length > 0 ? (struct sockaddr*)&remote : NULL, fang_trace_caller_rva(__builtin_return_address(0)), (int)total, completed, error, 0);
    if (completed > 0) {
        trace_wsabuf_payload(
            "client_to_server", "WSASend", socket,
            remote_length > 0 ? (struct sockaddr*)&remote : NULL,
            buffers, count, (DWORD)completed);
    }
    WSASetLastError(error);
    return result;
}

// Modifies: invokes native receiving into caller outputs and records socket and completed payload diagnostics.
static int WSAAPI hooked_wsarecv(SOCKET socket, LPWSABUF buffers, DWORD count, LPDWORD received, LPDWORD flags, LPWSAOVERLAPPED overlapped, LPWSAOVERLAPPED_COMPLETION_ROUTINE completion) {
    struct sockaddr_storage remote;
    int remote_length;
    int result = original_wsarecv(socket, buffers, count, received, flags, overlapped, completion);
    int error = result == SOCKET_ERROR ? WSAGetLastError() : 0;
    int completed = result == 0 && received != NULL ? (int)*received : result;
    peer_address(socket, &remote, &remote_length);
    trace_socket_event("WSARecv", socket, remote_length > 0 ? (struct sockaddr*)&remote : NULL, fang_trace_caller_rva(__builtin_return_address(0)), (int)wsabuf_length(buffers, count), completed, error, 0);
    if (completed > 0) {
        trace_wsabuf_payload(
            "server_to_client", "WSARecv", socket,
            remote_length > 0 ? (struct sockaddr*)&remote : NULL,
            buffers, count, (DWORD)completed);
    }
    WSASetLastError(error);
    return result;
}

// Modifies: receives into caller outputs, restores or tracks party sources, and records diagnostics.
static int WSAAPI hooked_wsarecvfrom(SOCKET socket, LPWSABUF buffers, DWORD count, LPDWORD received, LPDWORD flags, struct sockaddr* source, LPINT source_length, LPWSAOVERLAPPED overlapped, LPWSAOVERLAPPED_COMPLETION_ROUTINE completion) {
    int result = original_wsarecvfrom(socket, buffers, count, received, flags, source, source_length, overlapped, completion);
    int error = result == SOCKET_ERROR ? WSAGetLastError() : 0;
    int completed = result == 0 && received != NULL ? (int)*received : result;
    if (result == 0 && source_length != NULL) {
        restore_party_source(socket, source, *source_length);
    } else if (result == SOCKET_ERROR && error == WSA_IO_PENDING) {
        remember_party_receive(socket, overlapped, source, source_length);
    }
    trace_socket_event("WSARecvFrom", socket, source, fang_trace_caller_rva(__builtin_return_address(0)), (int)wsabuf_length(buffers, count), completed, error, 0);
    if (completed > 0) {
        trace_wsabuf_payload(
            "server_to_client", "WSARecvFrom", socket, source,
            buffers, count, (DWORD)completed);
    }
    WSASetLastError(error);
    return result;
}

// Modifies: writes native completion outputs and restores completed party receive source ports.
static BOOL WSAAPI hooked_wsagetoverlappedresult(SOCKET socket,
    LPWSAOVERLAPPED overlapped, LPDWORD transferred, BOOL wait, LPDWORD flags) {
    BOOL result = original_wsagetoverlappedresult(socket, overlapped, transferred, wait, flags);
    int error = result ? 0 : WSAGetLastError();
    if (result) {
        complete_party_receive(socket, overlapped);
    }
    WSASetLastError(error);
    return result;
}

// Modifies: clears accumulated DNS import patch diagnostics.
void fang_network_reset_diagnostics(void) {
    dns_diagnostics = 0;
}

// Reads: checks that the native control-channel socket operations are bound.
int fang_network_control_is_ready(void) {
    return original_connect != NULL && original_send != NULL &&
        original_recv != NULL && original_closesocket != NULL;
}

// Modifies: invokes the unhooked native socket connection operation.
int fang_network_control_connect(SOCKET socket, const struct sockaddr* address, int length) {
    return original_connect(socket, address, length);
}

// Modifies: sends control-channel bytes through the unhooked native operation.
int fang_network_control_send(SOCKET socket, const char* buffer, int length, int flags) {
    return original_send(socket, buffer, length, flags);
}

// Modifies: receives control-channel bytes into the caller's buffer through the unhooked native operation.
int fang_network_control_recv(SOCKET socket, char* buffer, int length, int flags) {
    return original_recv(socket, buffer, length, flags);
}

// Modifies: closes a control-channel socket through the unhooked native operation.
int fang_network_control_close(SOCKET socket) {
    return original_closesocket(socket);
}

// Modifies: stores redirect endpoints and binds native Winsock operations.
int fang_network_configure(const char* hostname, unsigned short port,
    unsigned short party_port, HMODULE winsock) {
    strncpy(redirect_hostname, hostname, sizeof(redirect_hostname) - 1);
    redirect_hostname[sizeof(redirect_hostname) - 1] = '\0';
    redirect_port = port;
    redirect_party_port = party_port;
    original_gethostbyname = (gethostbyname_fn)GetProcAddress(winsock, "gethostbyname");
    original_connect = (connect_fn)GetProcAddress(winsock, "connect");
    original_send = (send_fn)GetProcAddress(winsock, "send");
    original_recv = (recv_fn)GetProcAddress(winsock, "recv");
    original_sendto = (sendto_fn)GetProcAddress(winsock, "sendto");
    original_recvfrom = (recvfrom_fn)GetProcAddress(winsock, "recvfrom");
    original_closesocket = (closesocket_fn)GetProcAddress(winsock, "closesocket");
    original_wsasend = (wsasend_fn)GetProcAddress(winsock, "WSASend");
    original_wsarecv = (wsarecv_fn)GetProcAddress(winsock, "WSARecv");
    original_wsarecvfrom = (wsarecvfrom_fn)GetProcAddress(winsock, "WSARecvFrom");
    original_wsagetoverlappedresult = (wsagetoverlappedresult_fn)GetProcAddress(winsock, "WSAGetOverlappedResult");
    return original_gethostbyname != NULL && original_connect != NULL;
}

// Modifies: allocates and stores the TLS index used by redirected DNS results.
int fang_network_allocate_redirect_tls(void) {
    redirect_tls_index = TlsAlloc();
    return redirect_tls_index != TLS_OUT_OF_INDEXES;
}

// Reads: returns accumulated DNS import patch diagnostics.
unsigned int fang_network_diagnostics(void) {
    return dns_diagnostics;
}

// Modifies: patches a module's Winsock imports and updates patch counts and DNS diagnostics.
int fang_network_install_module(HMODULE module, int* dns_patches, int* connect_patches) {
    int trace_patches = 0;
    *dns_patches += fang_patch_import(module, "WS2_32.dll", "gethostbyname", 52, (void*)hooked_gethostbyname, &dns_diagnostics);
    *connect_patches += fang_patch_import(module, "WS2_32.dll", "connect", 4, (void*)hooked_connect, NULL);
    *dns_patches += fang_patch_import_address(module, (void*)original_gethostbyname, (void*)hooked_gethostbyname, &dns_diagnostics);
    *connect_patches += fang_patch_import_address(module, (void*)original_connect, (void*)hooked_connect, NULL);
    trace_patches += fang_patch_import(module, "WS2_32.dll", "send", 19, (void*)hooked_send, NULL);
    trace_patches += fang_patch_import(module, "WS2_32.dll", "recv", 16, (void*)hooked_recv, NULL);
    trace_patches += fang_patch_import(module, "WS2_32.dll", "sendto", 20, (void*)hooked_sendto, NULL);
    trace_patches += fang_patch_import(module, "WS2_32.dll", "recvfrom", 17, (void*)hooked_recvfrom, NULL);
    trace_patches += fang_patch_import(module, "WS2_32.dll", "closesocket", 3, (void*)hooked_closesocket, NULL);
    trace_patches += fang_patch_import(module, "WS2_32.dll", "WSASend", 0, (void*)hooked_wsasend, NULL);
    trace_patches += fang_patch_import(module, "WS2_32.dll", "WSARecv", 0, (void*)hooked_wsarecv, NULL);
    trace_patches += fang_patch_import(module, "WS2_32.dll", "WSARecvFrom", 0, (void*)hooked_wsarecvfrom, NULL);
    trace_patches += fang_patch_import(module, "WS2_32.dll", "WSAGetOverlappedResult", 0, (void*)hooked_wsagetoverlappedresult, NULL);
    trace_patches += fang_patch_import_address(module, (void*)original_send, (void*)hooked_send, NULL);
    trace_patches += fang_patch_import_address(module, (void*)original_recv, (void*)hooked_recv, NULL);
    trace_patches += fang_patch_import_address(module, (void*)original_sendto, (void*)hooked_sendto, NULL);
    trace_patches += fang_patch_import_address(module, (void*)original_recvfrom, (void*)hooked_recvfrom, NULL);
    trace_patches += fang_patch_import_address(module, (void*)original_closesocket, (void*)hooked_closesocket, NULL);
    trace_patches += fang_patch_import_address(module, (void*)original_wsasend, (void*)hooked_wsasend, NULL);
    trace_patches += fang_patch_import_address(module, (void*)original_wsarecv, (void*)hooked_wsarecv, NULL);
    trace_patches += fang_patch_import_address(module, (void*)original_wsarecvfrom, (void*)hooked_wsarecvfrom, NULL);
    trace_patches += fang_patch_import_address(module, (void*)original_wsagetoverlappedresult, (void*)hooked_wsagetoverlappedresult, NULL);
    return trace_patches;
}

/* Unhooked control-channel socket setup; authentication remains core-owned. */
// Modifies: opens and connects a control socket, applies timeouts, and closes it on setup failure.
SOCKET fang_network_control_open(const struct sockaddr_in* address) {
    SOCKET control = socket(AF_INET, SOCK_STREAM, IPPROTO_TCP);
    u_long is_nonblocking = 1;
    fd_set writes;
    fd_set errors;
    struct timeval timeout;
    int connection_error = 0;
    int error_length = sizeof(connection_error);
    DWORD operation_timeout = 500;
    if (control == INVALID_SOCKET) {
        return INVALID_SOCKET;
    }
    if (ioctlsocket(control, FIONBIO, &is_nonblocking) == SOCKET_ERROR) {
        fang_network_control_close(control);
        return INVALID_SOCKET;
    }
    if (fang_network_control_connect(control, (const struct sockaddr*)address, sizeof(*address)) == SOCKET_ERROR) {
        if (WSAGetLastError() != WSAEWOULDBLOCK) {
            fang_network_control_close(control);
            return INVALID_SOCKET;
        }
        FD_ZERO(&writes);
        FD_ZERO(&errors);
        FD_SET(control, &writes);
        FD_SET(control, &errors);
        timeout.tv_sec = 0;
        timeout.tv_usec = 500000;
        if (select(0, NULL, &writes, &errors, &timeout) <= 0 ||
            FD_ISSET(control, &errors) ||
            getsockopt(control, SOL_SOCKET, SO_ERROR,
                (char*)&connection_error, &error_length) == SOCKET_ERROR || connection_error != 0) {
            fang_network_control_close(control);
            return INVALID_SOCKET;
        }
    }
    is_nonblocking = 0;
    if (ioctlsocket(control, FIONBIO, &is_nonblocking) == SOCKET_ERROR ||
        setsockopt(control, SOL_SOCKET, SO_RCVTIMEO,
            (const char*)&operation_timeout, sizeof(operation_timeout)) == SOCKET_ERROR ||
        setsockopt(control, SOL_SOCKET, SO_SNDTIMEO,
            (const char*)&operation_timeout, sizeof(operation_timeout)) == SOCKET_ERROR) {
        fang_network_control_close(control);
        return INVALID_SOCKET;
    }
    return control;
}


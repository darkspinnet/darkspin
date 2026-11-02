#ifndef DARKSPIN_FANG_NETWORK_H
#define DARKSPIN_FANG_NETWORK_H

#include "fang.h"
#include <stdint.h>

/* Native policy remains core-owned; these ports read it at the hook call site. */
int fang_network_capture_is_enabled(void);
LONG fang_network_matchmaking_pending(void);
int fang_network_login_is_pending(void);
uintptr_t fang_trace_caller_rva(void* caller);
uint64_t fang_trace_digest(const char* buffer, int length);

void fang_network_reset_diagnostics(void);
int fang_network_configure(const char* hostname, unsigned short port,
    unsigned short party_port, HMODULE winsock);
int fang_network_allocate_redirect_tls(void);
int fang_network_install_module(HMODULE module, int* dns_patches, int* connect_patches);
unsigned int fang_network_diagnostics(void);

/* Snapshot diagnostics use the original exports to avoid tracing themselves. */
int fang_network_control_is_ready(void);
SOCKET fang_network_control_open(const struct sockaddr_in* address);
int fang_network_control_connect(SOCKET socket, const struct sockaddr* address, int length);
int fang_network_control_send(SOCKET socket, const char* buffer, int length, int flags);
int fang_network_control_recv(SOCKET socket, char* buffer, int length, int flags);
int fang_network_control_close(SOCKET socket);

#endif

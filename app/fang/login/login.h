#ifndef DARKSPIN_FANG_LOGIN_H
#define DARKSPIN_FANG_LOGIN_H

#include "client103/abi.h"

int fang_login_processing(void* controller);
void fang_login_receive(SOCKET socket, const char* bytes, unsigned int size);
void fang_login_forget_socket(SOCKET socket);
int fang_login_rejected(unsigned int code);
int fang_login_not_ready(void);
void fang_login_terminate(unsigned int exit_code);

#endif

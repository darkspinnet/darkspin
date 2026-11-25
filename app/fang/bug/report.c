#include "fang.h"
#include "bug/report.h"
#include <stdio.h>
#include <string.h>

static SRWLOCK report_lock = SRWLOCK_INIT;
static char previous_description[4096];
static ULONGLONG previous_tick;

// Only publish a small owned-launch request. Darkspinner performs all archive
// collection/compression outside the client's limited address space.
void fang_report_bug(const char* command) {
    char path[32768], temporary[32800], launch_id[64], document[4160];
    char remote[8], address[1024];
    const char* description=command;
    char* separator;
    DWORD length, written;
    unsigned int index;
    ULONGLONG tick=GetTickCount64();
    HANDLE output;
    int count, is_written;
    length=GetEnvironmentVariableA("DARKSPIN_REMOTE_SERVER",remote,sizeof(remote));
    if (length==1 && remote[0]=='0') return;
    if (length!=1 || remote[0]!='1') {
        length=GetEnvironmentVariableA("DARKSPIN_SERVER_ADDRESS",address,sizeof(address));
        if (!length || length>=sizeof(address) || !_strnicmp(address,"localhost:",10) ||
            !strncmp(address,"127.",4) || !strncmp(address,"[::1]:",6)) return;
    }
    if (!description) return;
    while (*description && !strchr(" \t\r\n",*description)) description++;
    while (*description && strchr(" \t\r\n",*description)) description++;
    if (!*description || strlen(description)>=sizeof(previous_description)) return;
    length=GetEnvironmentVariableA("DARKSPIN_CLIENT_RESULT",path,sizeof(path));
    if (!length || length>=sizeof(path)) return;
    separator=strrchr(path,'\\');
    if (!separator) return;
    length=GetEnvironmentVariableA("DARKSPIN_CLIENT_LAUNCH_ID",launch_id,sizeof(launch_id));
    if (length!=32) return;
    for(index=0; index<length; index++) if (!strchr("0123456789abcdef",launch_id[index])) return;
    AcquireSRWLockExclusive(&report_lock);
    if (tick-previous_tick<2000 && !strcmp(description,previous_description)) {
        ReleaseSRWLockExclusive(&report_lock); return;
    }
    strcpy(previous_description,description); previous_tick=tick;
    ReleaseSRWLockExclusive(&report_lock);
    count=snprintf(separator+1,sizeof(path)-(separator+1-path),"%s.bug.%llu.txt",launch_id,tick);
    if (count<=0 || count>=(int)(sizeof(path)-(separator+1-path))) return;
    count=snprintf(temporary,sizeof(temporary),"%s.tmp",path);
    if (count<=0 || count>=sizeof(temporary)) return;
    count=snprintf(document,sizeof(document),"%lu\n%s",GetCurrentProcessId(),description);
    if (count<=0 || count>=sizeof(document)) return;
    output=CreateFileA(temporary,GENERIC_WRITE,0,NULL,CREATE_NEW,FILE_ATTRIBUTE_NORMAL,NULL);
    if (output==INVALID_HANDLE_VALUE) return;
    is_written=WriteFile(output,document,count,&written,NULL) && written==(DWORD)count;
    CloseHandle(output);
    if (!is_written || !MoveFileExA(temporary,path,MOVEFILE_WRITE_THROUGH)) {
        DeleteFileA(temporary);
        OutputDebugStringA("Fang: launcher bug capture request failed\n");
    }
}

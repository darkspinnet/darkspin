#include <windows.h>
#include <string.h>

typedef DWORD (WINAPI *get_file_version_info_size_a_fn)(LPCSTR, LPDWORD);
typedef DWORD (WINAPI *get_file_version_info_size_w_fn)(LPCWSTR, LPDWORD);
typedef DWORD (WINAPI *get_file_version_info_size_ex_a_fn)(DWORD, LPCSTR, LPDWORD);
typedef DWORD (WINAPI *get_file_version_info_size_ex_w_fn)(DWORD, LPCWSTR, LPDWORD);
typedef BOOL (WINAPI *get_file_version_info_a_fn)(LPCSTR, DWORD, DWORD, LPVOID);
typedef BOOL (WINAPI *get_file_version_info_w_fn)(LPCWSTR, DWORD, DWORD, LPVOID);
typedef BOOL (WINAPI *get_file_version_info_ex_a_fn)(DWORD, LPCSTR, DWORD, DWORD, LPVOID);
typedef BOOL (WINAPI *get_file_version_info_ex_w_fn)(DWORD, LPCWSTR, DWORD, DWORD, LPVOID);
typedef DWORD (APIENTRY *ver_find_file_a_fn)(DWORD, LPSTR, LPSTR, LPSTR, LPSTR, PUINT, LPSTR, PUINT);
typedef DWORD (APIENTRY *ver_find_file_w_fn)(DWORD, LPWSTR, LPWSTR, LPWSTR, LPWSTR, PUINT, LPWSTR, PUINT);
typedef DWORD (APIENTRY *ver_install_file_a_fn)(DWORD, LPSTR, LPSTR, LPSTR, LPSTR, LPSTR, LPSTR, PUINT);
typedef DWORD (APIENTRY *ver_install_file_w_fn)(DWORD, LPWSTR, LPWSTR, LPWSTR, LPWSTR, LPWSTR, LPWSTR, PUINT);
typedef DWORD (WINAPI *ver_language_name_a_fn)(DWORD, LPSTR, DWORD);
typedef DWORD (WINAPI *ver_language_name_w_fn)(DWORD, LPWSTR, DWORD);
typedef BOOL (WINAPI *ver_query_value_a_fn)(LPCVOID, LPCSTR, LPVOID*, PUINT);
typedef BOOL (WINAPI *ver_query_value_w_fn)(LPCVOID, LPCWSTR, LPVOID*, PUINT);
typedef DWORD (WINAPI *fang_initialize_fn)(LPVOID);

static HMODULE version_module;

__declspec(dllexport) const char darkspinner_proxy_marker[] = "darkspin-version-proxy-v1";

static void proxy_log(const char* stage, DWORD result) {
    char path[32768];
    char line[256];
    HANDLE file;
    DWORD length;
    DWORD written;
    length = GetEnvironmentVariableA("DARKSPIN_PROXY_LOG", path, sizeof(path));
    if (length == 0 || length >= sizeof(path)) {
        return;
    }
    file = CreateFileA(path, FILE_APPEND_DATA, FILE_SHARE_READ | FILE_SHARE_WRITE,
        NULL, OPEN_ALWAYS, FILE_ATTRIBUTE_NORMAL, NULL);
    if (file == INVALID_HANDLE_VALUE) {
        return;
    }
    length = (DWORD)wsprintfA(line, "%s: 0x%08lx\r\n", stage, (unsigned long)result);
    WriteFile(file, line, length, &written, NULL);
    CloseHandle(file);
}

static HMODULE load_system_version(void) {
    char path[32768];
    DWORD length;
    if (version_module != NULL) {
        return version_module;
    }
    length = GetEnvironmentVariableA("DARKSPIN_VERSION_DLL", path, sizeof(path));
    if (length == 0) {
        length = GetSystemDirectoryA(path, sizeof(path));
        if (length == 0 || length + sizeof("\\version.dll") >= sizeof(path)) {
            return NULL;
        }
        lstrcatA(path, "\\version.dll");
    } else if (length >= sizeof(path)) {
        return NULL;
    }
    version_module = LoadLibraryA(path);
    proxy_log("version_load", version_module == NULL ? GetLastError() : 0);
    return version_module;
}

static FARPROC version_function(const char* name) {
    HMODULE module = load_system_version();
    if (module == NULL) {
        return NULL;
    }
    return GetProcAddress(module, name);
}

__declspec(dllexport) DWORD WINAPI GetFileVersionInfoSizeA(LPCSTR filename, LPDWORD handle) {
    get_file_version_info_size_a_fn function = (get_file_version_info_size_a_fn)version_function("GetFileVersionInfoSizeA");
    if (function == NULL) {
        SetLastError(ERROR_PROC_NOT_FOUND);
        return 0;
    }
    return function(filename, handle);
}

__declspec(dllexport) DWORD WINAPI GetFileVersionInfoSizeW(LPCWSTR filename, LPDWORD handle) {
    get_file_version_info_size_w_fn function = (get_file_version_info_size_w_fn)version_function("GetFileVersionInfoSizeW");
    if (function == NULL) {
        SetLastError(ERROR_PROC_NOT_FOUND);
        return 0;
    }
    return function(filename, handle);
}

__declspec(dllexport) DWORD WINAPI GetFileVersionInfoSizeExA(DWORD flags, LPCSTR filename, LPDWORD handle) {
    get_file_version_info_size_ex_a_fn function = (get_file_version_info_size_ex_a_fn)version_function("GetFileVersionInfoSizeExA");
    if (function == NULL) {
        SetLastError(ERROR_PROC_NOT_FOUND);
        return 0;
    }
    return function(flags, filename, handle);
}

__declspec(dllexport) DWORD WINAPI GetFileVersionInfoSizeExW(DWORD flags, LPCWSTR filename, LPDWORD handle) {
    get_file_version_info_size_ex_w_fn function = (get_file_version_info_size_ex_w_fn)version_function("GetFileVersionInfoSizeExW");
    if (function == NULL) {
        SetLastError(ERROR_PROC_NOT_FOUND);
        return 0;
    }
    return function(flags, filename, handle);
}

__declspec(dllexport) BOOL WINAPI GetFileVersionInfoA(LPCSTR filename, DWORD handle, DWORD length, LPVOID data) {
    get_file_version_info_a_fn function = (get_file_version_info_a_fn)version_function("GetFileVersionInfoA");
    if (function == NULL) {
        SetLastError(ERROR_PROC_NOT_FOUND);
        return FALSE;
    }
    return function(filename, handle, length, data);
}

__declspec(dllexport) BOOL WINAPI GetFileVersionInfoW(LPCWSTR filename, DWORD handle, DWORD length, LPVOID data) {
    get_file_version_info_w_fn function = (get_file_version_info_w_fn)version_function("GetFileVersionInfoW");
    if (function == NULL) {
        SetLastError(ERROR_PROC_NOT_FOUND);
        return FALSE;
    }
    return function(filename, handle, length, data);
}

__declspec(dllexport) BOOL WINAPI GetFileVersionInfoExA(DWORD flags, LPCSTR filename, DWORD handle, DWORD length, LPVOID data) {
    get_file_version_info_ex_a_fn function = (get_file_version_info_ex_a_fn)version_function("GetFileVersionInfoExA");
    if (function == NULL) {
        SetLastError(ERROR_PROC_NOT_FOUND);
        return FALSE;
    }
    return function(flags, filename, handle, length, data);
}

__declspec(dllexport) BOOL WINAPI GetFileVersionInfoExW(DWORD flags, LPCWSTR filename, DWORD handle, DWORD length, LPVOID data) {
    get_file_version_info_ex_w_fn function = (get_file_version_info_ex_w_fn)version_function("GetFileVersionInfoExW");
    if (function == NULL) {
        SetLastError(ERROR_PROC_NOT_FOUND);
        return FALSE;
    }
    return function(flags, filename, handle, length, data);
}

__declspec(dllexport) DWORD APIENTRY VerFindFileA(DWORD flags, LPSTR filename, LPSTR windows_directory,
    LPSTR application_directory, LPSTR current_directory, PUINT current_length,
    LPSTR destination_directory, PUINT destination_length) {
    ver_find_file_a_fn function = (ver_find_file_a_fn)version_function("VerFindFileA");
    if (function == NULL) return ERROR_PROC_NOT_FOUND;
    return function(flags, filename, windows_directory, application_directory, current_directory,
        current_length, destination_directory, destination_length);
}

__declspec(dllexport) DWORD APIENTRY VerFindFileW(DWORD flags, LPWSTR filename, LPWSTR windows_directory,
    LPWSTR application_directory, LPWSTR current_directory, PUINT current_length,
    LPWSTR destination_directory, PUINT destination_length) {
    ver_find_file_w_fn function = (ver_find_file_w_fn)version_function("VerFindFileW");
    if (function == NULL) return ERROR_PROC_NOT_FOUND;
    return function(flags, filename, windows_directory, application_directory, current_directory,
        current_length, destination_directory, destination_length);
}

__declspec(dllexport) DWORD APIENTRY VerInstallFileA(DWORD flags, LPSTR source_filename,
    LPSTR destination_filename, LPSTR source_directory, LPSTR destination_directory,
    LPSTR current_directory, LPSTR temporary_file, PUINT temporary_length) {
    ver_install_file_a_fn function = (ver_install_file_a_fn)version_function("VerInstallFileA");
    if (function == NULL) return ERROR_PROC_NOT_FOUND;
    return function(flags, source_filename, destination_filename, source_directory,
        destination_directory, current_directory, temporary_file, temporary_length);
}

__declspec(dllexport) DWORD APIENTRY VerInstallFileW(DWORD flags, LPWSTR source_filename,
    LPWSTR destination_filename, LPWSTR source_directory, LPWSTR destination_directory,
    LPWSTR current_directory, LPWSTR temporary_file, PUINT temporary_length) {
    ver_install_file_w_fn function = (ver_install_file_w_fn)version_function("VerInstallFileW");
    if (function == NULL) return ERROR_PROC_NOT_FOUND;
    return function(flags, source_filename, destination_filename, source_directory,
        destination_directory, current_directory, temporary_file, temporary_length);
}

__declspec(dllexport) DWORD WINAPI VerLanguageNameA(DWORD language, LPSTR buffer, DWORD length) {
    ver_language_name_a_fn function = (ver_language_name_a_fn)version_function("VerLanguageNameA");
    if (function == NULL) return 0;
    return function(language, buffer, length);
}

__declspec(dllexport) DWORD WINAPI VerLanguageNameW(DWORD language, LPWSTR buffer, DWORD length) {
    ver_language_name_w_fn function = (ver_language_name_w_fn)version_function("VerLanguageNameW");
    if (function == NULL) return 0;
    return function(language, buffer, length);
}

__declspec(dllexport) BOOL WINAPI VerQueryValueA(LPCVOID block, LPCSTR sub_block, LPVOID* buffer, PUINT length) {
    ver_query_value_a_fn function = (ver_query_value_a_fn)version_function("VerQueryValueA");
    if (function == NULL) {
        SetLastError(ERROR_PROC_NOT_FOUND);
        return FALSE;
    }
    return function(block, sub_block, buffer, length);
}

__declspec(dllexport) BOOL WINAPI VerQueryValueW(LPCVOID block, LPCWSTR sub_block, LPVOID* buffer, PUINT length) {
    ver_query_value_w_fn function = (ver_query_value_w_fn)version_function("VerQueryValueW");
    if (function == NULL) {
        SetLastError(ERROR_PROC_NOT_FOUND);
        return FALSE;
    }
    return function(block, sub_block, buffer, length);
}

static DWORD WINAPI initialize_fang(LPVOID parameter) {
    char path[32768];
    HMODULE fang;
    fang_initialize_fn initialize;
    DWORD length;
    (void)parameter;
    proxy_log("initialize_start", 0);
    length = GetEnvironmentVariableA("DARKSPIN_FANG_DLL", path, sizeof(path));
    if (length == 0 || length >= sizeof(path)) {
        proxy_log("fang_environment", ERROR_ENVVAR_NOT_FOUND);
        return ERROR_ENVVAR_NOT_FOUND;
    }
    fang = LoadLibraryA(path);
    if (fang == NULL) {
        DWORD result = GetLastError();
        proxy_log("fang_load", result);
        return result;
    }
    proxy_log("fang_load", 0);
    initialize = (fang_initialize_fn)GetProcAddress(fang, "RecapInitializeThread");
    if (initialize == NULL) {
        initialize = (fang_initialize_fn)GetProcAddress(fang, "RecapInitializeThread@4");
    }
    if (initialize == NULL) {
        proxy_log("fang_export", ERROR_PROC_NOT_FOUND);
        return ERROR_PROC_NOT_FOUND;
    }
    length = initialize(NULL);
    proxy_log("fang_initialize", length);
    return length;
}

static BOOL is_darkspinner_launch(void) {
    char marker[8];
    DWORD length = GetEnvironmentVariableA("DARKSPIN_DARKSPINNER_LAUNCH", marker, sizeof(marker));
    return length == 1 && marker[0] == '1';
}

static DWORD launch_darkspinner(void) {
    char executable_path[32768];
    char command_line[32768];
    char* separator;
    DWORD length;
    STARTUPINFOA startup;
    PROCESS_INFORMATION process;

    length = GetModuleFileNameA(NULL, executable_path, sizeof(executable_path));
    if (length == 0 || length >= sizeof(executable_path)) {
        return GetLastError();
    }
    separator = strrchr(executable_path, '\\');
    if (separator == NULL) {
        return ERROR_PATH_NOT_FOUND;
    }
    *separator = '\0';
    separator = strrchr(executable_path, '\\');
    if (separator == NULL) {
        return ERROR_PATH_NOT_FOUND;
    }
    *(separator + 1) = '\0';
    if (lstrlenA(executable_path) + lstrlenA("darkspinner.exe") + 1 >= sizeof(executable_path)) {
        return ERROR_INSUFFICIENT_BUFFER;
    }
    lstrcatA(executable_path, "darkspinner.exe");
    if (GetFileAttributesA(executable_path) == INVALID_FILE_ATTRIBUTES) {
        return ERROR_FILE_NOT_FOUND;
    }
    if (lstrlenA(executable_path) + lstrlenA("\"\" --steam-launch") + 1 >= sizeof(command_line)) {
        return ERROR_INSUFFICIENT_BUFFER;
    }
    command_line[0] = '\"';
    command_line[1] = '\0';
    lstrcatA(command_line, executable_path);
    lstrcatA(command_line, "\" --steam-launch");
    ZeroMemory(&startup, sizeof(startup));
    startup.cb = sizeof(startup);
    ZeroMemory(&process, sizeof(process));
    if (!CreateProcessA(executable_path, command_line, NULL, NULL, FALSE,
        CREATE_DEFAULT_ERROR_MODE, NULL, NULL, &startup, &process)) {
        return GetLastError();
    }
    CloseHandle(process.hThread);
    CloseHandle(process.hProcess);
    return ERROR_SUCCESS;
}

static DWORD WINAPI initialize_proxy(LPVOID parameter) {
    char fang_path[32768];
    DWORD length;
    DWORD result;
    (void)parameter;

    /* Managed Windows launches inject and initialize Fang before resuming the
       client. Loading it again here reopens the same trace and attempts to
       install every hook a second time. */
    if (is_darkspinner_launch()) {
        proxy_log("managed_launch", 0);
        return ERROR_SUCCESS;
    }

    length = GetEnvironmentVariableA("DARKSPIN_FANG_DLL", fang_path, sizeof(fang_path));
    if (length > 0 && length < sizeof(fang_path)) {
        return initialize_fang(NULL);
    }
    result = launch_darkspinner();
    proxy_log("spinner_launch", result);
    if (result == ERROR_SUCCESS) {
        ExitProcess(0);
    }
    return result;
}

BOOL WINAPI DllMain(HINSTANCE instance, DWORD reason, LPVOID reserved) {
    HANDLE thread;
    (void)reserved;
    if (reason != DLL_PROCESS_ATTACH) {
        return TRUE;
    }
    DisableThreadLibraryCalls(instance);
    thread = CreateThread(NULL, 0, initialize_proxy, NULL, 0, NULL);
    if (thread != NULL) {
        proxy_log("thread_create", 0);
        CloseHandle(thread);
    } else {
        proxy_log("thread_create", GetLastError());
    }
    return TRUE;
}

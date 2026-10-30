#include "client103/image.h"
#include "hook/patch.h"

#include <stdio.h>
#include <string.h>
#include <winver.h>

enum { validation_capacity = 64, maximum_version_size = 1024 * 1024 };
typedef struct validation_failure {
    char feature[48], symbol[64], reason[48], evidence[160];
    uint32_t rva;
} validation_failure;

static SRWLOCK validation_lock = SRWLOCK_INIT;
static validation_failure failures[validation_capacity];
static unsigned int failure_count;
static unsigned int omitted_count;
static fang_client103_image observed_image;

// Modifies: records a build-103 image validation failure and returns rejection.
static int reject_image(const char* feature, const char* symbol, uint32_t rva,
    const char* reason, const char* evidence) {
    AcquireSRWLockExclusive(&validation_lock);
    if (failure_count < validation_capacity) {
        validation_failure* failure = &failures[failure_count++];
        snprintf(failure->feature, sizeof(failure->feature), "%s", feature == NULL ? "image" : feature);
        snprintf(failure->symbol, sizeof(failure->symbol), "%s", symbol == NULL ? "unknown" : symbol);
        snprintf(failure->reason, sizeof(failure->reason), "%s", reason);
        snprintf(failure->evidence, sizeof(failure->evidence), "%s", evidence == NULL ? "unspecified" : evidence);
        failure->rva = rva;
    } else {
        omitted_count++;
    }
    ReleaseSRWLockExclusive(&validation_lock);
    return 0;
}

// Reads: resolves a bounded, readable span within a validated client image.
void* fang_client103_at(const fang_client103_image* image, uint32_t rva, size_t length) {
    if (image == NULL || image->base == NULL || rva >= image->size ||
        length == 0 || length > image->size - rva ||
        (uintptr_t)image->base > UINTPTR_MAX - rva) return NULL;
    if (!readable_range(image->base + rva, length)) return NULL;
    return image->base + rva;
}

// Reads/modifies: validates the loaded build-103 image and writes image metadata or rejection diagnostics.
int fang_client103_image_open(HMODULE executable, size_t minimum, fang_client103_image* image) {
    const IMAGE_DOS_HEADER* dos = (const IMAGE_DOS_HEADER*)executable;
    const IMAGE_NT_HEADERS32* nt;
    HRSRC resource;
    HGLOBAL loaded;
    DWORD resource_size;
    const void* version_data;
    VS_FIXEDFILEINFO* version = NULL;
    UINT version_size = 0;
    fang_client103_image candidate;
    if (image == NULL) return reject_image("image", "output", 0, "missing output", "build103 PE32");
    memset(image, 0, sizeof(*image));
    if (sizeof(void*) != 4 || !readable_range(dos, sizeof(*dos)) ||
        dos->e_magic != IMAGE_DOS_SIGNATURE || dos->e_lfanew <= 0 || dos->e_lfanew > 1024 * 1024 ||
        (uintptr_t)executable > UINTPTR_MAX - (uint32_t)dos->e_lfanew) {
        return reject_image("image", "dosHeader", 0, "unsupported DOS header", "build103 PE32 i386");
    }
    nt = (const IMAGE_NT_HEADERS32*)((const BYTE*)executable + dos->e_lfanew);
    if (!readable_range(nt, sizeof(*nt)) || nt->Signature != IMAGE_NT_SIGNATURE ||
        nt->FileHeader.Machine != IMAGE_FILE_MACHINE_I386 ||
        nt->FileHeader.SizeOfOptionalHeader < sizeof(IMAGE_OPTIONAL_HEADER32) ||
        nt->OptionalHeader.Magic != IMAGE_NT_OPTIONAL_HDR32_MAGIC ||
        nt->OptionalHeader.SizeOfHeaders < (uint32_t)dos->e_lfanew + sizeof(*nt) ||
        nt->OptionalHeader.SizeOfHeaders > nt->OptionalHeader.SizeOfImage ||
        nt->OptionalHeader.SizeOfImage < minimum || nt->OptionalHeader.SizeOfImage == 0 ||
        (uintptr_t)executable > UINTPTR_MAX - nt->OptionalHeader.SizeOfImage) {
        return reject_image("image", "ntHeader", 0, "unsupported PE32 image", "build103 PE32 i386");
    }
    resource = FindResourceW(executable, MAKEINTRESOURCEW(VS_VERSION_INFO), MAKEINTRESOURCEW(16));
    resource_size = resource == NULL ? 0 : SizeofResource(executable, resource);
    loaded = resource == NULL ? NULL : LoadResource(executable, resource);
    version_data = loaded == NULL ? NULL : LockResource(loaded);
    /* Bound the root VERSIONINFO block before asking the Windows version
     * parser to locate its fixed data. No file reads or resource copies. */
    if (resource_size < 40 + sizeof(*version) || resource_size > maximum_version_size ||
        !readable_range(version_data, resource_size) ||
        *(const WORD*)version_data < 40 + sizeof(*version) ||
        *(const WORD*)version_data > resource_size ||
        *((const WORD*)version_data + 1) != sizeof(*version) ||
        *((const WORD*)version_data + 2) != 0 ||
        memcmp((const BYTE*)version_data + 6, L"VS_VERSION_INFO", sizeof(L"VS_VERSION_INFO")) != 0 ||
        !VerQueryValueW(version_data, L"\\", (LPVOID*)&version, &version_size) ||
        version_size < sizeof(*version) || (uintptr_t)version < (uintptr_t)version_data ||
        (uintptr_t)version - (uintptr_t)version_data > resource_size - sizeof(*version) ||
        !readable_range(version, sizeof(*version)) || version->dwSignature != 0xFEEF04BD ||
        version->dwFileVersionMS != 0x00050003 || version->dwFileVersionLS != 103) {
        return reject_image("image", "fileVersion", 0, "expected 5.3.0.103", "loaded build103 VS_FIXEDFILEINFO");
    }
    candidate.base = (BYTE*)executable;
    candidate.size = nt->OptionalHeader.SizeOfImage;
    candidate.timestamp = nt->FileHeader.TimeDateStamp;
    candidate.checksum = nt->OptionalHeader.CheckSum;
    candidate.version_ms = version->dwFileVersionMS;
    candidate.version_ls = version->dwFileVersionLS;
    *image = candidate;
    AcquireSRWLockExclusive(&validation_lock);
    observed_image = candidate;
    ReleaseSRWLockExclusive(&validation_lock);
    return 1;
}

// Reads/modifies: checks binding bytes and records unreadable or mismatched signatures.
int fang_client103_expect(const fang_client103_image* image, const char* feature,
    const char* symbol, uint32_t rva, const BYTE* bytes, size_t length, const char* evidence) {
    const void* address = fang_client103_at(image, rva, length);
    if (address == NULL || bytes == NULL) {
        return reject_image(feature, symbol, rva, "unreadable binding", evidence);
    }
    if (memcmp(address, bytes, length) != 0) {
        return reject_image(feature, symbol, rva, "signature mismatch", evidence);
    }
    return 1;
}

// Reads/modifies: resolves a readable binding and records a validation failure if unavailable.
void* fang_client103_binding(const fang_client103_image* image, const char* feature,
    const char* symbol, uint32_t rva, size_t length, const char* evidence) {
    void* address = fang_client103_at(image, rva, length);
    if (address == NULL) reject_image(feature, symbol, rva, "unreadable binding", evidence);
    return address;
}

// Modifies: clears the caller's validity flag and records diagnostics when a required binding is unavailable.
void* fang_client103_required_binding(const fang_client103_image* image,
    const char* symbol, uint32_t rva, int* is_valid) {
    void* address = fang_client103_binding(image, "bootstrap", symbol, rva, 1,
        "existing build103 Fang interface; range and checked patch target");
    if (address == NULL) *is_valid = 0;
    return address;
}

// Modifies: writes escaped JSON text to an output buffer.
static void report_text(char* output, size_t capacity, const char* text) {
    size_t count = 0;
    while (*text != 0 && count + 2 < capacity) {
        unsigned char character = (unsigned char)*text++;
        if (character < 32) continue;
        if (character == '\\' || character == '"') output[count++] = '\\';
        output[count++] = (char)character;
    }
    output[count] = 0;
}

// Reads/modifies: snapshots image validation state and sends JSON records to the supplied writer.
void fang_client103_report(void (*write_line)(const char*, unsigned int)) {
    unsigned int index, count, omitted;
    fang_client103_image image;
    char line[1024];
    int length;
    if (write_line == NULL) return;
    AcquireSRWLockShared(&validation_lock);
    image = observed_image;
    count = failure_count;
    omitted = omitted_count;
    ReleaseSRWLockShared(&validation_lock);
    length = snprintf(line, sizeof(line),
        "{\"protocol\":\"fang_client103\",\"build\":103,\"record\":\"image\",\"base\":%llu,"
        "\"size\":%lu,\"timestamp\":%lu,\"checksum\":%lu,\"version\":\"%lu.%lu.%lu.%lu\",\"is_valid\":%s}\r\n",
        (unsigned long long)(uintptr_t)image.base, (unsigned long)image.size,
        (unsigned long)image.timestamp, (unsigned long)image.checksum,
        (unsigned long)(image.version_ms >> 16), (unsigned long)(image.version_ms & 0xFFFF),
        (unsigned long)(image.version_ls >> 16), (unsigned long)(image.version_ls & 0xFFFF),
        image.base == NULL ? "false" : "true");
    if (length > 0 && (size_t)length < sizeof(line)) write_line(line, (unsigned int)length);
    for (index = 0; index < count; index++) {
        validation_failure failure;
        char feature[96], symbol[128], reason[96], evidence[320];
        AcquireSRWLockShared(&validation_lock);
        failure = failures[index];
        ReleaseSRWLockShared(&validation_lock);
        report_text(feature, sizeof(feature), failure.feature);
        report_text(symbol, sizeof(symbol), failure.symbol);
        report_text(reason, sizeof(reason), failure.reason);
        report_text(evidence, sizeof(evidence), failure.evidence);
        length = snprintf(line, sizeof(line),
            "{\"protocol\":\"fang_client103\",\"build\":103,\"record\":\"binding_rejection\","
            "\"feature\":\"%s\",\"symbol\":\"%s\",\"rva\":%lu,\"reason\":\"%s\",\"evidence\":\"%s\"}\r\n",
            feature, symbol, (unsigned long)failure.rva, reason, evidence);
        if (length > 0 && (size_t)length < sizeof(line)) write_line(line, (unsigned int)length);
    }
    if (omitted != 0) {
        length = snprintf(line, sizeof(line),
            "{\"protocol\":\"fang_client103\",\"build\":103,\"record\":\"omitted_failures\",\"count\":%u}\r\n", omitted);
        if (length > 0 && (size_t)length < sizeof(line)) write_line(line, (unsigned int)length);
    }
}

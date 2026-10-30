#include "mod/mod.h"
#include "hook.h"

#include <stdint.h>
#include <string.h>

/* Build 103 EA::IO::IStream ABI, observed at FileStream's 0x103F238
 * vtable. DatabasePackedFile owns one stream reference at byte offset 608.
 * Only that new database instance receives this stream; no global file hooks.
 * Header/index bytes are translated in memory. Payloads stay in the read-only
 * DBBF file, including their original compression and 64-bit source offsets.
 */
typedef struct mod_stream mod_stream;
typedef struct {
    void* (FANG_THISCALL* destroy)(mod_stream*, unsigned int);
    int (FANG_THISCALL* add_ref)(mod_stream*);
    int (FANG_THISCALL* release)(mod_stream*);
    unsigned int (FANG_THISCALL* type)(mod_stream*);
    int (FANG_THISCALL* access)(mod_stream*);
    int (FANG_THISCALL* state)(mod_stream*);
    unsigned char (FANG_THISCALL* close)(mod_stream*);
    unsigned int (FANG_THISCALL* size)(mod_stream*);
    unsigned char (FANG_THISCALL* resize)(mod_stream*, unsigned int);
    int (FANG_THISCALL* position)(mod_stream*, int);
    unsigned char (FANG_THISCALL* seek)(mod_stream*, int, int);
    unsigned int (FANG_THISCALL* available)(mod_stream*);
    unsigned int (FANG_THISCALL* read)(mod_stream*, void*, unsigned int);
    unsigned char (FANG_THISCALL* flush)(mod_stream*);
    unsigned char (FANG_THISCALL* write)(mod_stream*, const void*, unsigned int);
} mod_stream_methods;

typedef struct {
    uint64_t source_offset;
    uint32_t start;
    uint32_t size;
} mod_resource_span;

struct mod_stream {
    const mod_stream_methods* methods;
    LONG refs;
    HANDLE source;
    CRITICAL_SECTION lock;
    BYTE* prefix;
    mod_resource_span* spans;
    uint32_t count;
    uint32_t prefix_size;
    uint32_t size;
    uint32_t position;
    DWORD error;
    unsigned int package_index;
    int is_payload_traced;
};

static uint32_t mod_u32(const BYTE* p) {
    uint32_t word;
    memcpy(&word, p, sizeof(word));
    return word;
}

static uint64_t mod_u64(const BYTE* p) {
    uint64_t word;
    memcpy(&word, p, sizeof(word));
    return word;
}

static void mod_put32(BYTE* p, uint32_t word) {
    memcpy(p, &word, sizeof(word));
}

static int mod_source_read(mod_stream* stream, uint64_t offset, void* buffer, DWORD size) {
    LARGE_INTEGER position;
    DWORD count = 0;
    position.QuadPart = (LONGLONG)offset;
    if (!SetFilePointerEx(stream->source, position, NULL, FILE_BEGIN) ||
        !ReadFile(stream->source, buffer, size, &count, NULL)) {
        stream->error = GetLastError();
        return 0;
    }
    if (count != size) {
        stream->error = ERROR_HANDLE_EOF;
        return 0;
    }
    return 1;
}

static unsigned char FANG_THISCALL mod_stream_close(mod_stream* stream) {
    unsigned char is_closed = 1;
    EnterCriticalSection(&stream->lock);
    if (stream->source != INVALID_HANDLE_VALUE) {
        if (!CloseHandle(stream->source)) {
            stream->error = GetLastError();
            trace_client_state("mod_dbbf_close_error", stream->error);
            is_closed = 0;
        } else {
            stream->source = INVALID_HANDLE_VALUE;
        }
    }
    LeaveCriticalSection(&stream->lock);
    return is_closed;
}

static void* FANG_THISCALL mod_stream_destroy(mod_stream* stream, unsigned int flags) {
    mod_stream_close(stream);
    DeleteCriticalSection(&stream->lock);
    if (stream->prefix) HeapFree(GetProcessHeap(), 0, stream->prefix);
    if (stream->spans) HeapFree(GetProcessHeap(), 0, stream->spans);
    if (flags & 1) HeapFree(GetProcessHeap(), 0, stream);
    return NULL;
}

static int FANG_THISCALL mod_stream_add_ref(mod_stream* stream) {
    return (int)InterlockedIncrement(&stream->refs);
}

static int FANG_THISCALL mod_stream_release(mod_stream* stream) {
    LONG count = InterlockedDecrement(&stream->refs);
    if (!count) mod_stream_destroy(stream, 1);
    return (int)count;
}

static unsigned int FANG_THISCALL mod_stream_type(mod_stream* stream) {
    (void)stream;
    return 0; /* Generic IStream; no FileStream-specific extensions. */
}

static int FANG_THISCALL mod_stream_access(mod_stream* stream) {
    return stream->source == INVALID_HANDLE_VALUE ? 0 : 1;
}

static int FANG_THISCALL mod_stream_state(mod_stream* stream) {
    return stream->source == INVALID_HANDLE_VALUE ? -2 : (int)stream->error;
}

static unsigned int FANG_THISCALL mod_stream_size(mod_stream* stream) {
    return stream->size;
}

static unsigned char FANG_THISCALL mod_stream_resize(mod_stream* stream, unsigned int size) {
    (void)size;
    stream->error = ERROR_ACCESS_DENIED;
    return 0;
}

static int FANG_THISCALL mod_stream_position(mod_stream* stream, int origin) {
    int position;
    EnterCriticalSection(&stream->lock);
    position = origin == 1 ? 0 : (int)stream->position;
    if (origin == 2) position -= (int)stream->size;
    LeaveCriticalSection(&stream->lock);
    return position;
}

static unsigned char FANG_THISCALL mod_stream_seek(mod_stream* stream, int offset, int origin) {
    int64_t position;
    unsigned char is_valid;
    EnterCriticalSection(&stream->lock);
    position = offset;
    if (origin == 1) position += stream->position;
    if (origin == 2) position += stream->size;
    is_valid = stream->source != INVALID_HANDLE_VALUE && origin >= 0 && origin <= 2 &&
        position >= 0 && position <= stream->size;
    if (is_valid) stream->position = (uint32_t)position;
    else stream->error = ERROR_INVALID_PARAMETER;
    LeaveCriticalSection(&stream->lock);
    return is_valid;
}

static unsigned int FANG_THISCALL mod_stream_available(mod_stream* stream) {
    unsigned int count;
    EnterCriticalSection(&stream->lock);
    count = stream->size - stream->position;
    LeaveCriticalSection(&stream->lock);
    return count;
}

static unsigned int FANG_THISCALL mod_stream_read(mod_stream* stream, void* buffer, unsigned int size) {
    BYTE* output = (BYTE*)buffer;
    unsigned int count = 0;
    EnterCriticalSection(&stream->lock);
    if (stream->source == INVALID_HANDLE_VALUE || (!buffer && size)) {
        stream->error = ERROR_INVALID_HANDLE;
        goto failed;
    }
    if (size > stream->size - stream->position) size = stream->size - stream->position;
    while (count < size) {
        uint32_t length = size - count;
        if (stream->position < stream->prefix_size) {
            uint32_t available = stream->prefix_size - stream->position;
            if (length > available) length = available;
            memcpy(output + count, stream->prefix + stream->position, length);
        } else {
            uint32_t low = 0, high = stream->count;
            mod_resource_span* span;
            uint32_t displacement;
            while (low < high) {
                uint32_t middle = low + (high - low) / 2;
                if (stream->spans[middle].start + stream->spans[middle].size <= stream->position)
                    low = middle + 1;
                else high = middle;
            }
            if (low == stream->count) {
                stream->error = ERROR_INVALID_DATA;
                goto failed;
            }
            span = &stream->spans[low];
            displacement = stream->position - span->start;
            if (length > span->size - displacement) length = span->size - displacement;
            if (!mod_source_read(stream, span->source_offset + displacement, output + count, length)) goto failed;
            if (!stream->is_payload_traced) {
                stream->is_payload_traced = 1;
                trace_client_state("mod_dbbf_payload_read", stream->package_index);
            }
        }
        stream->position += length;
        count += length;
    }
    LeaveCriticalSection(&stream->lock);
    return count;
failed:
    trace_client_state("mod_dbbf_read_error", stream->error);
    LeaveCriticalSection(&stream->lock);
    return 0xffffffffu;
}

static unsigned char FANG_THISCALL mod_stream_flush(mod_stream* stream) {
    return stream->source != INVALID_HANDLE_VALUE;
}

static unsigned char FANG_THISCALL mod_stream_write(mod_stream* stream, const void* buffer, unsigned int size) {
    (void)buffer;
    (void)size;
    stream->error = ERROR_ACCESS_DENIED;
    return 0;
}

static const mod_stream_methods stream_methods = {
    mod_stream_destroy, mod_stream_add_ref, mod_stream_release, mod_stream_type,
    mod_stream_access, mod_stream_state, mod_stream_close, mod_stream_size,
    mod_stream_resize, mod_stream_position, mod_stream_seek, mod_stream_available,
    mod_stream_read, mod_stream_flush, mod_stream_write
};

/* The same read-only stream ABI also serves an unmodified launcher-owned PNG
 * to the native importer. One span covers the file; there is no DBPF prefix. */
void* fang_mod_open_source(const wchar_t* path) {
    LARGE_INTEGER size;
    mod_stream* stream;
    HANDLE source = CreateFileW(path, GENERIC_READ, FILE_SHARE_READ, NULL, OPEN_EXISTING,
        FILE_ATTRIBUTE_NORMAL | FILE_FLAG_RANDOM_ACCESS, NULL);
    if (source == INVALID_HANDLE_VALUE) return NULL;
    if (!GetFileSizeEx(source, &size) || size.QuadPart <= 0 || size.QuadPart > 32 * 1024 * 1024) {
        if (!CloseHandle(source)) trace_client_state("mod_source_close_error", GetLastError());
        return NULL;
    }
    stream = (mod_stream*)HeapAlloc(GetProcessHeap(), HEAP_ZERO_MEMORY, sizeof(*stream));
    if (!stream) {
        if (!CloseHandle(source)) trace_client_state("mod_source_close_error", GetLastError());
        return NULL;
    }
    stream->methods = &stream_methods;
    stream->refs = 1;
    stream->source = source;
    stream->size = (uint32_t)size.QuadPart;
    InitializeCriticalSection(&stream->lock);
    stream->spans = (mod_resource_span*)HeapAlloc(GetProcessHeap(), HEAP_ZERO_MEMORY, sizeof(*stream->spans));
    if (!stream->spans) { mod_stream_release(stream); return NULL; }
    stream->count = 1;
    stream->spans[0].size = stream->size;
    return stream;
}

void fang_mod_close_source(void* stream) {
    if (stream) mod_stream_release((mod_stream*)stream);
}

/* Supply a feature-owned virtual package without writing a client asset or
 * cache copy. The read-only backing handle preserves native stream lifetime
 * semantics; all package bytes are owned by this stream's memory prefix. */
int fang_mod_attach_memory(void* database, const wchar_t* path, const BYTE* payload, uint32_t size) {
    mod_stream* stream;
    HANDLE source;
    if (!payload || !size || !readable_range((BYTE*)database + 608, sizeof(void*)) ||
        *(void**)((BYTE*)database + 608)) return 0;
    source = CreateFileW(path, GENERIC_READ, FILE_SHARE_READ, NULL, OPEN_EXISTING,
        FILE_ATTRIBUTE_NORMAL | FILE_FLAG_RANDOM_ACCESS, NULL);
    if (source == INVALID_HANDLE_VALUE) return 0;
    stream = (mod_stream*)HeapAlloc(GetProcessHeap(), HEAP_ZERO_MEMORY, sizeof(*stream));
    if (!stream) {
        if (!CloseHandle(source)) trace_client_state("mod_memory_close_error", GetLastError());
        return 0;
    }
    stream->methods = &stream_methods;
    stream->refs = 1;
    stream->source = source;
    stream->size = size;
    stream->prefix_size = size;
    InitializeCriticalSection(&stream->lock);
    stream->prefix = (BYTE*)HeapAlloc(GetProcessHeap(), 0, size);
    if (!stream->prefix) { mod_stream_release(stream); return 0; }
    memcpy(stream->prefix, payload, size);
    *(void**)((BYTE*)database + 608) = stream;
    return 1;
}

int fang_mod_attach_environment(void* database, const wchar_t* path) {
    static const uint32_t types[5] = {0xea5118b0u, 0x00b1b104u, 0x2f4e681bu, 0x00b1b104u, 0x2f4e681bu};
    static const uint32_t groups[5] = {0x40450200u, 0x40606000u, 0, 0x40606000u, 0};
    static const uint32_t instances[5] = {0xbafc5ca1u, 0xdaf31958u, 0xdaf31958u, 0x6261312au, 0x6261312au};
    fang_environment_source sources[5] = {{0}};
    mod_stream source;
    LARGE_INTEGER source_size;
    BYTE header[96];
    BYTE* index = NULL;
    BYTE* package = NULL;
    BYTE* cursor;
    BYTE* end;
    uint32_t count, index_size, flags, shared[3] = {0, 0, 0};
    unsigned int ordinal, slot, package_size, found = 0;
    int is_attached = 0;
    memset(&source, 0, sizeof(source));
    source.source = CreateFileW(path, GENERIC_READ, FILE_SHARE_READ, NULL, OPEN_EXISTING,
        FILE_ATTRIBUTE_NORMAL | FILE_FLAG_RANDOM_ACCESS, NULL);
    if (source.source == INVALID_HANDLE_VALUE) return 0;
    if (!GetFileSizeEx(source.source, &source_size) ||
        !mod_source_read(&source, 0, header, sizeof(header))) goto finished;
    if (memcmp(header, "DBPF", 4) || mod_u32(header + 4) != 3 || mod_u32(header + 60) != 3)
        goto finished;
    count = mod_u32(header + 36);
    index_size = mod_u32(header + 44);
    if (count > 1000000 || index_size < 4 || index_size > 64 * 1024 * 1024 ||
        (uint64_t)mod_u32(header + 64) + index_size > (uint64_t)source_size.QuadPart) goto finished;
    index = (BYTE*)HeapAlloc(GetProcessHeap(), 0, index_size);
    if (!index || !mod_source_read(&source, mod_u32(header + 64), index, index_size)) goto finished;
    flags = mod_u32(index);
    if (flags & ~7u) goto finished;
    cursor = index + 4;
    end = index + index_size;
    for (slot = 0; slot < 3; ++slot) {
        if (!(flags & (1u << slot))) continue;
        if (end - cursor < 4) goto finished;
        shared[slot] = mod_u32(cursor);
        cursor += 4;
    }
    for (ordinal = 0; ordinal < count; ++ordinal) {
        uint32_t keys[3], offset, stored, decoded;
        unsigned int compression;
        for (slot = 0; slot < 3; ++slot) {
            keys[slot] = shared[slot];
            if (flags & (1u << slot)) continue;
            if (end - cursor < 4) goto finished;
            keys[slot] = mod_u32(cursor);
            cursor += 4;
        }
        if (end - cursor < 20) goto finished;
        for (slot = 0; slot < 5; ++slot)
            if (keys[0] == types[slot] && keys[1] == groups[slot] && !keys[2] &&
                mod_u32(cursor) == instances[slot]) break;
        if (slot == 5) {
            cursor += 20;
            continue;
        }
        offset = mod_u32(cursor + 4);
        stored = mod_u32(cursor + 8) & 0x7fffffffu;
        decoded = mod_u32(cursor + 12);
        compression = cursor[16] | ((unsigned int)cursor[17] << 8);
        if (!stored || stored > 8 * 1024 * 1024 ||
            (uint64_t)offset + stored > (uint64_t)source_size.QuadPart) goto finished;
        if (sources[slot].payload) goto finished;
        sources[slot].payload = (BYTE*)HeapAlloc(GetProcessHeap(), 0, stored);
        if (!sources[slot].payload ||
            !mod_source_read(&source, offset, sources[slot].payload, stored)) goto finished;
        sources[slot].stored = stored;
        sources[slot].decoded = decoded;
        sources[slot].compression = compression;
        ++found;
        cursor += 20;
    }
    if (cursor != end || found != 5) goto finished;
    package = fang_mod_environment_package(sources, &package_size);
    if (!package) goto finished;
    is_attached = fang_mod_attach_memory(database, path, package, package_size);
finished:
    if (package) HeapFree(GetProcessHeap(), 0, package);
    for (slot = 0; slot < 5; ++slot)
        if (sources[slot].payload) HeapFree(GetProcessHeap(), 0, sources[slot].payload);
    if (index) HeapFree(GetProcessHeap(), 0, index);
    if (!CloseHandle(source.source)) trace_client_state("mod_editor_environment_close_error", GetLastError());
    trace_client_state("mod_editor_environment_package_attached", is_attached);
    return is_attached;
}

static int mod_scene_floor(mod_stream* stream, uint32_t* position) {
    /* Source slots are GMSH, RW4, lighting. Validate and expand the render
     * model while retaining its complete authored material records. */
    BYTE* entry = stream->prefix + 104 + 28;
    unsigned int stored = mod_u32(entry + 16) & 0x7fffffffu;
    unsigned int decoded = mod_u32(entry + 20);
    unsigned int compression = entry[24] | ((unsigned int)entry[25] << 8);
    BYTE* source;
    BYTE* model;
    BYTE* prefix;
    unsigned int slot;
    unsigned int cursor;
    if (!stored || stored > 32 * 1024 * 1024 || decoded > 32 * 1024 * 1024) return 0;
    source = (BYTE*)HeapAlloc(GetProcessHeap(), 0, stored);
    if (!source) return 0;
    if (!mod_source_read(stream, stream->spans[1].source_offset, source, stored)) {
        HeapFree(GetProcessHeap(), 0, source);
        return 0;
    }
    model = fang_mod_scene_floor(source, stored, decoded, compression);
    HeapFree(GetProcessHeap(), 0, source);
    if (!model) return 0;
    prefix = (BYTE*)HeapAlloc(GetProcessHeap(), 0, 320 + decoded);
    if (!prefix) {
        HeapFree(GetProcessHeap(), 0, model);
        return 0;
    }
    memcpy(prefix, stream->prefix, 320);
    memcpy(prefix + 320, model, decoded);
    HeapFree(GetProcessHeap(), 0, model);
    entry = prefix + 104 + 28;
    mod_put32(entry + 12, 320);
    mod_put32(entry + 16, decoded | 0x80000000u);
    entry[24] = 0;
    entry[25] = 0;
    cursor = 320 + decoded;
    for (slot = 0; slot < 3; ++slot) {
        if (slot == 1) continue;
        mod_put32(prefix + 104 + slot * 28 + 12, cursor);
        stream->spans[slot].start = cursor;
        cursor += stream->spans[slot].size;
    }
    stream->spans[1] = stream->spans[2];
    stream->count = 2;
    HeapFree(GetProcessHeap(), 0, stream->prefix);
    stream->prefix = prefix;
    stream->prefix_size = 320 + decoded;
    *position = cursor;
    return 1;
}

/* A private four-resource view of the original editor background. The
 * header, index, aliased wrapper and expanded render model live in memory.
 * GMSH and lighting remain compressed in the read-only source and stream on demand.
 * Private identities prevent the editor experiment from replacing resources
 * with the same original identity in Arsenal or normal gameplay.
 */
int fang_mod_attach_scene(void* database, const wchar_t* path) {
    static const uint32_t types[] = {0x01c135da, 0x2f4e681b, 0x00b1b104};
    mod_stream* stream;
    BYTE header[96];
    BYTE* index = NULL;
    BYTE* cursor;
    BYTE* end;
    BYTE* property;
    LARGE_INTEGER source_size;
    uint32_t flags, shared[3] = {0, 0, 0};
    uint32_t count, index_size, index_offset, ordinal, slot, position = 320;
    unsigned int found = 0;
    HANDLE source;
    if (!readable_range((BYTE*)database + 608, sizeof(void*)) ||
        *(void**)((BYTE*)database + 608)) return 0;
    source = CreateFileW(path, GENERIC_READ, FILE_SHARE_READ, NULL, OPEN_EXISTING,
        FILE_ATTRIBUTE_NORMAL | FILE_FLAG_RANDOM_ACCESS, NULL);
    if (source == INVALID_HANDLE_VALUE) return 0;
    stream = (mod_stream*)HeapAlloc(GetProcessHeap(), HEAP_ZERO_MEMORY, sizeof(*stream));
    if (!stream) {
        if (!CloseHandle(source)) trace_client_state("mod_scene_close_error", GetLastError());
        return 0;
    }
    stream->methods = &stream_methods;
    stream->refs = 1;
    stream->source = source;
    InitializeCriticalSection(&stream->lock);
    if (!GetFileSizeEx(source, &source_size) ||
        !mod_source_read(stream, 0, header, sizeof(header))) goto failed;
    if (memcmp(header, "DBPF", 4) || mod_u32(header + 4) != 3 || mod_u32(header + 60) != 3)
        goto failed;
    count = mod_u32(header + 36);
    index_size = mod_u32(header + 44);
    index_offset = mod_u32(header + 64);
    if (count > 1000000 || index_size < 4 || index_size > 64 * 1024 * 1024 ||
        (uint64_t)index_offset + index_size > (uint64_t)source_size.QuadPart) goto failed;
    index = (BYTE*)HeapAlloc(GetProcessHeap(), 0, index_size);
    stream->prefix = (BYTE*)HeapAlloc(GetProcessHeap(), HEAP_ZERO_MEMORY, 320);
    stream->spans = (mod_resource_span*)HeapAlloc(GetProcessHeap(), HEAP_ZERO_MEMORY, 3 * sizeof(*stream->spans));
    if (!index || !stream->prefix || !stream->spans) goto failed;
    stream->prefix_size = 320;
    stream->count = 3;
    if (!mod_source_read(stream, index_offset, index, index_size)) goto failed;
    cursor = index + 4;
    end = index + index_size;
    flags = mod_u32(index);
    if (flags & ~7u) goto failed;
    for (slot = 0; slot < 3; ++slot) {
        if (!(flags & (1u << slot))) continue;
        if (end - cursor < 4) goto failed;
        shared[slot] = mod_u32(cursor);
        cursor += 4;
    }
    for (ordinal = 0; ordinal < count; ++ordinal) {
        uint32_t keys[3], low, offset, stored;
        BYTE* entry;
        for (slot = 0; slot < 3; ++slot) {
            keys[slot] = shared[slot];
            if (flags & (1u << slot)) continue;
            if (end - cursor < 4) goto failed;
            keys[slot] = mod_u32(cursor);
            cursor += 4;
        }
        if (end - cursor < 20) goto failed;
        low = mod_u32(cursor);
        offset = mod_u32(cursor + 4);
        stored = mod_u32(cursor + 8) & 0x7fffffffu;
        slot = 3;
        if (!keys[2] && keys[1] == 0x40606000 && low == 0x01dcaf31) {
            if (keys[0] == types[0]) slot = 0;
            if (keys[0] == types[1]) slot = 1;
        }
        if (!keys[2] && keys[1] == 0x40200100 && low == 0xf1fac055 && keys[0] == types[2]) slot = 2;
        if (!keys[2] && keys[1] == 0x40606000 && low == 0x01dcaf31 && keys[0] == 0x00b1b104) {
            if ((found & 8) || stored != 104 || mod_u32(cursor + 12) != 104 ||
                cursor[16] || cursor[17] || (uint64_t)offset + stored > (uint64_t)source_size.QuadPart ||
                !mod_source_read(stream, offset, stream->prefix + 216, 104)) goto failed;
            found |= 8;
        }
        if (slot < 3) {
            if ((found & (1u << slot)) || !stored ||
                (uint64_t)offset + stored > (uint64_t)source_size.QuadPart ||
                stored > 0x7fffffffu - position) goto failed;
            entry = stream->prefix + 104 + slot * 28;
            mod_put32(entry, keys[0]); mod_put32(entry + 4, keys[1]);
            mod_put32(entry + 8, slot == 2 ? FANG_EDITOR_SCENE_LIGHT : FANG_EDITOR_SCENE_MODEL);
            mod_put32(entry + 12, position); mod_put32(entry + 16, stored | 0x80000000u);
            memcpy(entry + 20, cursor + 12, 8);
            stream->spans[slot].source_offset = offset;
            stream->spans[slot].start = position;
            stream->spans[slot].size = stored;
            position += stored;
            found |= 1u << slot;
        }
        cursor += 20;
    }
    if (cursor != end || found != 15) goto failed;
    if (!mod_scene_floor(stream, &position)) {
        trace_client_state("mod_editor_scene_floor_adapt_failed", 1);
        /* The original source spans and prefix remain intact on failure.
         * An optional presentation adjustment must not remove scene access. */
        trace_client_state("mod_editor_scene_floor_source_fallback", 1);
    }
    /* Source order need not match resource-type order. The stream reader uses
     * a binary search over virtual payload offsets. */
    for (slot = 1; slot < stream->count; ++slot) {
        uint32_t previous = slot;
        while (previous && stream->spans[previous - 1].start > stream->spans[previous].start) {
            mod_resource_span span = stream->spans[previous - 1];
            stream->spans[previous - 1] = stream->spans[previous];
            stream->spans[previous--] = span;
        }
    }
    memcpy(stream->prefix, "DBPF", 4);
    mod_put32(stream->prefix + 4, 3); mod_put32(stream->prefix + 36, 4);
    mod_put32(stream->prefix + 44, 120); mod_put32(stream->prefix + 60, 3);
    mod_put32(stream->prefix + 64, 96); mod_put32(stream->prefix + 96, 4);
    cursor = stream->prefix + 188;
    mod_put32(cursor, 0x00b1b104); mod_put32(cursor + 4, 0x40606000);
    mod_put32(cursor + 8, FANG_EDITOR_SCENE_MODEL); mod_put32(cursor + 12, 216);
    mod_put32(cursor + 16, 104 | 0x80000000u); mod_put32(cursor + 20, 104);
    property = stream->prefix + 216;
    /* Retain source bounds/radius and parent. Only alias the final RW4 key;
     * reject a different wrapper layout instead of editing arbitrary bytes. */
    if (mod_u32(property + 88) != 0x01dcaf31 || mod_u32(property + 92) != 0x2f4e681b ||
        mod_u32(property + 96) != 0x40606000) goto failed;
    mod_put32(property + 88, FANG_EDITOR_SCENE_MODEL);
    stream->size = position;
    HeapFree(GetProcessHeap(), 0, index);
    *(void**)((BYTE*)database + 608) = stream;
    trace_client_state("mod_scene_stream_resources", 4);
    return 1;
failed:
    if (index) HeapFree(GetProcessHeap(), 0, index);
    trace_client_state("mod_scene_stream_failed", stream->error ? stream->error : ERROR_INVALID_DATA);
    mod_stream_release(stream);
    return 0;
}

static int mod_stream_index(mod_stream* stream, const BYTE* header, uint64_t source_size) {
    uint32_t count = mod_u32(header + 36), flags, shared[3] = {0, 0, 0};
    uint64_t index_size = mod_u64(header + 40), index_offset = mod_u64(header + 56);
    BYTE* index;
    BYTE* cursor;
    BYTE* end;
    uint32_t ordinal, position, prefix_size;
    int is_valid = 0;
    if (mod_u32(header + 4) != 2 || mod_u32(header + 52) != 3 ||
        count > 1000000 || index_size < 4 || index_size > 64 * 1024 * 1024 ||
        index_offset > source_size || index_size > source_size - index_offset) return 0;
    index = (BYTE*)HeapAlloc(GetProcessHeap(), 0, (SIZE_T)index_size);
    if (!index) { stream->error = ERROR_NOT_ENOUGH_MEMORY; return 0; }
    if (!mod_source_read(stream, index_offset, index, (DWORD)index_size)) goto finished;
    flags = mod_u32(index);
    if (flags & ~7u) goto finished;
    cursor = index + 4;
    end = index + (SIZE_T)index_size;
    for (position = 0; position < 3; ++position) {
        if (!(flags & (1u << position))) continue;
        if (end - cursor < 4) goto finished;
        shared[position] = mod_u32(cursor);
        cursor += 4;
    }
    prefix_size = 96 + 8 + count * 28;
    stream->prefix = (BYTE*)HeapAlloc(GetProcessHeap(), HEAP_ZERO_MEMORY, prefix_size);
    stream->spans = (mod_resource_span*)HeapAlloc(GetProcessHeap(), HEAP_ZERO_MEMORY,
        (SIZE_T)(count ? count : 1) * sizeof(mod_resource_span));
    if (!stream->prefix || !stream->spans) { stream->error = ERROR_NOT_ENOUGH_MEMORY; goto finished; }
    stream->count = count;
    stream->prefix_size = prefix_size;
    memcpy(stream->prefix, "DBPF", 4);
    mod_put32(stream->prefix + 4, 3);
    mod_put32(stream->prefix + 36, count);
    mod_put32(stream->prefix + 44, prefix_size - 96);
    mod_put32(stream->prefix + 60, 3);
    mod_put32(stream->prefix + 64, 96);
    mod_put32(stream->prefix + 96, 4); /* Native requires shared instance-high = 0. */
    position = prefix_size;
    for (ordinal = 0; ordinal < count; ++ordinal) {
        uint32_t keys[3], key, low, stored, decoded;
        uint64_t source_offset;
        BYTE* entry = stream->prefix + 104 + ordinal * 28;
        for (key = 0; key < 3; ++key) {
            keys[key] = shared[key];
            if (flags & (1u << key)) continue;
            if (end - cursor < 4) goto finished;
            keys[key] = mod_u32(cursor);
            cursor += 4;
        }
        if (keys[2] || end - cursor < 24) goto finished;
        low = mod_u32(cursor);
        source_offset = mod_u64(cursor + 4);
        stored = mod_u32(cursor + 12) & 0x7fffffffu;
        decoded = mod_u32(cursor + 16);
        if (source_offset > source_size || stored > source_size - source_offset ||
            stored > 0x7fffffffu - position) goto finished;
        mod_put32(entry, keys[0]);
        mod_put32(entry + 4, keys[1]);
        mod_put32(entry + 8, low);
        mod_put32(entry + 12, position);
        mod_put32(entry + 16, stored | 0x80000000u);
        mod_put32(entry + 20, decoded);
        memcpy(entry + 24, cursor + 20, 4);
        stream->spans[ordinal].source_offset = source_offset;
        stream->spans[ordinal].start = position;
        stream->spans[ordinal].size = stored;
        position += stored;
        cursor += 24;
    }
    if (cursor != end) goto finished;
    stream->size = position;
    is_valid = 1;
finished:
    HeapFree(GetProcessHeap(), 0, index);
    return is_valid;
}

/* Returns 0 for ordinary DBPF, 1 when attached, -1 on failure. The native
 * database owns the stream's sole reference after attachment and releases it
 * through its normal shutdown path (0xB19F10). Source handles deny writers so
 * offsets cannot change under a live resource request.
 */
int fang_mod_attach_dbbf(void* database, const wchar_t* path, unsigned int package_index) {
    mod_stream* stream;
    BYTE header[96];
    LARGE_INTEGER source_size;
    HANDLE source = CreateFileW(path, GENERIC_READ, FILE_SHARE_READ, NULL, OPEN_EXISTING,
        FILE_ATTRIBUTE_NORMAL | FILE_FLAG_RANDOM_ACCESS, NULL);
    if (source == INVALID_HANDLE_VALUE) {
        trace_client_state("mod_dbbf_open_error", GetLastError());
        return -1;
    }
    stream = (mod_stream*)HeapAlloc(GetProcessHeap(), HEAP_ZERO_MEMORY, sizeof(mod_stream));
    if (!stream) {
        if (!CloseHandle(source)) trace_client_state("mod_dbbf_close_error", GetLastError());
        return -1;
    }
    stream->methods = &stream_methods;
    stream->refs = 1;
    stream->source = source;
    stream->package_index = package_index;
    InitializeCriticalSection(&stream->lock);
    if (!mod_source_read(stream, 0, header, sizeof(header))) goto failed;
    if (memcmp(header, "DBBF", 4) != 0) {
        mod_stream_release(stream);
        return 0;
    }
    if (!GetFileSizeEx(source, &source_size)) {
        stream->error = GetLastError();
        goto failed;
    }
    if (!mod_stream_index(stream, header, (uint64_t)source_size.QuadPart)) goto failed;
    if (!readable_range((BYTE*)database + 608, sizeof(void*)) ||
        *(void**)((BYTE*)database + 608) != NULL) goto failed;
    *(void**)((BYTE*)database + 608) = stream;
    trace_client_state("mod_dbbf_stream_attached", package_index);
    trace_client_state("mod_dbbf_resource_count", stream->count);
    trace_client_state("mod_dbbf_index_bytes", stream->prefix_size);
    return 1;
failed:
    trace_client_state("mod_dbbf_attach_failed", package_index);
    trace_client_state("mod_dbbf_attach_error", stream->error ? stream->error : ERROR_INVALID_DATA);
    mod_stream_release(stream);
    return -1;
}

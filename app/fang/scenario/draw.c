#include "mod/mod.h"
#include "scenario/renderer.h"

#if FANG_SCENARIO
#include "hook.h"
#include <d3d9.h>
#include <string.h>
#include <stdio.h>
#include <wchar.h>
#include <stddef.h>

typedef HRESULT (WINAPI* scenario_draw_fn)(IDirect3DDevice9*, D3DPRIMITIVETYPE,
    INT, UINT, UINT, UINT, UINT);
typedef HRESULT (WINAPI* scenario_primitive_fn)(IDirect3DDevice9*, D3DPRIMITIVETYPE, UINT, UINT);
typedef HRESULT (WINAPI* scenario_primitive_up_fn)(IDirect3DDevice9*, D3DPRIMITIVETYPE, UINT, const void*, UINT);

typedef struct scenario_draw_record {
    UINT vertex_count, triangle_count, start_index;
    DWORD texture_mask, shader_mask, color_op, lighting, error_flags;
    DWORD pixel_program, vertex_program;
    UINT texture_width, texture_height;
    UINT light_width, light_height;
    DWORD render_states[12];
    DWORD sampler_u, sampler_v;
    float tint[4], levels[4], forward_tint[4];
} scenario_draw_record;

static scenario_draw_fn original_draw;
static scenario_primitive_fn original_primitive;
static scenario_primitive_up_fn original_primitive_up;
static unsigned int movie_draw_count;
static DWORD movie_draw_at;
static BYTE* draw_owner;
static DWORD draw_started;
static unsigned int draw_attempt_count;
static unsigned int draw_record_count;
static unsigned int draw_record_sequence;
static unsigned int draw_last_hint;
static scenario_draw_record draw_records[96];

static int is_mesh_declaration(IDirect3DDevice9* device) {
    IDirect3DVertexDeclaration9* declaration = NULL;
    D3DVERTEXELEMENT9 elements[MAXD3DDECLLENGTH + 1];
    UINT count = sizeof(elements) / sizeof(elements[0]);
    UINT index;
    HRESULT result = device->lpVtbl->GetVertexDeclaration(device, &declaration);
    int is_mesh = 0;
    if (FAILED(result) || !declaration) return 0;
    result = declaration->lpVtbl->GetDeclaration(declaration, elements, &count);
    if (SUCCEEDED(result) && count <= sizeof(elements) / sizeof(elements[0])) {
        for (index = 0; index < count; ++index)
            if (elements[index].Usage == D3DDECLUSAGE_NORMAL &&
                elements[index].Type != D3DDECLTYPE_UNUSED) is_mesh = 1;
    }
    declaration->lpVtbl->Release(declaration);
    return is_mesh;
}

static void observe_ground_declaration(IDirect3DDevice9* device) {
    IDirect3DVertexDeclaration9* declaration = NULL;
    D3DVERTEXELEMENT9 elements[MAXD3DDECLLENGTH + 1];
    UINT count = sizeof(elements) / sizeof(elements[0]);
    UINT index;
    HRESULT result = device->lpVtbl->GetVertexDeclaration(device, &declaration);
    if (!declaration) return;
    if (SUCCEEDED(result)) result = declaration->lpVtbl->GetDeclaration(declaration, elements, &count);
    if (SUCCEEDED(result) && count <= sizeof(elements) / sizeof(elements[0])) {
        for (index = 0; index < count; ++index) {
            trace_client_state("scenario_ground_element_offset", elements[index].Offset);
            trace_client_state("scenario_ground_element_type", elements[index].Type);
            trace_client_state("scenario_ground_element_usage", elements[index].Usage);
            trace_client_state("scenario_ground_element_index", elements[index].UsageIndex);
        }
    }
    declaration->lpVtbl->Release(declaration);
}

/* D3DX buffer getters are the standard COM methods after IUnknown. Load the
 * existing client disassembler dynamically; shader bytes stay in diagnostics. */
typedef struct scenario_shader_buffer { void** methods; } scenario_shader_buffer;
typedef HRESULT (WINAPI* scenario_disassemble_fn)(const DWORD*, BOOL, const char*, scenario_shader_buffer**);
typedef HRESULT (WINAPI* scenario_save_texture_fn)(const wchar_t*, int, IDirect3DBaseTexture9*, const void*);
typedef HRESULT (WINAPI* scenario_save_surface_fn)(const wchar_t*, int, IDirect3DSurface9*, const void*, const RECT*);

static void observe_draw_target(IDirect3DDevice9* device, unsigned int ordinal) {
    IDirect3DSurface9* target = NULL;
    IDirect3DSurface9* back = NULL;
    D3DSURFACE_DESC description;
    HMODULE module = GetModuleHandleW(L"d3dx9_27.dll");
    scenario_save_surface_fn save;
    wchar_t path[32768];
    wchar_t* slash;
    DWORD length;
    HRESULT result = device->lpVtbl->GetRenderTarget(device, 0, &target);
    if (!target) return;
    if (FAILED(result)) goto done;
    result = target->lpVtbl->GetDesc(target, &description);
    if (SUCCEEDED(result)) {
        trace_client_state("scenario_ground_target_format", description.Format);
        trace_client_state("scenario_ground_target_width", description.Width);
        trace_client_state("scenario_ground_target_height", description.Height);
    }
    result = device->lpVtbl->GetBackBuffer(device, 0, 0, D3DBACKBUFFER_TYPE_MONO, &back);
    if (SUCCEEDED(result)) trace_client_state("scenario_ground_target_is_backbuffer", target == back);
    if (!module) goto done;
    save = (scenario_save_surface_fn)(void*)GetProcAddress(module, "D3DXSaveSurfaceToFileW");
    if (!save) goto done;
    length = GetEnvironmentVariableW(L"DARKSPIN_CLIENT_TRACE", path, sizeof(path) / sizeof(path[0]));
    if (!length || length >= sizeof(path) / sizeof(path[0])) goto done;
    slash = wcsrchr(path, L'\\');
    if (!slash || _snwprintf(slash + 1, sizeof(path) / sizeof(path[0]) - (slash + 1 - path),
        L"editor-ground-%u-target.png", ordinal) < 0) goto done;
    if (GetFileAttributesW(path) != INVALID_FILE_ATTRIBUTES) goto done;
    result = save(path, 3, target, NULL, NULL);
    if (FAILED(result)) trace_client_state("scenario_ground_target_save_failed", (unsigned int)result);
done:
    if (back) back->lpVtbl->Release(back);
    target->lpVtbl->Release(target);
}

static void save_draw_texture(IDirect3DDevice9* device, unsigned int ordinal, DWORD stage, const wchar_t* label) {
    IDirect3DBaseTexture9* texture = NULL;
    HMODULE module = GetModuleHandleW(L"d3dx9_27.dll");
    scenario_save_texture_fn save;
    wchar_t path[32768];
    wchar_t* slash;
    DWORD length;
    HRESULT result;
    if (!module) return;
    save = (scenario_save_texture_fn)(void*)GetProcAddress(module, "D3DXSaveTextureToFileW");
    if (!save) return;
    length = GetEnvironmentVariableW(L"DARKSPIN_CLIENT_TRACE", path, sizeof(path) / sizeof(path[0]));
    if (!length || length >= sizeof(path) / sizeof(path[0])) return;
    slash = wcsrchr(path, L'\\');
    if (!slash || _snwprintf(slash + 1, sizeof(path) / sizeof(path[0]) - (slash + 1 - path),
        L"%ls-%u-stage-%lu.png", label, ordinal, stage) < 0) return;
    if (GetFileAttributesW(path) != INVALID_FILE_ATTRIBUTES) return;
    result = device->lpVtbl->GetTexture(device, stage, &texture);
    if (FAILED(result) || !texture) return;
    result = save(path, 3, texture, NULL); /* D3DXIFF_PNG, diagnostics only. */
    if (FAILED(result)) trace_client_state("scenario_editor_texture_save_failed", (unsigned int)result);
    texture->lpVtbl->Release(texture);
}

static void save_draw_shader(IDirect3DDevice9* device, unsigned int hint, unsigned int ordinal, int is_pixel) {
    IDirect3DPixelShader9* pixel = NULL;
    IDirect3DVertexShader9* vertex = NULL;
    scenario_shader_buffer* assembly = NULL;
    scenario_disassemble_fn disassemble;
    HMODULE module = GetModuleHandleW(L"d3dx9_27.dll");
    BYTE* code = NULL;
    UINT size = 0;
    HRESULT result;
    wchar_t path[32768];
    DWORD path_size, written;
    wchar_t* slash;
    HANDLE output;
    if (!module) return;
    disassemble = (scenario_disassemble_fn)(void*)GetProcAddress(module, "D3DXDisassembleShader");
    if (!disassemble) return;
    path_size = GetEnvironmentVariableW(L"DARKSPIN_CLIENT_TRACE", path, sizeof(path) / sizeof(path[0]));
    if (!path_size || path_size >= sizeof(path) / sizeof(path[0])) return;
    slash = wcsrchr(path, L'\\');
    if (!slash) return;
    if (_snwprintf(slash + 1, sizeof(path) / sizeof(path[0]) - (slash + 1 - path),
        L"editor-draw-%u-%u-%ls.txt", hint, ordinal, is_pixel ? L"pixel" : L"vertex") < 0) return;
    if (is_pixel) {
        result = device->lpVtbl->GetPixelShader(device, &pixel);
        if (FAILED(result) || !pixel) goto done;
        result = pixel->lpVtbl->GetFunction(pixel, NULL, &size);
    } else {
        result = device->lpVtbl->GetVertexShader(device, &vertex);
        if (FAILED(result) || !vertex) goto done;
        result = vertex->lpVtbl->GetFunction(vertex, NULL, &size);
    }
    if (FAILED(result) || size < 4 || size > 256 * 1024) goto done;
    code = (BYTE*)HeapAlloc(GetProcessHeap(), 0, size);
    if (!code) goto done;
    result = is_pixel ? pixel->lpVtbl->GetFunction(pixel, code, &size) :
        vertex->lpVtbl->GetFunction(vertex, code, &size);
    if (FAILED(result)) goto done;
    result = disassemble((const DWORD*)code, FALSE, NULL, &assembly);
    if (FAILED(result) || !assembly) goto done;
    output = CreateFileW(path, GENERIC_WRITE, FILE_SHARE_READ, NULL, CREATE_NEW, FILE_ATTRIBUTE_NORMAL, NULL);
    if (output != INVALID_HANDLE_VALUE) {
        const void* data = ((const void* (WINAPI*)(scenario_shader_buffer*))assembly->methods[3])(assembly);
        DWORD length = ((DWORD (WINAPI*)(scenario_shader_buffer*))assembly->methods[4])(assembly);
        if (!data || !length || length > 1024 * 1024 ||
            !WriteFile(output, data, length, &written, NULL) || written != length)
            trace_client_state("scenario_editor_shader_write_failed", hint);
        if (!CloseHandle(output)) trace_client_state("scenario_editor_shader_close_failed", hint);
    } else if (GetLastError() != ERROR_FILE_EXISTS) {
        trace_client_state("scenario_editor_shader_open_failed", hint);
    }
done:
    if (assembly) ((ULONG (WINAPI*)(scenario_shader_buffer*))assembly->methods[2])(assembly);
    if (code) HeapFree(GetProcessHeap(), 0, code);
    if (pixel) pixel->lpVtbl->Release(pixel);
    if (vertex) vertex->lpVtbl->Release(vertex);
}

static void observe_draw_state(IDirect3DDevice9* device, scenario_draw_record* record) {
    static const D3DRENDERSTATETYPE states[12] = {D3DRS_ZENABLE, D3DRS_ZWRITEENABLE,
        D3DRS_ZFUNC, D3DRS_CULLMODE, D3DRS_STENCILENABLE, D3DRS_STENCILFUNC,
        D3DRS_STENCILREF, D3DRS_COLORWRITEENABLE, D3DRS_ALPHATESTENABLE,
        D3DRS_ALPHAREF, D3DRS_ALPHAFUNC, D3DRS_ALPHABLENDENABLE};
    IDirect3DPixelShader9* pixel = NULL;
    IDirect3DVertexShader9* vertex = NULL;
    unsigned int stage;
    HRESULT result;
    result = device->lpVtbl->GetPixelShader(device, &pixel);
    if (FAILED(result)) record->error_flags |= 1;
    if (pixel) {
        record->shader_mask |= 1;
        record->pixel_program = (DWORD)(uintptr_t)pixel;
        pixel->lpVtbl->Release(pixel);
    }
    result = device->lpVtbl->GetVertexShader(device, &vertex);
    if (FAILED(result)) record->error_flags |= 2;
    if (vertex) {
        record->shader_mask |= 2;
        record->vertex_program = (DWORD)(uintptr_t)vertex;
        vertex->lpVtbl->Release(vertex);
    }
    for (stage = 0; stage < 13; ++stage) {
        IDirect3DBaseTexture9* texture = NULL;
        result = device->lpVtbl->GetTexture(device, stage, &texture);
        if (FAILED(result)) record->error_flags |= 4u << stage;
        if (!texture) continue;
        record->texture_mask |= 1u << stage;
        if ((stage == 0 || stage == 12) && texture->lpVtbl->GetType(texture) == D3DRTYPE_TEXTURE) {
            D3DSURFACE_DESC description;
            IDirect3DTexture9* texture2d = (IDirect3DTexture9*)texture;
            result = texture2d->lpVtbl->GetLevelDesc(texture2d, 0, &description);
            if (SUCCEEDED(result)) {
                if (!stage) {
                    record->texture_width = description.Width;
                    record->texture_height = description.Height;
                } else {
                    record->light_width = description.Width;
                    record->light_height = description.Height;
                }
            } else record->error_flags |= 64;
        }
        texture->lpVtbl->Release(texture);
    }
    result = device->lpVtbl->GetTextureStageState(device, 0, D3DTSS_COLOROP, &record->color_op);
    if (FAILED(result)) record->error_flags |= 128;
    result = device->lpVtbl->GetRenderState(device, D3DRS_LIGHTING, &record->lighting);
    if (FAILED(result)) record->error_flags |= 256;
    result = device->lpVtbl->GetVertexShaderConstantF(device, 0, record->tint, 1);
    if (FAILED(result)) record->error_flags |= 0x10000;
    result = device->lpVtbl->GetVertexShaderConstantF(device, 11, record->levels, 1);
    if (FAILED(result)) record->error_flags |= 0x20000;
    result = device->lpVtbl->GetVertexShaderConstantF(device, 7, record->forward_tint, 1);
    if (FAILED(result)) record->error_flags |= 0x40000;
    for (stage = 0; stage < 12; ++stage) {
        result = device->lpVtbl->GetRenderState(device, states[stage], record->render_states + stage);
        if (FAILED(result)) record->error_flags |= 0x80000;
    }
    result = device->lpVtbl->GetSamplerState(device, 0, D3DSAMP_ADDRESSU, &record->sampler_u);
    if (FAILED(result)) record->error_flags |= 0x100000;
    result = device->lpVtbl->GetSamplerState(device, 0, D3DSAMP_ADDRESSV, &record->sampler_v);
    if (FAILED(result)) record->error_flags |= 0x200000;
}

static void record_editor_draw(IDirect3DDevice9* device, D3DPRIMITIVETYPE type,
    UINT vertex_count, UINT start_index, UINT primitive_count) {
    BYTE* editor = fang_mod_editor_active();
    scenario_draw_record record;
    unsigned int index;
    unsigned int hint = 0;
    if (!fang_mod_editor_is_scene(editor)) {
        draw_owner = NULL;
        return;
    }
    if (draw_owner != editor) {
        draw_owner = editor;
        draw_started = GetTickCount();
        draw_attempt_count = 0;
        draw_record_count = 0;
    }
    /* Observe a bounded normal-render window after initial scene loading.
     * No replay, render-state writes, alternate drawing or texture retention. */
    if ((DWORD)(GetTickCount() - draw_started) < 2000 || draw_attempt_count >= 256 ||
        draw_record_count >= sizeof(draw_records) / sizeof(draw_records[0])) return;
    if (type != D3DPT_TRIANGLELIST) return;
    /* Counts from installed RW4 metadata are hints, not object ownership.
     * Ignore UI batches so loading overlays cannot exhaust the capture before
     * the environment's first completed frame. */
    if (vertex_count == 1920 && primitive_count == 1440) hint = 1;
    if (vertex_count == 1091 && primitive_count == 1620) hint = 2;
    if (vertex_count == 1613 && primitive_count == 920) hint = 3;
    if (vertex_count == 642 && primitive_count == 958) hint = 4;
    if (!hint && !is_mesh_declaration(device)) return;
    ++draw_attempt_count;
    memset(&record, 0, sizeof(record));
    record.vertex_count = vertex_count;
    record.triangle_count = primitive_count;
    record.start_index = start_index;
    observe_draw_state(device, &record);
    for (index = 0; index < draw_record_count; ++index)
        if (!memcmp(draw_records + index, &record, offsetof(scenario_draw_record, tint))) return;
    draw_records[draw_record_count++] = record;
    ++draw_record_sequence;
    draw_last_hint = hint;
    save_draw_shader(device, hint, draw_record_count, 0);
    save_draw_shader(device, hint, draw_record_count, 1);
    if (hint == 2) {
        observe_ground_declaration(device);
        save_draw_texture(device, draw_record_count, 0, L"editor-ground");
        save_draw_texture(device, draw_record_count, 12, L"editor-ground");
    }
    trace_client_state("scenario_editor_draw_record", draw_record_count);
    trace_client_state("scenario_editor_draw_vertices", vertex_count);
    trace_client_state("scenario_editor_draw_triangles", primitive_count);
    trace_client_state("scenario_editor_draw_start_index", start_index);
    trace_client_state("scenario_editor_draw_mesh_hint", hint);
    trace_client_state("scenario_editor_draw_texture_mask", record.texture_mask);
    trace_client_state("scenario_editor_draw_shader_mask", record.shader_mask);
    trace_client_state("scenario_editor_draw_pixel_program", record.pixel_program);
    trace_client_state("scenario_editor_draw_vertex_program", record.vertex_program);
    trace_client_state("scenario_editor_draw_texture_width", record.texture_width);
    trace_client_state("scenario_editor_draw_texture_height", record.texture_height);
    trace_client_state("scenario_editor_draw_light_width", record.light_width);
    trace_client_state("scenario_editor_draw_light_height", record.light_height);
    for (index = 0; index < 4; ++index) {
        DWORD bits;
        memcpy(&bits, record.tint + index, sizeof(bits));
        trace_client_state("scenario_editor_draw_tint_bits", bits);
        memcpy(&bits, record.levels + index, sizeof(bits));
        trace_client_state("scenario_editor_draw_level_bits", bits);
        memcpy(&bits, record.forward_tint + index, sizeof(bits));
        trace_client_state("scenario_editor_draw_forward_tint_bits", bits);
    }
    trace_client_state("scenario_editor_draw_color_op", record.color_op);
    trace_client_state("scenario_editor_draw_lighting", record.lighting);
    trace_client_state("scenario_editor_draw_error_flags", record.error_flags);
    trace_client_state("scenario_editor_draw_sampler_u", record.sampler_u);
    trace_client_state("scenario_editor_draw_sampler_v", record.sampler_v);
    for (index = 0; index < 12; ++index)
        trace_client_state("scenario_editor_draw_render_state", record.render_states[index]);
}

static void record_movie_draw(IDirect3DDevice9* device, UINT primitive_count) {
    scenario_draw_record record;
    D3DVIEWPORT9 viewport;
    RECT scissor;
    DWORD enabled;
    HRESULT result;
    unsigned int index;
    if (primitive_count > 4 || !is_scenario_movie_observed() || movie_draw_count >= 8 ||
        (movie_draw_at && (DWORD)(GetTickCount() - movie_draw_at) < 750)) return;
    memset(&record, 0, sizeof(record));
    observe_draw_state(device, &record);
    /* Ignore cursor/font atlas draws; retain large movie-like sampled textures. */
    if (record.texture_width < 320 || record.texture_height < 180) return;
    movie_draw_at = GetTickCount();
    ++movie_draw_count;
    trace_client_state("scenario_movie_gpu_record", movie_draw_count);
    trace_client_state("scenario_movie_gpu_texture_width", record.texture_width);
    trace_client_state("scenario_movie_gpu_texture_height", record.texture_height);
    trace_client_state("scenario_movie_gpu_texture_mask", record.texture_mask);
    trace_client_state("scenario_movie_gpu_pixel_program", record.pixel_program);
    for (index = 0; index < 12; ++index)
        trace_client_state("scenario_movie_gpu_render_state", record.render_states[index]);
    result = device->lpVtbl->GetViewport(device, &viewport);
    if (SUCCEEDED(result)) {
        trace_client_state("scenario_movie_gpu_viewport_width", viewport.Width);
        trace_client_state("scenario_movie_gpu_viewport_height", viewport.Height);
    }
    result = device->lpVtbl->GetRenderState(device, D3DRS_SCISSORTESTENABLE, &enabled);
    if (SUCCEEDED(result)) trace_client_state("scenario_movie_gpu_scissor_enabled", enabled);
    result = device->lpVtbl->GetScissorRect(device, &scissor);
    if (SUCCEEDED(result)) {
        trace_client_state("scenario_movie_gpu_scissor_width", scissor.right - scissor.left);
        trace_client_state("scenario_movie_gpu_scissor_height", scissor.bottom - scissor.top);
    }
    for (index = 0; index < 3; ++index)
        save_draw_texture(device, movie_draw_count, index, L"movie");
    save_draw_shader(device, 124, movie_draw_count, 0);
    save_draw_shader(device, 124, movie_draw_count, 1);
}

static HRESULT WINAPI observe_primitive(IDirect3DDevice9* device, D3DPRIMITIVETYPE type,
    UINT start, UINT count) {
    DWORD last_error = GetLastError();
    record_movie_draw(device, count);
    SetLastError(last_error);
    return original_primitive(device, type, start, count);
}

static HRESULT WINAPI observe_primitive_up(IDirect3DDevice9* device, D3DPRIMITIVETYPE type,
    UINT count, const void* vertices, UINT stride) {
    DWORD last_error = GetLastError();
    record_movie_draw(device, count);
    SetLastError(last_error);
    return original_primitive_up(device, type, count, vertices, stride);
}

static HRESULT WINAPI observe_draw(IDirect3DDevice9* device, D3DPRIMITIVETYPE type,
    INT base_vertex, UINT minimum_vertex, UINT vertex_count, UINT start_index, UINT primitive_count) {
    DWORD last_error = GetLastError();
    unsigned int previous_sequence = draw_record_sequence;
    HRESULT result;
    record_movie_draw(device, primitive_count);
    record_editor_draw(device, type, vertex_count, start_index, primitive_count);
    SetLastError(last_error);
    result = original_draw(device, type, base_vertex, minimum_vertex, vertex_count, start_index, primitive_count);
    last_error = GetLastError();
    if (draw_record_sequence != previous_sequence) {
        trace_client_state("scenario_editor_draw_result", (unsigned int)result);
        if (draw_last_hint == 2) observe_draw_target(device, draw_record_count);
    }
    SetLastError(last_error);
    return result;
}

void observe_scenario_draw_device(void* pointer) {
    IDirect3DDevice9* device = (IDirect3DDevice9*)pointer;
    scenario_draw_fn draw;
    if (!device || !readable_range(device, sizeof(void*)) ||
        !readable_range(device->lpVtbl, sizeof(*device->lpVtbl))) return;
    draw = device->lpVtbl->DrawIndexedPrimitive;
    if (draw == observe_draw) return;
    if (original_draw && original_draw != draw) {
        trace_client_state("scenario_editor_draw_device_unsupported", 1);
        return;
    }
    original_draw = draw;
    original_primitive = device->lpVtbl->DrawPrimitive;
    original_primitive_up = device->lpVtbl->DrawPrimitiveUP;
    trace_client_state("scenario_movie_primitive_hook", patch_pointer(
        (void**)&device->lpVtbl->DrawPrimitive, (void*)original_primitive, (void*)observe_primitive));
    trace_client_state("scenario_movie_primitive_up_hook", patch_pointer(
        (void**)&device->lpVtbl->DrawPrimitiveUP, (void*)original_primitive_up, (void*)observe_primitive_up));
    trace_client_state("scenario_editor_draw_hook", patch_pointer(
        (void**)&device->lpVtbl->DrawIndexedPrimitive, (void*)draw, (void*)observe_draw));
}
#endif

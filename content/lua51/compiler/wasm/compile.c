/* Compile source without opening libraries or executing the loaded function. */
/* Attribution: root ATTRIBUTION.txt, section 1 (Lua 5.1 and WASI runtime). */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "lua.h"
#include "lauxlib.h"

#define MAX_SOURCE_SIZE (16u * 1024u * 1024u)

/* The guest is disposable: terminate on a compile error instead of longjmp. */
void darkspin_compile_error(void *opaque_state) {
    lua_State *state = (lua_State *)opaque_state;
    const char *diagnostic = lua_tostring(state, -1);
    fprintf(stderr, "%s\n", diagnostic == NULL ? "source compilation failed" : diagnostic);
    exit(1);
}

static int write_chunk(lua_State *state, const void *payload, size_t size, void *destination) {
    (void)state;
    return fwrite(payload, 1, size, (FILE *)destination) != size;
}

int main(int argc, char **argv) {
    if (argc != 2 || strlen(argv[1]) > 4096) {
        fputs("invalid source name\n", stderr);
        return 1;
    }
    char *source = (char *)malloc(MAX_SOURCE_SIZE + 1);
    if (source == NULL) {
        fputs("source allocation failed\n", stderr);
        return 1;
    }
    size_t size = fread(source, 1, MAX_SOURCE_SIZE + 1, stdin);
    if (ferror(stdin) || size > MAX_SOURCE_SIZE) {
        free(source);
        fputs("source read failed or exceeds 16 MiB\n", stderr);
        return 1;
    }
    if (size > 0 && (unsigned char)source[0] == 0x1b) {
        free(source);
        fputs("expected Lua source, not a binary chunk\n", stderr);
        return 1;
    }
    lua_State *state = luaL_newstate();
    if (state == NULL) {
        free(source);
        fputs("compiler allocation failed\n", stderr);
        return 1;
    }
    int status = luaL_loadbuffer(state, source, size, argv[1]);
    free(source);
    if (status != 0) {
        const char *diagnostic = lua_tostring(state, -1);
        fprintf(stderr, "%s\n", diagnostic == NULL ? "source compilation failed" : diagnostic);
        lua_close(state);
        return 1;
    }
    status = lua_dump(state, write_chunk, stdout);
    lua_close(state);
    if (status != 0 || fflush(stdout) != 0) {
        fputs("chunk serialization failed\n", stderr);
        return 1;
    }
    return 0;
}

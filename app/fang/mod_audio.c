#include "hook.h"

#include <stdint.h>
#include <string.h>

/* The bound-rig adapter marks recognized vocal cues before native dispatch.
 * Only those feature-owned markers select a complete Spore voice event here;
 * other voices, footsteps and native selection retain their original path.
 * Native timing, sound handles, spatialization and interruption remain owned
 * by 0x99C480 -> 0xC75780. Spore banks supply the selected event/sample data.
 */
typedef unsigned char (FANG_THISCALL* mod_audio_select_fn)(void*, void*, int, char*, int);
typedef unsigned char (FANG_THISCALL* mod_audio_context_fn)(void*, uint32_t, uint32_t*);

static mod_audio_select_fn native_select;
static mod_audio_context_fn native_audio_context;
static volatile LONG audio_trace_count;

static uint32_t mod_audio_event_key(const char* event, int capacity) {
    uint32_t hash = 0x811c9dc5;
    int index;
    if (capacity <= 0 || capacity > 200 || !readable_range(event, (size_t)capacity)) return 0;
    for (index = 0; index < capacity; ++index) {
        unsigned char letter = (unsigned char)event[index];
        if (!letter) return hash;
        if (letter >= 'A' && letter <= 'Z') letter += 'a' - 'A';
        hash = hash * 0x01000193 ^ letter;
    }
    return 0;
}

static unsigned char FANG_THISCALL mod_select_audio(void* selector, void* context,
    int flags, char* event, int capacity) {
    uint32_t before = mod_audio_event_key(event, capacity);
    int is_creature_event = before && (event[0] == 'v' ||
        (capacity >= 4 && event[0] == 'f' && event[1] == 'o' && event[2] == 'o' && event[3] == 't'));
    unsigned char result;
    if (fang_mod_select_voice(event, capacity)) result = 1;
    else result = native_select(selector, context, flags, event, capacity);
    if (is_creature_event && InterlockedIncrement(&audio_trace_count) <= 64) {
        uint32_t part_type = 0;
        uint32_t override_type = 0;
        unsigned char has_part = native_audio_context(context, 60792321, &part_type);
        unsigned char has_override = native_audio_context(context, 60796929, &override_type);
        trace_client_state("mod_audio_part_mouth", has_part ? part_type : 0);
        trace_client_state("mod_audio_override_mouth", has_override ? override_type : 0);
        trace_client_state("mod_audio_event_before", before);
        trace_client_state("mod_audio_event_selected", mod_audio_event_key(event, capacity));
    }
    return result;
}

void fang_mod_install_audio(BYTE* base) {
    /* 0xC75835 calls native animation selection with ECX and four arguments.
     * 0xC5D7E0 is the native context's integer lookup, ECX plus two arguments.
     * Require exact build-103 function targets before installing selection.
     */
    static const BYTE context_signature[] = {0x51, 0x8d, 0x04, 0x24, 0x50, 0x8b, 0x44, 0x24};
    if (!readable_range(base + 0x85d7e0, sizeof(context_signature)) ||
        memcmp(base + 0x85d7e0, context_signature, sizeof(context_signature)) != 0) {
        trace_client_state("mod_audio_observer_unsupported", 1);
        return;
    }
    native_select = (mod_audio_select_fn)(base + 0x8754a0);
    native_audio_context = (mod_audio_context_fn)(base + 0x85d7e0);
    if (!patch_call(base + 0x875835, native_select, mod_select_audio)) {
        trace_client_state("mod_audio_observer_failed", 1);
        return;
    }
    trace_client_state("mod_audio_observer", 1);
    fang_mod_install_voice(base);
}

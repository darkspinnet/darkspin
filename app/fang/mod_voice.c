#include "hook.h"

#include <math.h>
#include <stdint.h>
#include <string.h>

typedef struct {
    const char* marker;
    const char* event;
} mod_voice_event;

#define VOICE(event) {"vfang_mod:" event, event}
/* Complete event names recovered from the pinned Spore audio registry. Keep
 * strings static: the native context may outlive the animation call's stack.
 * Each row follows jh/jc/po/bc/bh/ro/sh/mc/ao/fc/bir/mam/ins mouth families.
 */
static const mod_voice_event voice_events[MOD_VOICE_COUNT][13] = {
    {
        VOICE("vcsa_dance_genjhxcr"),
        VOICE("vcsa_dance_genjcxcr"),
        VOICE("vcsa_dance_genpoxcr"),
        VOICE("vcsa_dance_genbcxcr"),
        VOICE("vcsa_dance_genbhxcr"),
        VOICE("vcsa_dance_genroxcr"),
        VOICE("vcsa_dance_genshxcr"),
        VOICE("vcsa_dance_genmcxcr"),
        VOICE("vcsa_dance_genaoxcr"),
        VOICE("vcsa_dance_genfcxcr"),
        VOICE("vcsa_dance_genbirain"),
        VOICE("vcsa_dance_genmamain"),
        VOICE("vcsa_dance_geninsain")
    },
    {
        VOICE("vcsa_actn_battleroarjhxcr"),
        VOICE("vcsa_actn_battleroarjcxcr"),
        VOICE("vcsa_actn_battleroarpoxcr"),
        VOICE("vcsa_actn_battleroarbcxcr"),
        VOICE("vcsa_actn_battleroarbhxcr"),
        VOICE("vcsa_actn_battleroarroxcr"),
        VOICE("vcsa_actn_battleroarshxcr"),
        VOICE("vcsa_actn_battleroarmcxcr"),
        VOICE("vcsa_actn_battleroaraoxcr"),
        VOICE("vcsa_actn_battleroarfcxcr"),
        VOICE("vcsa_actn_battleroarbirain"),
        VOICE("vcsa_actn_battleroarmamain"),
        VOICE("vcsa_actn_battleroarinsain")
    },
    {
        VOICE("vcom_bitejhxcr"),
        VOICE("vcom_bitejcxcr"),
        VOICE("vcom_bitepoxcr"),
        VOICE("vcom_bitebcxcr"),
        VOICE("vcom_bitebhxcr"),
        VOICE("vcom_biteroxcr"),
        VOICE("vcom_biteshxcr"),
        VOICE("vcom_bitemcxcr"),
        VOICE("vcom_biteaoxcr"),
        VOICE("vcom_bitefcxcr"),
        VOICE("vcom_bitebirain"),
        VOICE("vcom_bitemamain"),
        VOICE("vcom_biteinsain")
    },
    {
        VOICE("vgen_startledjhxcr"),
        VOICE("vgen_startledjcxcr"),
        VOICE("vgen_startledpoxcr"),
        VOICE("vgen_startledbcxcr"),
        VOICE("vgen_startledbhxcr"),
        VOICE("vgen_startledroxcr"),
        VOICE("vgen_startledshxcr"),
        VOICE("vgen_startledmcxcr"),
        VOICE("vgen_startledaoxcr"),
        VOICE("vgen_startledfcxcr"),
        VOICE("vcom_att_gethit_club2birain"),
        VOICE("vgen_startled_landmamain"),
        VOICE("vgen_startled_landinsain")
    },
    {
        VOICE("vgen_death_downjhxcr"),
        VOICE("vgen_death_downjcxcr"),
        VOICE("vgen_death_downpoxcr"),
        VOICE("vgen_death_downbcxcr"),
        VOICE("vgen_death_downbhxcr"),
        VOICE("vgen_death_downroxcr"),
        VOICE("vgen_death_downshxcr"),
        VOICE("vgen_death_downmcxcr"),
        VOICE("vgen_death_downaoxcr"),
        VOICE("vgen_death_downfcxcr"),
        VOICE("vgen_boulder_diebirain"),
        VOICE("vgen_death_sidemamain"),
        VOICE("vgen_boulder_dieinsain")
    },
    {
        VOICE("vidle_gen_lookdownjhxcr"),
        VOICE("vidle_gen_lookdownjcxcr"),
        VOICE("vidle_gen_lookdownpoxcr"),
        VOICE("vidle_gen_lookdownbcxcr"),
        VOICE("vidle_gen_lookdownbhxcr"),
        VOICE("vidle_gen_lookdownroxcr"),
        VOICE("vidle_gen_lookdownshxcr"),
        VOICE("vidle_gen_lookdownmcxcr"),
        VOICE("vidle_gen_lookdownaoxcr"),
        VOICE("vidle_gen_lookdownfcxcr"),
        VOICE("vidle_gen_lookdownbirain"),
        VOICE("vidle_gen_lookdownmamain"),
        VOICE("vidle_gen_lookdowninsain")
    },
    {
        VOICE("vcsa_emo_scaredjhxcr"),
        VOICE("vcsa_emo_scaredjcxcr"),
        VOICE("vcsa_emo_scaredpoxcr"),
        VOICE("vcsa_emo_scaredbcxcr"),
        VOICE("vcsa_emo_scaredbhxcr"),
        VOICE("vcsa_emo_scaredroxcr"),
        VOICE("vcsa_emo_scaredshxcr"),
        VOICE("vcsa_emo_scaredmcxcr"),
        VOICE("vcsa_emo_scaredaoxcr"),
        VOICE("vcsa_emo_scaredfcxcr"),
        VOICE("vcsa_emo_scaredbirain"),
        VOICE("vcsa_emo_scaredmamain"),
        VOICE("vcsa_emo_scaredinsain")
    },
    {
        VOICE("vcsa_emo_laughjhxcr"),
        VOICE("vcsa_emo_laughjcxcr"),
        VOICE("vcsa_emo_laughpoxcr"),
        VOICE("vcsa_emo_laughbcxcr"),
        VOICE("vcsa_emo_laughbhxcr"),
        VOICE("vcsa_emo_laughroxcr"),
        VOICE("vcsa_emo_laughshxcr"),
        VOICE("vcsa_emo_laughmcxcr"),
        VOICE("vcsa_emo_laughaoxcr"),
        VOICE("vcsa_emo_laughfcxcr"),
        VOICE("vcsa_emo_laughbirain"),
        VOICE("vcsa_emo_laughmamain"),
        VOICE("vcsa_emo_laughinsain")
    },
    {
        VOICE("vcsa_emo_happyjhxcr"),
        VOICE("vcsa_emo_happyjcxcr"),
        VOICE("vcsa_emo_happypoxcr"),
        VOICE("vcsa_emo_happybcxcr"),
        VOICE("vcsa_emo_happybhxcr"),
        VOICE("vcsa_emo_happyroxcr"),
        VOICE("vcsa_emo_happyshxcr"),
        VOICE("vcsa_emo_happymcxcr"),
        VOICE("vcsa_emo_happyaoxcr"),
        VOICE("vcsa_emo_happyfcxcr"),
        VOICE("vcsa_emo_happybirain"),
        VOICE("vcsa_emo_happymamain"),
        VOICE("vcsa_emo_happyinsain")
    },
    {
        VOICE("vcsa_actn_jumphitjhxcr"),
        VOICE("vcsa_actn_jumphitjcxcr"),
        VOICE("vcsa_actn_jumphitpoxcr"),
        VOICE("vcsa_actn_jumphitbcxcr"),
        VOICE("vcsa_actn_jumphitbhxcr"),
        VOICE("vcsa_actn_jumphitroxcr"),
        VOICE("vcsa_actn_jumphitshxcr"),
        VOICE("vcsa_actn_jumphitmcxcr"),
        VOICE("vcsa_actn_jumphitaoxcr"),
        VOICE("vcsa_actn_jumphitfcxcr"),
        VOICE("vcsa_actn_jumphitbirain"),
        VOICE("vcsa_actn_jumphitmamain"),
        VOICE("vcsa_actn_jumphitinsain")
    },
    {
        VOICE("vcsa_emo_angryjhxcr"),
        VOICE("vcsa_emo_angryjcxcr"),
        VOICE("vcsa_emo_angrypoxcr"),
        VOICE("vcsa_emo_angrybcxcr"),
        VOICE("vcsa_emo_angrybhxcr"),
        VOICE("vcsa_emo_angryroxcr"),
        VOICE("vcsa_emo_angryshxcr"),
        VOICE("vcsa_emo_angrymcxcr"),
        VOICE("vcsa_emo_angryaoxcr"),
        VOICE("vcsa_emo_angryfcxcr"),
        VOICE("vcsa_emo_angrybirain"),
        VOICE("vcsa_emo_angrymamain"),
        VOICE("vcsa_emo_angryinsain")
    }
};
#undef VOICE

typedef struct {
    const char* name;
    enum fang_mod_voice_kind kind;
} mod_voice_cue;

/* Explicit source cues recovered from retail CompiledAnimData.package. Do not
 * reinterpret arbitrary weapon sounds, ship dialogue or unfamiliar events.
 */
static const mod_voice_cue voice_cues[] = {
    {"vo_char_attack", MOD_VOICE_ATTACK},
    {"vo_char_attack_end", MOD_VOICE_ATTACK},
    {"vo_attack", MOD_VOICE_ATTACK},
    {"vo_attackb", MOD_VOICE_ATTACK},
    {"vo_attack_melee", MOD_VOICE_ATTACK},
    {"vo_attack_mini", MOD_VOICE_ATTACK},
    {"vo_melee", MOD_VOICE_ATTACK},
    {"vo_punch", MOD_VOICE_ATTACK},
    {"vo_lob", MOD_VOICE_ATTACK},
    {"vcom_bite_lvl01", MOD_VOICE_ATTACK},
    {"vcom_bite_lvl02", MOD_VOICE_ATTACK},
    {"vcom_bite_lvl03", MOD_VOICE_ATTACK},
    {"vcom_bite_lvl04", MOD_VOICE_ATTACK},
    {"vcom_bite_lvl05", MOD_VOICE_ATTACK},
    {"vcom_strike_lvl01", MOD_VOICE_ATTACK},
    {"vcom_strike_lvl02", MOD_VOICE_ATTACK},
    {"vcom_strike_lvl03", MOD_VOICE_ATTACK},
    {"vcom_strike_lvl04", MOD_VOICE_ATTACK},
    {"vcom_strike_lvl05", MOD_VOICE_ATTACK},
    {"vcom_att_club1", MOD_VOICE_ATTACK},
    {"vcom_punch", MOD_VOICE_ATTACK},
    {"vcom_spit", MOD_VOICE_ATTACK},
    {"vcom_stun", MOD_VOICE_ATTACK},
    {"vo_cast", MOD_VOICE_CAST},
    {"vo_charge", MOD_VOICE_CAST},
    {"vo_charge_start", MOD_VOICE_CAST},
    {"vo_charge_end", MOD_VOICE_CAST},
    {"vo_chargeup", MOD_VOICE_CAST},
    {"vo_buildup", MOD_VOICE_CAST},
    {"vo_minioncall", MOD_VOICE_CAST},
    {"vcsa_actn_jumphit", MOD_VOICE_CAST},
    {"vcsa_actn_punch", MOD_VOICE_CAST},
    {"vo_taunt", MOD_VOICE_TAUNT},
    {"vo_roar", MOD_VOICE_TAUNT},
    {"vo_aggro", MOD_VOICE_TAUNT},
    {"vo_enrage", MOD_VOICE_TAUNT},
    {"vo_alert", MOD_VOICE_TAUNT},
    {"vcsa_actn_battleroar", MOD_VOICE_TAUNT},
    {"vo_char_hit_react", MOD_VOICE_HIT},
    {"vo_die", MOD_VOICE_DEATH},
    {"vo_death", MOD_VOICE_DEATH},
    {"vo_idle", MOD_VOICE_IDLE},
    {"vo_idle_b", MOD_VOICE_IDLE},
    {"vo_idle_c", MOD_VOICE_IDLE},
    {"vcode_select_grumble", MOD_VOICE_IDLE},
    {"vcom_grumble", MOD_VOICE_IDLE},
    {"vo_fear", MOD_VOICE_FEAR},
    {"vo_fear_tremble", MOD_VOICE_FEAR},
    {"vo_fear_yelp", MOD_VOICE_FEAR},
    {"vcsa_emo_scared", MOD_VOICE_FEAR},
    {"vo_laugh", MOD_VOICE_LAUGH},
    {"vcsa_emo_laugh", MOD_VOICE_LAUGH},
    {"vo_char_unlock", MOD_VOICE_HAPPY},
    {"vcsa_emo_happy", MOD_VOICE_HAPPY},
    {"vo_growl", MOD_VOICE_GROWL},
    {"vo_hiss", MOD_VOICE_GROWL},
    {"vcsa_emo_angry", MOD_VOICE_GROWL}
};

static void* native_animation_audio __attribute__((used));
static volatile LONG voice_trace_count;
static volatile LONG unmapped_trace_count;

static uint32_t voice_hash(const char* text) {
    uint32_t hash = 0x811c9dc5;
    while (*text) {
        unsigned char letter = (unsigned char)*text++;
        if (letter >= 'A' && letter <= 'Z') letter += 'a' - 'A';
        hash = hash * 0x01000193 ^ letter;
    }
    return hash;
}

static int voice_family(uint32_t mouth) {
    static const char* families[] = {"jh", "jc", "po", "bc", "bh", "ro", "sh", "mc", "ao", "fc",
        "bir", "mam", "ins"};
    unsigned int index;
    for (index = 0; index < sizeof(families) / sizeof(families[0]); ++index) {
        char name[8];
        char variation;
        size_t length = strlen(families[index]);
        if (voice_hash(families[index]) == mouth) return (int)index;
        strcpy(name, families[index]);
        name[length + 1] = 0;
        for (variation = 'a'; variation <= 'd'; ++variation) {
            name[length] = variation;
            if (voice_hash(name) == mouth) return (int)index;
        }
        name[length] = 'x';
        if (voice_hash(name) == mouth) return (int)index;
    }
    return -1;
}

static int voice_is_bound(BYTE* rig) {
    BYTE* recipe;
    uint32_t key[3];
    if (!readable_range(rig, 760)) return 0;
    recipe = *(BYTE**)rig;
    if (!readable_range(recipe, 804)) return 0;
    key[0] = *(uint32_t*)(recipe + 792);
    key[1] = 0x2b978c46;
    key[2] = *(uint32_t*)(recipe + 796) & 0xe0ffffff;
    return fang_mod_is_bound_key(key);
}

static int voice_rig_family(BYTE* rig, uint32_t joint_index) {
    uintptr_t begin = *(uintptr_t*)(rig + 752);
    uintptr_t end = *(uintptr_t*)(rig + 756);
    uintptr_t joint;
    float mouth_size = -1.f;
    int family = voice_family(*(uint32_t*)(rig + 744));
    if (family >= 0) return family;
    if (end < begin || (end - begin) % 708 || (end - begin) / 708 > 1024 ||
        !readable_range((void*)begin, end - begin)) return -1;
    if (joint_index < (end - begin) / 708) {
        BYTE* part = *(BYTE**)(begin + 708 * joint_index);
        if (readable_range(part, 1076)) {
            family = voice_family(*(uint32_t*)(part + 1072));
            if (family >= 0) return family;
        }
    }
    for (joint = begin; joint < end; joint += 708) {
        BYTE* part = *(BYTE**)joint;
        int candidate;
        float size;
        if (!readable_range(part, 1076)) continue;
        candidate = voice_family(*(uint32_t*)(part + 1072));
        size = *(float*)(part + 896);
        if (candidate < 0 || !isfinite(size) || size <= mouth_size) continue;
        family = candidate;
        mouth_size = size;
    }
    return family;
}

uint32_t fang_mod_vocalization(BYTE* rig, uint32_t joint, enum fang_mod_voice_kind kind) {
    int family;
    if ((unsigned int)kind >= MOD_VOICE_COUNT || !voice_is_bound(rig)) return 0;
    family = voice_rig_family(rig, joint);
    if (family < 0) return 0;
    return voice_hash(voice_events[kind][family].event);
}

/* Called only by the register adapter below. Its three arguments are normal
 * cdecl, with an aligned compiler-owned frame; the caller's EAX rig is saved.
 */
static const char* __attribute__((used, noinline, force_align_arg_pointer))
mod_voice_animation_event(BYTE* rig, const char* event, uint32_t joint) {
    char name[128];
    unsigned int length;
    unsigned int index;
    int family;
    enum fang_mod_voice_kind kind = MOD_VOICE_COUNT;
    if (!readable_range(event, sizeof(name))) return event;
    for (length = 0; length < sizeof(name); ++length) {
        unsigned char letter = (unsigned char)event[length];
        if (letter >= 'A' && letter <= 'Z') letter += 'a' - 'A';
        name[length] = (char)letter;
        if (!letter) break;
    }
    if (length == sizeof(name) || name[0] != 'v') return event;
    for (index = 0; index < sizeof(voice_cues) / sizeof(voice_cues[0]); ++index) {
        if (strcmp(name, voice_cues[index].name) == 0) {
            kind = voice_cues[index].kind;
            break;
        }
    }
    if (kind == MOD_VOICE_COUNT || !voice_is_bound(rig)) return event;
    family = voice_rig_family(rig, joint);
    if (family < 0) {
        if (InterlockedIncrement(&unmapped_trace_count) <= 32)
            trace_client_state("mod_voice_family_unmapped", voice_hash(name));
        return event;
    }
    if (InterlockedIncrement(&voice_trace_count) <= 96) {
        trace_client_state("mod_voice_cue", voice_hash(name));
        trace_client_state("mod_voice_kind", kind);
        trace_client_state("mod_voice_family", (unsigned int)family);
    }
    return voice_events[kind][family].marker;
}

/* Original 0x99C480 takes rig in EAX plus seven caller-cleaned stack args.
 * Change only the event-string argument, restore EAX and tail-call native.
 * Its original caller still receives/owns the returned playback handle.
 */
static void __attribute__((naked)) mod_voice_animation(void) {
    __asm__(
        "pushl %ebp\n"
        "movl %esp, %ebp\n"
        "pushl %eax\n"
        "pushl 12(%ebp)\n"
        "pushl 8(%ebp)\n"
        "pushl %eax\n"
        "call _mod_voice_animation_event\n"
        "addl $12, %esp\n"
        "movl %eax, 8(%ebp)\n"
        "movl -4(%ebp), %eax\n"
        "leave\n"
        "jmp *_native_animation_audio\n");
}

int fang_mod_select_voice(char* event, int capacity) {
    unsigned int kind;
    unsigned int family;
    if (capacity <= 10 || capacity > 200 || !readable_range(event, (size_t)capacity) ||
        memcmp(event, "vfang_mod:", 10) != 0) return 0;
    for (kind = 0; kind < MOD_VOICE_COUNT; ++kind) {
        for (family = 0; family < 13; ++family) {
            const mod_voice_event* voice = &voice_events[kind][family];
            size_t marker_length = strlen(voice->marker);
            size_t event_length;
            if (marker_length >= (size_t)capacity || memcmp(event, voice->marker, marker_length + 1) != 0)
                continue;
            event_length = strlen(voice->event);
            memcpy(event, voice->event, event_length + 1);
            return 1;
        }
    }
    return 0;
}

void fang_mod_install_voice(BYTE* base) {
    static const BYTE signature[] = {0x83, 0xec, 0x18, 0x53, 0x8b, 0x5c, 0x24, 0x28};
    if (!readable_range(base + 0x59c480, sizeof(signature)) ||
        memcmp(base + 0x59c480, signature, sizeof(signature)) != 0) {
        trace_client_state("mod_voice_unsupported", 1);
        return;
    }
    native_animation_audio = base + 0x59c480;
    /* General animation sound event branch, not either footstep caller. */
    if (!patch_call(base + 0x59e50d, native_animation_audio, mod_voice_animation)) {
        trace_client_state("mod_voice_hook_failed", 1);
        return;
    }
    trace_client_state("mod_voice_enabled", 1);
}

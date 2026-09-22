#include "bridge.h"

#include <dlfcn.h>
#include <stdio.h>
#include <string.h>

/* Implemented in Go (callbacks.go). */
extern void     goVideoRefresh(void *data, unsigned width, unsigned height, size_t pitch);
extern size_t   goAudioBatch(int16_t *data, size_t frames);
extern void     goInputPoll(void);
extern int16_t  goInputState(unsigned port, unsigned device, unsigned index, unsigned id);

static void *g_handle;
static unsigned g_pixfmt = RETRO_PIXEL_FORMAT_0RGB1555;

static char g_system_dir[1024] = ".";
static char g_save_dir[1024]   = ".";

static void     (*p_set_environment)(retro_environment_t);
static void     (*p_set_video_refresh)(retro_video_refresh_t);
static void     (*p_set_audio_sample)(retro_audio_sample_t);
static void     (*p_set_audio_sample_batch)(retro_audio_sample_batch_t);
static void     (*p_set_input_poll)(retro_input_poll_t);
static void     (*p_set_input_state)(retro_input_state_t);
static void     (*p_init)(void);
static void     (*p_deinit)(void);
static unsigned (*p_api_version)(void);
static void     (*p_get_system_av_info)(struct retro_system_av_info *);
static bool     (*p_load_game)(const struct retro_game_info *);
static void     (*p_unload_game)(void);
static void     (*p_run)(void);
static void    *(*p_get_memory_data)(unsigned);
static size_t   (*p_get_memory_size)(unsigned);
static size_t   (*p_serialize_size)(void);
static bool     (*p_serialize)(void *, size_t);
static bool     (*p_unserialize)(const void *, size_t);
static void     (*p_set_controller_port_device)(unsigned, unsigned);

/* ---- callback trampolines ---- */

static void cb_video(const void *data, unsigned w, unsigned h, size_t pitch) {
    goVideoRefresh((void *)data, w, h, pitch);
}

static void cb_audio_sample(int16_t l, int16_t r) {
    int16_t buf[2] = { l, r };
    goAudioBatch(buf, 1);
}

static size_t cb_audio_batch(const int16_t *data, size_t frames) {
    return goAudioBatch((int16_t *)data, frames);
}

static void cb_input_poll(void) { goInputPoll(); }

static int16_t cb_input_state(unsigned port, unsigned device, unsigned index, unsigned id) {
    return goInputState(port, device, index, id);
}

static bool cb_environment(unsigned cmd, void *data) {
    switch (cmd) {
    case RETRO_ENVIRONMENT_GET_CAN_DUPE:
        *(bool *)data = true;
        return true;
    case RETRO_ENVIRONMENT_SET_PIXEL_FORMAT:
        g_pixfmt = *(const enum retro_pixel_format *)data;
        return true;
    case RETRO_ENVIRONMENT_GET_SYSTEM_DIRECTORY:
        *(const char **)data = g_system_dir;
        return true;
    case RETRO_ENVIRONMENT_GET_SAVE_DIRECTORY:
        *(const char **)data = g_save_dir;
        return true;
    case RETRO_ENVIRONMENT_SET_PERFORMANCE_LEVEL:
    case RETRO_ENVIRONMENT_SET_INPUT_DESCRIPTORS:
    case RETRO_ENVIRONMENT_SET_CONTROLLER_INFO:
    case RETRO_ENVIRONMENT_SET_VARIABLES:
    case RETRO_ENVIRONMENT_SET_SUPPORT_ACHIEVEMENTS:
    case RETRO_ENVIRONMENT_SET_MEMORY_MAPS:
    case RETRO_ENVIRONMENT_SET_GEOMETRY:
    case RETRO_ENVIRONMENT_SET_SERIALIZATION_QUIRKS:
        return true;
    case RETRO_ENVIRONMENT_GET_CORE_OPTIONS_VERSION:
        *(unsigned *)data = 0;   /* force the SET_VARIABLES fallback */
        return true;
    case RETRO_ENVIRONMENT_GET_VARIABLE_UPDATE:
        *(bool *)data = false;
        return true;
    default:
        /* Everything else: unsupported, the core falls back to defaults. */
        return false;
    }
}

/* ---- loading ---- */

#define SYM(var, name)                                                   \
    do {                                                                 \
        *(void **)(&var) = dlsym(g_handle, name);                        \
        if (!var) {                                                      \
            snprintf(errbuf, errlen, "missing symbol: %s", name);      \
            dlclose(g_handle); g_handle = NULL;                          \
            return -1;                                                   \
        }                                                                \
    } while (0)

int br_load(const char *path, char *errbuf, size_t errlen) {
    if (g_handle) { snprintf(errbuf, errlen, "core already loaded"); return -1; }

    g_handle = dlopen(path, RTLD_LAZY | RTLD_LOCAL);
    if (!g_handle) { snprintf(errbuf, errlen, "dlopen: %s", dlerror()); return -1; }

    SYM(p_set_environment,          "retro_set_environment");
    SYM(p_set_video_refresh,        "retro_set_video_refresh");
    SYM(p_set_audio_sample,         "retro_set_audio_sample");
    SYM(p_set_audio_sample_batch,   "retro_set_audio_sample_batch");
    SYM(p_set_input_poll,           "retro_set_input_poll");
    SYM(p_set_input_state,          "retro_set_input_state");
    SYM(p_init,                     "retro_init");
    SYM(p_deinit,                   "retro_deinit");
    SYM(p_api_version,              "retro_api_version");
    SYM(p_get_system_av_info,       "retro_get_system_av_info");
    SYM(p_load_game,                "retro_load_game");
    SYM(p_unload_game,              "retro_unload_game");
    SYM(p_run,                      "retro_run");
    SYM(p_get_memory_data,          "retro_get_memory_data");
    SYM(p_get_memory_size,          "retro_get_memory_size");
    SYM(p_serialize_size,           "retro_serialize_size");
    SYM(p_serialize,                "retro_serialize");
    SYM(p_unserialize,              "retro_unserialize");
    SYM(p_set_controller_port_device, "retro_set_controller_port_device");

    /* The environment callback must be registered before retro_init. */
    p_set_environment(cb_environment);
    p_set_video_refresh(cb_video);
    p_set_audio_sample(cb_audio_sample);
    p_set_audio_sample_batch(cb_audio_batch);
    p_set_input_poll(cb_input_poll);
    p_set_input_state(cb_input_state);
    return 0;
}

void br_unload(void) {
    if (g_handle) { dlclose(g_handle); g_handle = NULL; }
}

void br_set_dirs(const char *system, const char *save) {
    snprintf(g_system_dir, sizeof(g_system_dir), "%s", system);
    snprintf(g_save_dir,   sizeof(g_save_dir),   "%s", save);
}

void     br_init(void)        { p_init(); }
void     br_deinit(void)      { p_deinit(); }
unsigned br_api_version(void) { return p_api_version(); }
void     br_run(void)         { p_run(); }
void     br_unload_game(void) { p_unload_game(); }
unsigned br_pixel_format(void){ return g_pixfmt; }

void br_get_av_info(struct retro_system_av_info *info) { p_get_system_av_info(info); }

int br_load_game(const char *path, const void *data, size_t size) {
    struct retro_game_info gi;
    memset(&gi, 0, sizeof(gi));
    gi.path = path;
    gi.data = data;
    gi.size = size;
    return p_load_game(&gi) ? 0 : -1;
}

void  *br_memory_data(unsigned id) { return p_get_memory_data(id); }
size_t br_memory_size(unsigned id) { return p_get_memory_size(id); }

size_t br_serialize_size(void) { return p_serialize_size(); }
int    br_serialize(void *data, size_t size)        { return p_serialize(data, size) ? 0 : -1; }
int    br_unserialize(const void *data, size_t size){ return p_unserialize(data, size) ? 0 : -1; }

void br_set_controller_port_device(unsigned port, unsigned device) {
    p_set_controller_port_device(port, device);
}

#ifndef MP_BRIDGE_H
#define MP_BRIDGE_H

#include <stddef.h>
#include <stdint.h>
#include "libretro.h"

/* Loads the core and resolves its symbols. 0 = ok, -1 = error (message in errbuf). */
int    br_load(const char *path, char *errbuf, size_t errlen);
void   br_unload(void);

void   br_init(void);
void   br_deinit(void);
unsigned br_api_version(void);

int    br_load_game(const char *path, const void *data, size_t size);
void   br_unload_game(void);
void   br_run(void);

void   br_get_av_info(struct retro_system_av_info *info);
unsigned br_pixel_format(void);

void  *br_memory_data(unsigned id);
size_t br_memory_size(unsigned id);

size_t br_serialize_size(void);
int    br_serialize(void *data, size_t size);
int    br_unserialize(const void *data, size_t size);

void   br_set_controller_port_device(unsigned port, unsigned device);

/* Directories reported to the core via the environment callback. */
void   br_set_dirs(const char *system, const char *save);

#endif

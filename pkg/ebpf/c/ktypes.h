#ifndef __KTYPES_H__
#define __KTYPES_H__

#include "bpf_metadata.h"

#ifdef COMPILE_CORE
#include "vmlinux.h"
#else
#include <linux/types.h>
#include <linux/version.h>
#endif

#endif

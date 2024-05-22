#ifndef __BPF_METADATA_H__
#define __BPF_METADATA_H__

#if defined(__x86_64__) || defined(__TARGET_ARCH_x86)
char __dd_metadata_arch[] __attribute__((section("dd_metadata"), used)) = "<arch:amd64>";
#elif defined(__aarch64__) || defined(__TARGET_ARCH_arm64)
char __dd_metadata_arch[] __attribute__((section("dd_metadata"), used)) = "<arch:arm64>";
#else
char __dd_metadata_arch[] __attribute__((section("dd_metadata"), used)) = "<arch:unset>";
#endif

#endif

#ifndef __SOCK_H
#define __SOCK_H

#ifndef COMPILE_CORE
#include <linux/net.h>      // for socket
#include <linux/socket.h>   // for AF_INET, AF_INET6
#include <linux/tcp.h>      // IWYU pragma: keep // for tcp_sk, tcp_sock
#include <net/inet_sock.h>  // IWYU pragma: keep // for inet_sk, inet_sock, inet_daddr, inet_dport
#include <net/sock.h>       // for sock
#include <uapi/linux/in6.h> // for in6_addr
#endif

#include "bpf_builtins.h"         // for bpf_memset
#include "bpf_endian.h"           // for bpf_ntohs
#include "bpf_helpers.h"          // for __always_inline, log_debug, NULL, bpf_probe_read_kernel
#include "bpf_telemetry.h"        // for FN_INDX_bpf_probe_read_kernel, bpf_probe_read_kernel_with_telemetry
#include "bpf_tracing.h"          // for BPF_CORE_READ_INTO
#include "conn_tuple.h"           // for conn_tuple_t, CONN_TYPE_TCP, CONN_V4, metadata_mask_t, CONN_TYPE_UDP, CONN_V6
#include "ipv6.h"                 // for read_in6_addr, is_ipv4_mapped_ipv6, is_tcpv6_enabled, is_udpv6_enabled
#include "ktypes.h"               // for u16, u32, ...
#include "netns.h"                // for get_netns_from_sock
#include "offsets.h"     // for offset_socket_sk

#ifdef COMPILE_CORE

static __always_inline struct tcp_sock *tcp_sk(const struct sock *sk)
{
    return (struct tcp_sock *)sk;
}

static __always_inline struct inet_sock *inet_sk(const struct sock *sk)
{
    return (struct inet_sock *)sk;
}

#endif // COMPILE_CORE

#ifndef MSG_SPLICE_PAGES
#define MSG_SPLICE_PAGES 0x8000000
#endif

static __always_inline struct sock * socket_sk(struct socket *sock) {
    struct sock * sk = NULL;
#ifdef COMPILE_PREBUILT
    if (bpf_probe_read_kernel_with_telemetry(&sk, sizeof(sk), ((char*)sock) + offset_socket_sk()) < 0) {
        return NULL;
    }
#elif defined(COMPILE_CORE) || defined(COMPILE_RUNTIME)
    BPF_CORE_READ_INTO(&sk, sock, sk);
#endif
    return sk;
}

static __always_inline void get_tcp_segment_counts(struct sock* skp, __u32* packets_in, __u32* packets_out) {
#ifdef COMPILE_PREBUILT
    // counting segments/packets not currently supported on prebuilt
    // to implement, would need to do the offset-guess on the following
    // fields in the tcp_sk: packets_in & packets_out (respectively)
    *packets_in = 0;
    *packets_out = 0;
#elif defined(COMPILE_CORE) || defined(COMPILE_RUNTIME)
    BPF_CORE_READ_INTO(packets_out, tcp_sk(skp), segs_out);
    BPF_CORE_READ_INTO(packets_in, tcp_sk(skp), segs_in);
#endif
}

static __always_inline u16 read_sport(struct sock* skp) {
    // try skc_num, then inet_sport
    u16 sport = 0;
#ifdef COMPILE_PREBUILT
    // try skc_num, then inet_sport
    bpf_probe_read_kernel_with_telemetry(&sport, sizeof(sport), ((char*)skp) + offset_dport() + sizeof(sport));
    if (sport == 0) {
        bpf_probe_read_kernel_with_telemetry(&sport, sizeof(sport), ((char*)skp) + offset_sport());
        sport = bpf_ntohs(sport);
    }
#elif defined(COMPILE_CORE) || defined(COMPILE_RUNTIME)
    BPF_CORE_READ_INTO(&sport, skp, sk_num);
    if (sport == 0) {
        BPF_CORE_READ_INTO(&sport, inet_sk(skp), inet_sport);
        sport = bpf_ntohs(sport);
    }
#endif

    return sport;
}

static u16 read_dport(struct sock *skp) {
    u16 dport = 0;
#ifdef COMPILE_PREBUILT
    bpf_probe_read_kernel_with_telemetry(&dport, sizeof(dport), ((char*)skp) + offset_dport());
#elif defined(COMPILE_CORE) || defined(COMPILE_RUNTIME)
    bpf_probe_read_kernel(&dport, sizeof(dport), &skp->sk_dport);
    BPF_CORE_READ_INTO(&dport, skp, sk_dport);
    if (dport == 0) {
        BPF_CORE_READ_INTO(&dport, inet_sk(skp), inet_dport);
    }
#endif

    return bpf_ntohs(dport);
}

static __always_inline u32 read_saddr_v4(struct sock *skp) {
    u32 saddr = 0;
#ifdef COMPILE_PREBUILT
    bpf_probe_read_kernel_with_telemetry(&saddr, sizeof(u32), ((char*)skp) + offset_saddr());
#elif defined(COMPILE_CORE) || defined(COMPILE_RUNTIME)
    BPF_CORE_READ_INTO(&saddr, skp, sk_rcv_saddr);
    if (saddr == 0) {
        BPF_CORE_READ_INTO(&saddr, inet_sk(skp), inet_saddr);
    }
#endif

    return saddr;
}

static __always_inline u32 read_daddr_v4(struct sock *skp) {
    u32 daddr = 0;
#ifdef COMPILE_PREBUILT
    bpf_probe_read_kernel_with_telemetry(&daddr, sizeof(u32), ((char*)skp) + offset_daddr());
#elif defined(COMPILE_CORE) || defined(COMPILE_RUNTIME)
    BPF_CORE_READ_INTO(&daddr, skp, sk_daddr);
    if (daddr == 0) {
        BPF_CORE_READ_INTO(&daddr, inet_sk(skp), inet_daddr);
    }
#endif

    return daddr;
}

static __always_inline void read_saddr_v6(struct sock *skp, u64 *addr_h, u64 *addr_l) {
    struct in6_addr in6 = {};
#ifdef COMPILE_PREBUILT
    bpf_probe_read_kernel_with_telemetry(&in6, sizeof(in6), ((char*)skp) + offset_daddr_ipv6() + 2 * sizeof(u64));
#elif defined(COMPILE_CORE) || defined(COMPILE_RUNTIME)
    BPF_CORE_READ_INTO(&in6, skp, sk_v6_rcv_saddr);
#endif
    read_in6_addr(addr_h, addr_l, &in6);
}

static __always_inline void read_daddr_v6(struct sock *skp, u64 *addr_h, u64 *addr_l) {
    struct in6_addr in6 = {};
#ifdef COMPILE_PREBUILT
    bpf_probe_read_kernel_with_telemetry(&in6, sizeof(in6), ((char*)skp) + offset_daddr_ipv6());
#elif defined(COMPILE_CORE) || defined(COMPILE_RUNTIME)
    BPF_CORE_READ_INTO(&in6, skp, sk_v6_daddr);
#endif
    read_in6_addr(addr_h, addr_l, &in6);
}

static __always_inline u16 _sk_family(struct sock *skp) {
    u16 family = 0;
#ifdef COMPILE_PREBUILT
    bpf_probe_read_kernel_with_telemetry(&family, sizeof(family), ((char*)skp) + offset_family());
#elif defined(COMPILE_CORE) || defined(COMPILE_RUNTIME)
    BPF_CORE_READ_INTO(&family, skp, sk_family);
#endif
    return family;
}

/**
 * Reads values into a `conn_tuple_t` from a `sock`. Any values that are already set in conn_tuple_t
 * are not overwritten. Returns 1 success, 0 otherwise.
 */
static __always_inline int read_conn_tuple_partial(conn_tuple_t* t, struct sock* skp, u64 pid_tgid, metadata_mask_t type) {
    int err = 0;
    t->pid = pid_tgid >> 32;
    t->metadata = type;

    // Retrieve network namespace id first since addresses and ports may not be available for unconnected UDP
    // sends
    t->netns = get_netns_from_sock(skp);
    u16 family = _sk_family(skp);
    // Retrieve addresses
    if (family == AF_INET) {
        t->metadata |= CONN_V4;
        if (t->saddr_l == 0) {
            t->saddr_l = read_saddr_v4(skp);
        }
        if (t->daddr_l == 0) {
            t->daddr_l = read_daddr_v4(skp);
        }

        if (t->saddr_l == 0 || t->daddr_l == 0) {
            log_debug("ERR(read_conn_tuple.v4): src or dst addr not set src=%llu, dst=%llu", t->saddr_l, t->daddr_l);
            err = 1;
        }
    } else if (family == AF_INET6) {
        if (!is_tcpv6_enabled() && !is_udpv6_enabled()) {
            return 0;
        }

        if (!(t->saddr_h || t->saddr_l)) {
            read_saddr_v6(skp, &t->saddr_h, &t->saddr_l);
        }
        if (!(t->daddr_h || t->daddr_l)) {
            read_daddr_v6(skp, &t->daddr_h, &t->daddr_l);
        }

        /* We can only pass 4 args to bpf_trace_printk */
        /* so split those 2 statements to be able to log everything */
        if (!(t->saddr_h || t->saddr_l)) {
            log_debug("ERR(read_conn_tuple.v6): src addr not set: src_l:%llu,src_h:%llu",
                t->saddr_l, t->saddr_h);
            err = 1;
        }

        if (!(t->daddr_h || t->daddr_l)) {
            log_debug("ERR(read_conn_tuple.v6): dst addr not set: dst_l:%llu,dst_h:%llu",
                t->daddr_l, t->daddr_h);
            err = 1;
        }

        // Check if we can map IPv6 to IPv4
        if (is_ipv4_mapped_ipv6(t->saddr_h, t->saddr_l, t->daddr_h, t->daddr_l)) {
            t->metadata |= CONN_V4;
            t->saddr_h = 0;
            t->daddr_h = 0;
            t->saddr_l = (__u32)(t->saddr_l >> 32);
            t->daddr_l = (__u32)(t->daddr_l >> 32);
        } else {
            t->metadata |= CONN_V6;
        }
    } else {
        log_debug("ERR(read_conn_tuple): unknown family %d", family);
        err = 1;
    }

    // Retrieve ports
    if (t->sport == 0) {
        t->sport = read_sport(skp);
    }
    if (t->dport == 0) {
        t->dport = read_dport(skp);
    }

    if (t->sport == 0 || t->dport == 0) {
        log_debug("ERR(read_conn_tuple.v4): src/dst port not set: src:%d, dst:%d", t->sport, t->dport);
        err = 1;
    }

    return err ? 0 : 1;
}

/**
 * Reads values into a `conn_tuple_t` from a `sock`. Initializes all values in conn_tuple_t to `0`. Returns 1 success, 0 otherwise.
 */
static __always_inline int read_conn_tuple(conn_tuple_t* t, struct sock* skp, u64 pid_tgid, metadata_mask_t type) {
    bpf_memset(t, 0, sizeof(conn_tuple_t));
    return read_conn_tuple_partial(t, skp, pid_tgid, type);
}

static __always_inline int get_proto(conn_tuple_t *t) {
    return (t->metadata & CONN_TYPE_TCP) ? CONN_TYPE_TCP : CONN_TYPE_UDP;
}

#endif // __SOCK_H

// SPDX-License-Identifier: GPL-2.0
// FlowScope eBPF Flow Tracker
// Tracks TCP/UDP connections and data transfer at socket level

#include <linux/bpf.h>
#include <linux/if_ether.h>
#include <linux/ip.h>
#include <linux/ipv6.h>
#include <linux/tcp.h>
#include <linux/udp.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_endian.h>

#define MAX_ENTRIES 65536
#define TASK_COMM_LEN 16

// Flow key for identifying unique connections
struct flow_key {
    __u32 src_ip;
    __u32 dst_ip;
    __u16 src_port;
    __u16 dst_port;
    __u8  protocol;
    __u8  pad[3];
};

// Flow metrics
struct flow_metrics {
    __u64 bytes_sent;
    __u64 bytes_received;
    __u64 packets_sent;
    __u64 packets_received;
    __u64 start_time_ns;
    __u64 last_seen_ns;
    __u32 pid;
    __u32 uid;
    char  comm[TASK_COMM_LEN];
};

// Event sent to userspace
struct flow_event {
    struct flow_key key;
    struct flow_metrics metrics;
    __u8  event_type; // 0 = update, 1 = close
    __u8  direction;  // 0 = egress, 1 = ingress
    __u8  pad[6];
};

// Map to track active flows
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, MAX_ENTRIES);
    __type(key, struct flow_key);
    __type(value, struct flow_metrics);
} flow_map SEC(".maps");

// Ring buffer for events to userspace
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024); // 256KB ring buffer
} events SEC(".maps");

// Configuration map
struct {
    __uint(type, BPF_MAP_TYPE_ARRAY);
    __uint(max_entries, 1);
    __type(key, __u32);
    __type(value, __u64);
} config SEC(".maps");

// Helper to create flow key from packet
static __always_inline void create_flow_key(
    struct flow_key *key,
    __u32 src_ip, __u32 dst_ip,
    __u16 src_port, __u16 dst_port,
    __u8 protocol
) {
    key->src_ip = src_ip;
    key->dst_ip = dst_ip;
    key->src_port = src_port;
    key->dst_port = dst_port;
    key->protocol = protocol;
    key->pad[0] = 0;
    key->pad[1] = 0;
    key->pad[2] = 0;
}

// Track egress packets (outbound from pod)
SEC("cgroup_skb/egress")
int track_egress(struct __sk_buff *skb) {
    void *data = (void *)(long)skb->data;
    void *data_end = (void *)(long)skb->data_end;

    // Parse Ethernet header (if present)
    struct ethhdr *eth = data;
    if ((void *)(eth + 1) > data_end)
        return 1; // Allow packet

    // Skip non-IP packets
    __u16 eth_proto = bpf_ntohs(eth->h_proto);
    if (eth_proto != ETH_P_IP)
        return 1;

    // Parse IP header
    struct iphdr *ip = (void *)(eth + 1);
    if ((void *)(ip + 1) > data_end)
        return 1;

    // Only track TCP and UDP
    if (ip->protocol != IPPROTO_TCP && ip->protocol != IPPROTO_UDP)
        return 1;

    __u16 src_port = 0, dst_port = 0;

    if (ip->protocol == IPPROTO_TCP) {
        struct tcphdr *tcp = (void *)ip + (ip->ihl * 4);
        if ((void *)(tcp + 1) > data_end)
            return 1;
        src_port = bpf_ntohs(tcp->source);
        dst_port = bpf_ntohs(tcp->dest);
    } else {
        struct udphdr *udp = (void *)ip + (ip->ihl * 4);
        if ((void *)(udp + 1) > data_end)
            return 1;
        src_port = bpf_ntohs(udp->source);
        dst_port = bpf_ntohs(udp->dest);
    }

    // Create flow key
    struct flow_key key = {};
    create_flow_key(&key, ip->saddr, ip->daddr, src_port, dst_port, ip->protocol);

    // Get or create flow metrics
    struct flow_metrics *metrics = bpf_map_lookup_elem(&flow_map, &key);
    __u64 now = bpf_ktime_get_ns();

    if (!metrics) {
        // New flow
        struct flow_metrics new_metrics = {};
        new_metrics.bytes_sent = skb->len;
        new_metrics.packets_sent = 1;
        new_metrics.start_time_ns = now;
        new_metrics.last_seen_ns = now;
        new_metrics.pid = bpf_get_current_pid_tgid() >> 32;
        new_metrics.uid = bpf_get_current_uid_gid() & 0xFFFFFFFF;
        bpf_get_current_comm(&new_metrics.comm, sizeof(new_metrics.comm));

        bpf_map_update_elem(&flow_map, &key, &new_metrics, BPF_ANY);
    } else {
        // Update existing flow
        __sync_fetch_and_add(&metrics->bytes_sent, skb->len);
        __sync_fetch_and_add(&metrics->packets_sent, 1);
        metrics->last_seen_ns = now;
    }

    return 1; // Allow packet
}

// Track ingress packets (inbound to pod)
SEC("cgroup_skb/ingress")
int track_ingress(struct __sk_buff *skb) {
    void *data = (void *)(long)skb->data;
    void *data_end = (void *)(long)skb->data_end;

    struct ethhdr *eth = data;
    if ((void *)(eth + 1) > data_end)
        return 1;

    __u16 eth_proto = bpf_ntohs(eth->h_proto);
    if (eth_proto != ETH_P_IP)
        return 1;

    struct iphdr *ip = (void *)(eth + 1);
    if ((void *)(ip + 1) > data_end)
        return 1;

    if (ip->protocol != IPPROTO_TCP && ip->protocol != IPPROTO_UDP)
        return 1;

    __u16 src_port = 0, dst_port = 0;

    if (ip->protocol == IPPROTO_TCP) {
        struct tcphdr *tcp = (void *)ip + (ip->ihl * 4);
        if ((void *)(tcp + 1) > data_end)
            return 1;
        src_port = bpf_ntohs(tcp->source);
        dst_port = bpf_ntohs(tcp->dest);
    } else {
        struct udphdr *udp = (void *)ip + (ip->ihl * 4);
        if ((void *)(udp + 1) > data_end)
            return 1;
        src_port = bpf_ntohs(udp->source);
        dst_port = bpf_ntohs(udp->dest);
    }

    // For ingress, reverse the flow key to match egress
    struct flow_key key = {};
    create_flow_key(&key, ip->daddr, ip->saddr, dst_port, src_port, ip->protocol);

    struct flow_metrics *metrics = bpf_map_lookup_elem(&flow_map, &key);
    __u64 now = bpf_ktime_get_ns();

    if (metrics) {
        __sync_fetch_and_add(&metrics->bytes_received, skb->len);
        __sync_fetch_and_add(&metrics->packets_received, 1);
        metrics->last_seen_ns = now;
    }

    return 1;
}

// Periodic flush of flow data to userspace (called from userspace timer)
SEC("iter/bpf_map_elem")
int flush_flows(struct bpf_iter__bpf_map_elem *ctx) {
    struct flow_key *key = ctx->key;
    struct flow_metrics *metrics = ctx->value;

    if (!key || !metrics)
        return 0;

    // Reserve space in ring buffer
    struct flow_event *event = bpf_ringbuf_reserve(&events, sizeof(*event), 0);
    if (!event)
        return 0;

    // Copy flow data
    __builtin_memcpy(&event->key, key, sizeof(event->key));
    __builtin_memcpy(&event->metrics, metrics, sizeof(event->metrics));
    event->event_type = 0; // Update
    event->direction = 0;  // Will be determined by userspace

    bpf_ringbuf_submit(event, 0);

    return 0;
}

char LICENSE[] SEC("license") = "GPL";

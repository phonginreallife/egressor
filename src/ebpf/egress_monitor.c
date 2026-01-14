// SPDX-License-Identifier: GPL-2.0
// FlowScope eBPF Egress Monitor
// Specifically tracks egress traffic leaving the cluster

#include <linux/bpf.h>
#include <linux/if_ether.h>
#include <linux/ip.h>
#include <linux/tcp.h>
#include <linux/udp.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_endian.h>

#define MAX_ENTRIES 32768
#define MAX_CIDR_ENTRIES 256

// Egress event for external traffic
struct egress_event {
    __u32 src_ip;
    __u32 dst_ip;
    __u16 src_port;
    __u16 dst_port;
    __u8  protocol;
    __u8  pad[3];
    __u64 bytes;
    __u64 timestamp_ns;
    __u32 pid;
    __u32 pad2;
};

// CIDR range for cluster-internal IPs (to identify egress)
struct cidr_range {
    __u32 network;
    __u32 mask;
};

// Map of cluster CIDR ranges (internal IPs)
struct {
    __uint(type, BPF_MAP_TYPE_ARRAY);
    __uint(max_entries, MAX_CIDR_ENTRIES);
    __type(key, __u32);
    __type(value, struct cidr_range);
} cluster_cidrs SEC(".maps");

// Number of configured CIDRs
struct {
    __uint(type, BPF_MAP_TYPE_ARRAY);
    __uint(max_entries, 1);
    __type(key, __u32);
    __type(value, __u32);
} cidr_count SEC(".maps");

// Ring buffer for egress events
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 512 * 1024); // 512KB ring buffer
} egress_events SEC(".maps");

// Egress byte counter per destination
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, MAX_ENTRIES);
    __type(key, __u32); // dst_ip
    __type(value, __u64); // total bytes
} egress_bytes SEC(".maps");

// Check if IP is internal to cluster
static __always_inline int is_internal_ip(__u32 ip) {
    __u32 key = 0;
    __u32 *count = bpf_map_lookup_elem(&cidr_count, &key);
    if (!count)
        return 0;

    __u32 num_cidrs = *count;
    if (num_cidrs > MAX_CIDR_ENTRIES)
        num_cidrs = MAX_CIDR_ENTRIES;

    #pragma unroll
    for (__u32 i = 0; i < 16; i++) { // Check up to 16 CIDRs
        if (i >= num_cidrs)
            break;

        __u32 idx = i;
        struct cidr_range *cidr = bpf_map_lookup_elem(&cluster_cidrs, &idx);
        if (!cidr)
            continue;

        if ((ip & cidr->mask) == (cidr->network & cidr->mask))
            return 1;
    }

    return 0;
}

// Check for common private IP ranges
static __always_inline int is_private_ip(__u32 ip) {
    __u32 ip_host = bpf_ntohl(ip);
    
    // 10.0.0.0/8
    if ((ip_host & 0xFF000000) == 0x0A000000)
        return 1;
    
    // 172.16.0.0/12
    if ((ip_host & 0xFFF00000) == 0xAC100000)
        return 1;
    
    // 192.168.0.0/16
    if ((ip_host & 0xFFFF0000) == 0xC0A80000)
        return 1;
    
    // 127.0.0.0/8 (loopback)
    if ((ip_host & 0xFF000000) == 0x7F000000)
        return 1;

    return 0;
}

SEC("tc")
int monitor_egress(struct __sk_buff *skb) {
    void *data = (void *)(long)skb->data;
    void *data_end = (void *)(long)skb->data_end;

    // Parse Ethernet header
    struct ethhdr *eth = data;
    if ((void *)(eth + 1) > data_end)
        return TC_ACT_OK;

    if (eth->h_proto != bpf_htons(ETH_P_IP))
        return TC_ACT_OK;

    // Parse IP header
    struct iphdr *ip = (void *)(eth + 1);
    if ((void *)(ip + 1) > data_end)
        return TC_ACT_OK;

    // Only track TCP and UDP
    if (ip->protocol != IPPROTO_TCP && ip->protocol != IPPROTO_UDP)
        return TC_ACT_OK;

    // Check if destination is external (not private, not cluster internal)
    if (is_private_ip(ip->daddr) || is_internal_ip(ip->daddr))
        return TC_ACT_OK;

    // This is egress traffic!
    __u16 src_port = 0, dst_port = 0;

    if (ip->protocol == IPPROTO_TCP) {
        struct tcphdr *tcp = (void *)ip + (ip->ihl * 4);
        if ((void *)(tcp + 1) > data_end)
            return TC_ACT_OK;
        src_port = bpf_ntohs(tcp->source);
        dst_port = bpf_ntohs(tcp->dest);
    } else {
        struct udphdr *udp = (void *)ip + (ip->ihl * 4);
        if ((void *)(udp + 1) > data_end)
            return TC_ACT_OK;
        src_port = bpf_ntohs(udp->source);
        dst_port = bpf_ntohs(udp->dest);
    }

    // Update egress byte counter
    __u64 *bytes = bpf_map_lookup_elem(&egress_bytes, &ip->daddr);
    if (bytes) {
        __sync_fetch_and_add(bytes, skb->len);
    } else {
        __u64 initial = skb->len;
        bpf_map_update_elem(&egress_bytes, &ip->daddr, &initial, BPF_ANY);
    }

    // Send event to userspace
    struct egress_event *event = bpf_ringbuf_reserve(&egress_events, sizeof(*event), 0);
    if (!event)
        return TC_ACT_OK;

    event->src_ip = ip->saddr;
    event->dst_ip = ip->daddr;
    event->src_port = src_port;
    event->dst_port = dst_port;
    event->protocol = ip->protocol;
    event->bytes = skb->len;
    event->timestamp_ns = bpf_ktime_get_ns();
    event->pid = 0; // Not available in TC context

    bpf_ringbuf_submit(event, 0);

    return TC_ACT_OK;
}

char LICENSE[] SEC("license") = "GPL";

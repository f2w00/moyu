package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"net"
	"net/netip"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mdlayher/arp"
)

const (
	maxRetries     = 3
	initialTimeout = 500 * time.Millisecond
	backoff        = 1.5
	globalTimeout  = 3 * time.Second
	packetInterval = 4 * time.Millisecond
)

type device struct {
	IP  netip.Addr
	MAC net.HardwareAddr
	RTT time.Duration
}

type arpHost struct {
	ip       netip.Addr
	attempts int
	nextSend time.Time
	deadline time.Time
	sentAt   time.Time
}

func main() {
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "需要 root 权限运行")
		os.Exit(1)
	}

	iface, ipnet, err := resolveInterface()
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n", err)
		os.Exit(1)
	}

	localIP := ipnet.IP.To4()
	localAddr, _ := netip.AddrFromSlice(localIP)

	fmt.Printf("接口: %s (%s)\n", iface.Name, iface.HardwareAddr)
	fmt.Printf("IP:   %s\n", localAddr)
	fmt.Printf("掩码: %s\n\n", ipnet.Mask)

	targets := generateTargets(ipnet)
	fmt.Printf("扫描 %d 个目标 (最多重试 %d 次)\n\n", len(targets), maxRetries)

	start := time.Now()

	client, err := arp.Dial(iface)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ARP 失败: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	hosts := make([]*arpHost, len(targets))
	for i, ip := range targets {
		hosts[i] = &arpHost{ip: ip, nextSend: time.Now()}
	}

	results := scan(client, localAddr, hosts)

	printResults(results, time.Since(start))
}

func resolveInterface() (*net.Interface, *net.IPNet, error) {
	data, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return nil, nil, err
	}

	var bestIface string
	bestMetric := math.MaxInt32

	for _, line := range strings.Split(string(data), "\n")[1:] {
		fields := strings.Fields(line)
		if len(fields) < 8 || fields[1] != "00000000" {
			continue
		}
		flags, _ := strconv.ParseInt(fields[3], 16, 64)
		if flags&0x1 == 0 {
			continue
		}
		metric, _ := strconv.ParseInt(fields[6], 10, 64)
		if int(metric) < bestMetric {
			bestMetric = int(metric)
			bestIface = fields[0]
		}
	}

	if bestIface == "" {
		return nil, nil, fmt.Errorf("未找到默认路由")
	}

	iface, err := net.InterfaceByName(bestIface)
	if err != nil {
		return nil, nil, fmt.Errorf("接口 %s 不存在: %v", bestIface, err)
	}

	addrs, err := iface.Addrs()
	if err != nil {
		return nil, nil, err
	}
	for _, addr := range addrs {
		ipnet, ok := addr.(*net.IPNet)
		if ok && ipnet.IP.To4() != nil {
			return iface, ipnet, nil
		}
	}

	return nil, nil, fmt.Errorf("接口 %s 没有 IPv4 地址", bestIface)
}

func generateTargets(ipnet *net.IPNet) []netip.Addr {
	ones, bits := ipnet.Mask.Size()
	total := 1 << uint(bits-ones)

	network := ipnet.IP.Mask(ipnet.Mask).To4()
	results := make([]netip.Addr, 0, total-2)

	for i := 1; i < total-1; i++ {
		ip := make([]byte, 4)
		copy(ip, network)

		n := i
		for j := 3; j >= 0 && n > 0; j-- {
			ip[j] += byte(n & 0xFF)
			n >>= 8
		}

		addr, ok := netip.AddrFromSlice(ip)
		if ok {
			results = append(results, addr)
		}
	}

	return results
}

func scan(client *arp.Client, localIP netip.Addr, hosts []*arpHost) []device {
	var devices []device
	deadline := time.Now().Add(globalTimeout)
	active := len(hosts)

	for active > 0 && time.Now().Before(deadline) {
		now := time.Now()

		// send requests for all ready hosts
		for _, h := range hosts {
			if h == nil || !now.After(h.nextSend) {
				continue
			}
			client.Request(h.ip)
			h.sentAt = time.Now()
			h.attempts++
			timeout := float64(initialTimeout) * math.Pow(backoff, float64(h.attempts-1))
			h.deadline = h.sentAt.Add(time.Duration(timeout))
			h.nextSend = time.Time{} // clear, we're now waiting
			time.Sleep(packetInterval)
		}

		// find next event: earliest deadline or nextSend across all hosts
		nextEvent := deadline
		for _, h := range hosts {
			if h == nil {
				continue
			}
			if h.attempts == 0 {
				if h.nextSend.Before(nextEvent) {
					nextEvent = h.nextSend
				}
				continue
			}
			if h.attempts <= maxRetries {
				if h.deadline.Before(nextEvent) {
					nextEvent = h.deadline
				}
			}
			if h.attempts > 0 && h.attempts < maxRetries {
				if !h.nextSend.IsZero() && h.nextSend.Before(nextEvent) {
					nextEvent = h.nextSend
				}
			}
		}

		// read replies until next event (but at least 50ms to drain buffer)
		if !nextEvent.After(time.Now()) {
			nextEvent = time.Now().Add(50 * time.Millisecond)
		}
		client.SetReadDeadline(nextEvent)
	readLoop:
		for {
			pkt, _, err := client.Read()
			if err != nil {
				break
			}
			if pkt.Operation != arp.OperationReply || pkt.SenderIP == localIP {
				continue
			}
			for i, h := range hosts {
				if h == nil || h.ip != pkt.SenderIP {
					continue
				}
				devices = append(devices, device{
					IP:  h.ip,
					MAC: pkt.SenderHardwareAddr,
					RTT: time.Since(h.sentAt),
				})
				hosts[i] = nil
				active--
				continue readLoop
			}
		}

		// handle timeouts: retry or give up
		now = time.Now()
		for i, h := range hosts {
			if h == nil {
				continue
			}
			if h.attempts == 0 || !now.After(h.deadline) {
				continue
			}
			if h.attempts < maxRetries {
				// schedule retry immediately
				h.nextSend = now
			} else {
				hosts[i] = nil
				active--
			}
		}
	}

	return devices
}

func printResults(devices []device, elapsed time.Duration) {
	sort.Slice(devices, func(i, j int) bool {
		return binary.BigEndian.Uint32(devices[i].IP.AsSlice()) <
			binary.BigEndian.Uint32(devices[j].IP.AsSlice())
	})

	fmt.Println("IP 地址\t\tMAC 地址\t\t响应时间")
	fmt.Println("--------------------------------------------------")
	for _, d := range devices {
		rtt := d.RTT.Truncate(100 * time.Microsecond)
		fmt.Printf("%-15s\t%s\t%s\n", d.IP, d.MAC, rtt)
	}
	if len(devices) > 0 {
		fmt.Printf("\n发现 %d 台设备 (耗时 %v)\n", len(devices), elapsed.Truncate(10*time.Millisecond))
	}
}

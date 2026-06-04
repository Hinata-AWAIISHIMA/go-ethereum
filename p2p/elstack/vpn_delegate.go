package elstack

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/p2p/elstack/el_stack" // if you copied el_stack directory directly below elstack directory, use it.
)

// WisteriaVpnEventDelegate 実装
type VpnDelegate struct {
	AddrCh   chan net.IP
	ErrCh    chan error
	done     chan struct{}
	doneOnce sync.Once
	stopped  atomic.Bool
}

func NewELStackVpnDelegate() *VpnDelegate {
	return &VpnDelegate{
		AddrCh: make(chan net.IP, 1),
		ErrCh:  make(chan error, 1),
		done:   make(chan struct{}),
	}
}

func (d *VpnDelegate) Done() <-chan struct{} {
	if d == nil {
		return nil
	}
	return d.done
}

func (d *VpnDelegate) OnStatusChange(status el_stack.VpnStatus) {
	elLog.Debug("VPN Status", "status", status)
}

func (d *VpnDelegate) OnConnectionError(msg string) {
	elLog.Error("VPN Connection error", "msg", msg)
	d.sendErr(fmt.Errorf(msg))
}

func (d *VpnDelegate) OnLinkedParams(ipAddrs, dnsAddrs, routes []string) {
	elLog.Info("LinkedParams", "IP", ipAddrs, "DNS", dnsAddrs, "ROUTES", routes)
	if len(ipAddrs) == 0 {
		elLog.Warn("LinkedParams has no IP address yet; skipping")
		return
	}
	ipAddr := strings.TrimSpace(ipAddrs[0])
	if slash := strings.Index(ipAddr, "/"); slash >= 0 {
		ipAddr = strings.TrimSpace(ipAddr[:slash])
	}
	elLog.Info("get ip address", "address", ipAddr)
	addr := net.ParseIP(ipAddr)
	if addr == nil {
		d.sendErr(fmt.Errorf("invalid IP from EL: %s", ipAddr))
		return
	}
	d.sendAddr(addr)
}

func (d *VpnDelegate) sendErr(err error) {
	if d == nil || err == nil || d.stopped.Load() {
		return
	}
	d.ErrCh <- err
}

func (d *VpnDelegate) sendAddr(addr net.IP) {
	if d == nil || addr == nil || d.stopped.Load() {
		return
	}
	d.AddrCh <- addr
}

func SetupEL(cfg *ELConfig, delegate *VpnDelegate) {
	if delegate == nil {
		return
	}
	// We intentionally panic on missing required values earlier so failures are
	// loud during startup rather than surfacing deep in the networking stack.
	elLog.Info("SetupEL arg", "cfg.ServerAddr", cfg.ServerAddr)
	elLog.Info("SetupEL arg", "cfg.ServerPort", cfg.ServerPort)

	vc := cfg.HolderVC
	vcPrivKey := cfg.HolderPrivKey
	issuerPubkey := cfg.IssuerPubKey

	vpnHost := cfg.ServerAddr
	vpnPort := strconv.Itoa(cfg.ServerPort)

	antiOverlap := cfg.AntiOverlap

	var capturePath *string
	if cfg.CapturePath != "" {
		capturePath = &cfg.CapturePath
	}

	vpnKeepAliveSec := uint64(60)
	vpnTimeoutSec := uint64(180)
	vpnConnectionTimeoutSec := cfg.ConnectionTimeout

	vpnCfg := el_stack.NewElStackVpnConfig(
		vpnHost, vpnPort, antiOverlap,
		vpnTimeoutSec, vpnConnectionTimeoutSec, vpnKeepAliveSec,
		el_stack.ElStackVpnConnectionTypeQuic,
	)

	productName := "go-ethereum-el"
	productVersion := "0.1.0"
	productPlatform := "Linux"

	prodCfg := el_stack.NewElStackProductConfig(productName, productVersion, productPlatform, cfg.ServerCACert)

	// default:
	// tcpBuffSize := uint64(16384)
	// udpBuffSize := uint64(8192)
	// udpMetaSize := uint64(32)
	// buffCfg := el_stack.NewElStackSocketBufferConfig(1024, nil, nil, nil)
	// todo: reserch to default android default tcp/udp buffer statuses
	// AndroidOS:
	// tcpBuffSize := uint64(131072)
	// udpBuffSize := uint64(212992)
	// udpMetaSize := uint64(32)
	// iOS:
	// tcpBuffSize := uint64(65536)
	// udpBuffSize := uint64(65536)
	// udpMetaSize := uint64(32)
	// maxBurstSize := uint64(1024)
	// tcpBuffSize := uint64(65536)
	// udpBuffSize := uint64(65536)
	// udpMetaSize := uint64(2048)
	// NewElStackSocketBufferConfig was deleted from el_stack.go on feature-change-tcpip
	// buffCfg := el_stack.NewElStackSocketBufferConfig(maxBurstSize, &tcpBuffSize, &udpBuffSize, &udpMetaSize)
	
	vcCfg := el_stack.NewElStackVcConfig(vc, vcPrivKey, issuerPubkey)
	runtimeCfg := el_stack.NewDefaultElStackRuntimeConfig()

	// el_stack.Initialize(prodCfg, buffCfg)
	el_stack.Initialize(prodCfg, runtimeCfg)

	if err := el_stack.Start(delegate, vpnCfg, vcCfg, capturePath); err != nil {
		el_stack.Stop()
		elLog.Error("SetupEL ERROR", "err", err)
		delegate.sendErr(err)
		return
	}

}

func StopEL(delegate *VpnDelegate) {
	if delegate == nil {
		return
	}
	if delegate.stopped.Swap(true) {
		return
	}
	delegate.doneOnce.Do(func() {
		close(delegate.done)
	})
	el_stack.Stop()
}

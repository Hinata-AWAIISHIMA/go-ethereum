// Copyright 2014 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

// Package p2p implements the Ethereum p2p network protocols.
package p2p

import (
	"bytes"
	"cmp"
	"crypto/ecdsa"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/mclock"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/p2p/discover"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
	"github.com/ethereum/go-ethereum/p2p/netutil"
	"golang.org/x/exp/slices"
)

const (
	defaultDialTimeout = 15 * time.Second

	// This is the fairness knob for the discovery mixer. When looking for peers, we'll
	// wait this long for a single source of candidates before moving on and trying other
	// sources.
	discmixTimeout = 5 * time.Second

	// Connectivity defaults.
	defaultMaxPendingPeers = 50
	defaultDialRatio       = 3

	// This time limits inbound connection attempts per source IP.
	inboundThrottleTime = 30 * time.Second

	// Maximum time allowed for reading a complete message.
	// This is effectively the amount of time a connection can be idle.
	frameReadTimeout = 30 * time.Second

	// Maximum amount of time allowed for writing a complete message.
	frameWriteTimeout = 20 * time.Second
)

var (
	errServerStopped     = errors.New("server stopped")
	errEncHandshakeError = errors.New("rlpx enc error")
)

type protoHandshakeError struct{ err error }

func (e *protoHandshakeError) Error() string { return fmt.Sprintf("rlpx proto error: %v", e.err) }
func (e *protoHandshakeError) Unwrap() error { return e.err }

// Server manages all peer connections.
type Server struct {
	// Config fields may not be modified while the server is running.
	Config

	// Hooks for testing. These are useful because we can inhibit
	// the whole protocol stack.
	newTransport func(net.Conn, *ecdsa.PublicKey) transport
	newPeerHook  func(*Peer)
	listenFunc   func(network, addr string) (net.Listener, error)

	lock    sync.Mutex // protects running
	running bool

	listener net.Listener
	// ADDED by Hinata AWAIISHIMA BEG (research: IPv4/IPv6 dualstack)
	listeners        []net.Listener
	listenConfigAddr string
	tcpListenPort    int
	tcpListenPort6   int
	// ADDED by Hinata AWAIISHIMA END (research: IPv4/IPv6 dualstack)
	ourHandshake *protoHandshake
	loopWG       sync.WaitGroup // loop, listenLoop
	peerFeed     event.Feed
	log          log.Logger

	nodedb    *enode.DB
	localnode *enode.LocalNode
	discv4    *discover.UDPv4
	discv5    *discover.UDPv5
	discmix   *enode.FairMix
	dialsched *dialScheduler

	// This is read by the NAT port mapping loop.
	portMappingRegister chan *portMapping

	// Channels into the run loop.
	quit                    chan struct{}
	addtrusted              chan *enode.Node
	removetrusted           chan *enode.Node
	peerOp                  chan peerOpFunc
	peerOpDone              chan struct{}
	delpeer                 chan peerDrop
	checkpointPostHandshake chan *conn
	checkpointAddPeer       chan *conn

	// State of run loop and listenLoop.
	inboundHistory expHeap
}

type peerOpFunc func(map[enode.ID]*Peer)

type peerDrop struct {
	*Peer
	err       error
	requested bool // true if signaled by the peer
}

type connFlag int32

const (
	dynDialedConn connFlag = 1 << iota
	staticDialedConn
	inboundConn
	trustedConn
)

// conn wraps a network connection with information gathered
// during the two handshakes.
type conn struct {
	fd net.Conn
	transport
	node  *enode.Node
	flags connFlag
	cont  chan error // The run loop uses cont to signal errors to SetupConn.
	caps  []Cap      // valid after the protocol handshake
	name  string     // valid after the protocol handshake
}

type transport interface {
	// The two handshakes.
	doEncHandshake(prv *ecdsa.PrivateKey) (*ecdsa.PublicKey, error)
	doProtoHandshake(our *protoHandshake) (*protoHandshake, error)
	// The MsgReadWriter can only be used after the encryption
	// handshake has completed. The code uses conn.id to track this
	// by setting it to a non-nil value after the encryption handshake.
	MsgReadWriter
	// transports must provide Close because we use MsgPipe in some of
	// the tests. Closing the actual network connection doesn't do
	// anything in those tests because MsgPipe doesn't use it.
	close(err error)
}

func (c *conn) String() string {
	s := c.flags.String()
	if (c.node.ID() != enode.ID{}) {
		s += " " + c.node.ID().String()
	}
	s += " " + c.fd.RemoteAddr().String()
	return s
}

func (f connFlag) String() string {
	s := ""
	if f&trustedConn != 0 {
		s += "-trusted"
	}
	if f&dynDialedConn != 0 {
		s += "-dyndial"
	}
	if f&staticDialedConn != 0 {
		s += "-staticdial"
	}
	if f&inboundConn != 0 {
		s += "-inbound"
	}
	if s != "" {
		s = s[1:]
	}
	return s
}

func (c *conn) is(f connFlag) bool {
	flags := connFlag(atomic.LoadInt32((*int32)(&c.flags)))
	return flags&f != 0
}

func (c *conn) set(f connFlag, val bool) {
	for {
		oldFlags := connFlag(atomic.LoadInt32((*int32)(&c.flags)))
		flags := oldFlags
		if val {
			flags |= f
		} else {
			flags &= ^f
		}
		if atomic.CompareAndSwapInt32((*int32)(&c.flags), int32(oldFlags), int32(flags)) {
			return
		}
	}
}

// LocalNode returns the local node record.
func (srv *Server) LocalNode() *enode.LocalNode {
	return srv.localnode
}

// Peers returns all connected peers.
func (srv *Server) Peers() []*Peer {
	var ps []*Peer
	srv.doPeerOp(func(peers map[enode.ID]*Peer) {
		for _, p := range peers {
			ps = append(ps, p)
		}
	})
	return ps
}

// PeerCount returns the number of connected peers.
func (srv *Server) PeerCount() int {
	var count int
	srv.doPeerOp(func(ps map[enode.ID]*Peer) {
		count = len(ps)
	})
	return count
}

// AddPeer adds the given node to the static node set. When there is room in the peer set,
// the server will connect to the node. If the connection fails for any reason, the server
// will attempt to reconnect the peer.
func (srv *Server) AddPeer(node *enode.Node) {
	srv.dialsched.addStatic(node)
}

// RemovePeer removes a node from the static node set. It also disconnects from the given
// node if it is currently connected as a peer.
//
// This method blocks until all protocols have exited and the peer is removed. Do not use
// RemovePeer in protocol implementations, call Disconnect on the Peer instead.
func (srv *Server) RemovePeer(node *enode.Node) {
	var (
		ch  chan *PeerEvent
		sub event.Subscription
	)
	// Disconnect the peer on the main loop.
	srv.doPeerOp(func(peers map[enode.ID]*Peer) {
		srv.dialsched.removeStatic(node)
		if peer := peers[node.ID()]; peer != nil {
			ch = make(chan *PeerEvent, 1)
			sub = srv.peerFeed.Subscribe(ch)
			peer.Disconnect(DiscRequested)
		}
	})
	// Wait for the peer connection to end.
	if ch != nil {
		defer sub.Unsubscribe()
		for ev := range ch {
			if ev.Peer == node.ID() && ev.Type == PeerEventTypeDrop {
				return
			}
		}
	}
}

// AddTrustedPeer adds the given node to a reserved trusted list which allows the
// node to always connect, even if the slot are full.
func (srv *Server) AddTrustedPeer(node *enode.Node) {
	select {
	case srv.addtrusted <- node:
	case <-srv.quit:
	}
}

// RemoveTrustedPeer removes the given node from the trusted peer set.
func (srv *Server) RemoveTrustedPeer(node *enode.Node) {
	select {
	case srv.removetrusted <- node:
	case <-srv.quit:
	}
}

// SubscribeEvents subscribes the given channel to peer events
func (srv *Server) SubscribeEvents(ch chan *PeerEvent) event.Subscription {
	return srv.peerFeed.Subscribe(ch)
}

// Self returns the local node's endpoint information.
func (srv *Server) Self() *enode.Node {
	srv.lock.Lock()
	ln := srv.localnode
	srv.lock.Unlock()

	if ln == nil {
		return enode.NewV4(&srv.PrivateKey.PublicKey, net.ParseIP("0.0.0.0"), 0, 0)
	}
	return ln.Node()
}

// DiscoveryV4 returns the discovery v4 instance, if configured.
func (srv *Server) DiscoveryV4() *discover.UDPv4 {
	return srv.discv4
}

// DiscoveryV5 returns the discovery v5 instance, if configured.
func (srv *Server) DiscoveryV5() *discover.UDPv5 {
	return srv.discv5
}

// Stop terminates the server and all active peer connections.
// It blocks until all active connections have been closed.
func (srv *Server) Stop() {
	srv.lock.Lock()
	if !srv.running {
		srv.lock.Unlock()
		return
	}
	srv.running = false
	// MODIFIED by Hinata AWAIISHIMA (research: IPv4/IPv6 dualstack)
	// if srv.listener != nil {
	// 	// this unblocks listener Accept
	// 	srv.listener.Close()
	// }
	for _, listener := range srv.listeners {
		// this unblocks listener Accept
		listener.Close()
	}
	close(srv.quit)
	srv.lock.Unlock()
	srv.loopWG.Wait()
}

// sharedUDPConn implements a shared connection. Write sends messages to the underlying connection while read returns
// messages that were found unprocessable and sent to the unhandled channel by the primary listener.
type sharedUDPConn struct {
	// MODIFIED by Hinata AWAIISHIMA (research: IPv4/IPv6 dualstack)
	// net.UDPConn
	discover.UDPConn
	unhandled chan discover.ReadPacket
}

// ReadFromUDPAddrPort implements discover.UDPConn
func (s *sharedUDPConn) ReadFromUDPAddrPort(b []byte) (n int, addr netip.AddrPort, err error) {
	packet, ok := <-s.unhandled
	if !ok {
		return 0, netip.AddrPort{}, errors.New("connection was closed")
	}
	l := len(packet.Data)
	if l > len(b) {
		l = len(b)
	}
	copy(b[:l], packet.Data[:l])
	return l, packet.Addr, nil
}

// Close implements discover.UDPConn
func (s *sharedUDPConn) Close() error {
	return nil
}

// ADDED by Hinata AWAIISHIMA (research: IPv4/IPv6 dualstack)
type udpListenConn struct {
	network string
	conn    *net.UDPConn
}

// ADDED by Hinata AWAIISHIMA (research: IPv4/IPv6 dualstack)
type dualStackUDPConn struct {
	conns     []udpListenConn
	readCh    chan discover.ReadPacket
	closeCh   chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup
}

// ADDED by Hinata AWAIISHIMA (research: IPv4/IPv6 dualstack)
func newDualStackUDPConn(conns []udpListenConn) *dualStackUDPConn {
	c := &dualStackUDPConn{
		conns:   conns,
		readCh:  make(chan discover.ReadPacket, len(conns)*2),
		closeCh: make(chan struct{}),
	}
	for _, uc := range conns {
		c.wg.Add(1)
		go c.readLoop(uc)
	}
	return c
}

// ADDED by Hinata AWAIISHIMA (research: IPv4/IPv6 dualstack)
func (c *dualStackUDPConn) readLoop(uc udpListenConn) {
	defer c.wg.Done()
	var buf [1280]byte
	for {
		n, addr, err := uc.conn.ReadFromUDPAddrPort(buf[:])
		if err != nil {
			return
		}
		packet := make([]byte, n)
		copy(packet, buf[:n])
		select {
		case c.readCh <- discover.ReadPacket{Addr: addr, Data: packet}:
		case <-c.closeCh:
			return
		}
	}
}

// ADDED by Hinata AWAIISHIMA (research: IPv4/IPv6 dualstack)
func (c *dualStackUDPConn) ReadFromUDPAddrPort(b []byte) (n int, addr netip.AddrPort, err error) {
	packet, ok := <-c.readCh
	if !ok {
		return 0, netip.AddrPort{}, errors.New("connection was closed")
	}
	n = copy(b, packet.Data)
	return n, packet.Addr, nil
}

// ADDED by Hinata AWAIISHIMA (research: IPv4/IPv6 dualstack)
func (c *dualStackUDPConn) WriteToUDPAddrPort(b []byte, addr netip.AddrPort) (n int, err error) {
	ip := addr.Addr()
	if ip.Is4In6() {
		ip = netip.AddrFrom4(ip.As4())
		addr = netip.AddrPortFrom(ip, addr.Port())
	}
	for _, uc := range c.conns {
		switch {
		case ip.Is4() && uc.network == "udp4":
			return uc.conn.WriteToUDPAddrPort(b, addr)
		case ip.Is6() && !ip.Is4In6() && uc.network == "udp6":
			return uc.conn.WriteToUDPAddrPort(b, addr)
		}
	}
	return 0, fmt.Errorf("no UDP listener for %v", addr)
}

// ADDED by Hinata AWAIISHIMA (research: IPv4/IPv6 dualstack)
func (c *dualStackUDPConn) Close() error {
	var firstErr error
	c.closeOnce.Do(func() {
		close(c.closeCh)
		for _, uc := range c.conns {
			if err := uc.conn.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		c.wg.Wait()
		close(c.readCh)
	})
	return firstErr
}

// ADDED by Hinata AWAIISHIMA (research: IPv4/IPv6 dualstack)
func (c *dualStackUDPConn) LocalAddr() net.Addr {
	if len(c.conns) == 0 {
		return nil
	}
	return c.conns[0].conn.LocalAddr()
}

// Start starts running the server.
// Servers can not be re-used after stopping.
func (srv *Server) Start() (err error) {
	srv.lock.Lock()
	defer srv.lock.Unlock()
	if srv.running {
		return errors.New("server already running")
	}
	srv.running = true
	srv.log = srv.Logger
	if srv.log == nil {
		srv.log = log.Root()
	}
	if srv.clock == nil {
		srv.clock = mclock.System{}
	}
	if srv.NoDial && srv.ListenAddr == "" {
		srv.log.Warn("P2P server will be useless, neither dialing nor listening")
	}

	// static fields
	if srv.PrivateKey == nil {
		return errors.New("Server.PrivateKey must be set to a non-nil key")
	}
	if srv.newTransport == nil {
		srv.newTransport = newRLPX
	}
	if srv.listenFunc == nil {
		srv.listenFunc = net.Listen
	}
	// ADDED by Hinata AWAIISHIMA (research: IPv4/IPv6 dualstack)
	srv.listenConfigAddr = srv.ListenAddr
	srv.quit = make(chan struct{})
	srv.delpeer = make(chan peerDrop)
	srv.checkpointPostHandshake = make(chan *conn)
	srv.checkpointAddPeer = make(chan *conn)
	srv.addtrusted = make(chan *enode.Node)
	srv.removetrusted = make(chan *enode.Node)
	srv.peerOp = make(chan peerOpFunc)
	srv.peerOpDone = make(chan struct{})

	if err := srv.setupLocalNode(); err != nil {
		return err
	}
	srv.setupPortMapping()

	if srv.ListenAddr != "" {
		if err := srv.setupListening(); err != nil {
			return err
		}
	}
	if err := srv.setupDiscovery(); err != nil {
		return err
	}
	srv.setupDialScheduler()

	srv.loopWG.Add(1)
	go srv.run()
	return nil
}

func (srv *Server) setupLocalNode() error {
	// Create the devp2p handshake.
	pubkey := crypto.FromECDSAPub(&srv.PrivateKey.PublicKey)
	srv.ourHandshake = &protoHandshake{Version: baseProtocolVersion, Name: srv.Name, ID: pubkey[1:]}
	for _, p := range srv.Protocols {
		srv.ourHandshake.Caps = append(srv.ourHandshake.Caps, p.cap())
	}
	slices.SortFunc(srv.ourHandshake.Caps, Cap.Cmp)

	// Create the local node.
	db, err := enode.OpenDB(srv.NodeDatabase)
	if err != nil {
		return err
	}
	srv.nodedb = db
	srv.localnode = enode.NewLocalNode(db, srv.PrivateKey)
	// Keep a loopback fallback for IPv4 until discovery or NAT learns a better
	// advertised endpoint. IPv6 should similarly converge through endpoint prediction
	// or an explicit/static address and is not derived from wildcard listen addresses.
	srv.localnode.SetFallbackIP(net.IP{127, 0, 0, 1})
	// TODO: check conflicts
	for _, p := range srv.Protocols {
		for _, e := range p.Attributes {
			srv.localnode.Set(e)
		}
	}
	return nil
}

func (srv *Server) setupDiscovery() error {
	srv.discmix = enode.NewFairMix(discmixTimeout)

	// Don't listen on UDP endpoint if DHT is disabled.
	if srv.NoDiscovery {
		return nil
	}
	conn, err := srv.setupUDPListening()
	if err != nil {
		return err
	}

	var (
		sconn     discover.UDPConn = conn
		unhandled chan discover.ReadPacket
	)
	// If both versions of discovery are running, setup a shared
	// connection, so v5 can read unhandled messages from v4.
	if srv.Config.DiscoveryV4 && srv.Config.DiscoveryV5 {
		unhandled = make(chan discover.ReadPacket, 100)
		sconn = &sharedUDPConn{conn, unhandled}
	}

	// Start discovery services.
	if srv.Config.DiscoveryV4 {
		cfg := discover.Config{
			PrivateKey:  srv.PrivateKey,
			NetRestrict: srv.NetRestrict,
			Bootnodes:   srv.BootstrapNodes,
			Unhandled:   unhandled,
			Log:         srv.log,
		}
		ntab, err := discover.ListenV4(conn, srv.localnode, cfg)
		if err != nil {
			return err
		}
		srv.discv4 = ntab
		srv.discmix.AddSource(ntab.RandomNodes())
	}
	if srv.Config.DiscoveryV5 {
		cfg := discover.Config{
			PrivateKey:  srv.PrivateKey,
			NetRestrict: srv.NetRestrict,
			Bootnodes:   srv.BootstrapNodesV5,
			Log:         srv.log,
		}
		srv.discv5, err = discover.ListenV5(sconn, srv.localnode, cfg)
		if err != nil {
			return err
		}
	}

	// Add protocol-specific discovery sources.
	added := make(map[string]bool)
	for _, proto := range srv.Protocols {
		if proto.DialCandidates != nil && !added[proto.Name] {
			srv.discmix.AddSource(proto.DialCandidates)
			added[proto.Name] = true
		}
	}
	return nil
}

func (srv *Server) setupDialScheduler() {
	config := dialConfig{
		self:           srv.localnode.ID(),
		maxDialPeers:   srv.MaxDialedConns(),
		maxActiveDials: srv.MaxPendingPeers,
		log:            srv.Logger,
		netRestrict:    srv.NetRestrict,
		dialer:         srv.Dialer,
		clock:          srv.clock,
	}
	if srv.discv4 != nil {
		config.resolver = srv.discv4
	}
	if config.dialer == nil {
		config.dialer = tcpDialer{&net.Dialer{Timeout: defaultDialTimeout}}
	}
	srv.dialsched = newDialScheduler(config, srv.discmix, srv.SetupConn)
	for _, n := range srv.StaticNodes {
		srv.dialsched.addStatic(n)
	}
}

func (srv *Server) MaxInboundConns() int {
	return srv.MaxPeers - srv.MaxDialedConns()
}

func (srv *Server) MaxDialedConns() (limit int) {
	if srv.NoDial || srv.MaxPeers == 0 {
		return 0
	}
	if srv.DialRatio == 0 {
		limit = srv.MaxPeers / defaultDialRatio
	} else {
		limit = srv.MaxPeers / srv.DialRatio
	}
	if limit == 0 {
		limit = 1
	}
	return limit
}

func networkForListenAddr(addr, defaultNetwork, ipv6Network string) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil || host == "" {
		return defaultNetwork
	}
	// IPv6 zone identifiers (e.g. fe80::1%eth0) are not accepted by ParseIP.
	if zoneIdx := strings.IndexByte(host, '%'); zoneIdx >= 0 {
		host = host[:zoneIdx]
	}
	ip := net.ParseIP(host)
	if ip != nil && ip.To4() == nil {
		return ipv6Network
	}
	return defaultNetwork
}

// ADDED by Hinata AWAIISHIMA (reseach: IPv4/IPv6 dualstack)
type listenEndpoint struct {
	network string
	addr    string
}

// ADDED by Hinata AWAIISHIMA (reseach: IPv4/IPv6 dualstack)
func listenEndpointsForAddr(addr string) []listenEndpoint {
	addr = strings.TrimSpace(addr)
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return []listenEndpoint{{network: networkForListenAddr(addr, "tcp", "tcp6"), addr: addr}}
	}
	if host == "" {
		return []listenEndpoint{
			{network: "tcp4", addr: net.JoinHostPort("0.0.0.0", port)},
			{network: "tcp6", addr: net.JoinHostPort("::", port)},
		}
	}

	// IPv6 zone identifiers (e.g. fe80::1%eth0) are not accepted by ParseIP.
	hostNoZone := host
	if zoneIdx := strings.IndexByte(hostNoZone, '%'); zoneIdx >= 0 {
		hostNoZone = hostNoZone[:zoneIdx]
	}
	ip := net.ParseIP(hostNoZone)
	if ip == nil {
		return []listenEndpoint{{network: networkForListenAddr(addr, "tcp", "tcp6"), addr: addr}}
	}
	if ip.IsUnspecified() {
		return []listenEndpoint{
			{network: "tcp4", addr: net.JoinHostPort("0.0.0.0", port)},
			{network: "tcp6", addr: net.JoinHostPort("::", port)},
		}
	}
	if ip.To4() != nil {
		return []listenEndpoint{{network: "tcp4", addr: addr}}
	}
	return []listenEndpoint{{network: "tcp6", addr: addr}}
}

// ADDED by Hinata AWAIISHIMA (research: IPv4/IPv6 dualstack)
func udpListenEndpointsForAddr(addr string) []listenEndpoint {
	addr = strings.TrimSpace(addr)
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return []listenEndpoint{{network: networkForListenAddr(addr, "udp", "udp6"), addr: addr}}
	}
	if host == "" {
		return []listenEndpoint{
			{network: "udp4", addr: net.JoinHostPort("0.0.0.0", port)},
			{network: "udp6", addr: net.JoinHostPort("::", port)},
		}
	}

	hostNoZone := host
	if zoneIdx := strings.IndexByte(hostNoZone, '%'); zoneIdx >= 0 {
		hostNoZone = hostNoZone[:zoneIdx]
	}
	ip := net.ParseIP(hostNoZone)
	if ip == nil {
		return []listenEndpoint{{network: networkForListenAddr(addr, "udp", "udp6"), addr: addr}}
	}
	if ip.IsUnspecified() {
		return []listenEndpoint{
			{network: "udp4", addr: net.JoinHostPort("0.0.0.0", port)},
			{network: "udp6", addr: net.JoinHostPort("::", port)},
		}
	}
	if ip.To4() != nil {
		return []listenEndpoint{{network: "udp4", addr: addr}}
	}
	return []listenEndpoint{{network: "udp6", addr: addr}}
}

// ADDED by Hinata AWAIISHIMA (reseach: IPv4/IPv6 dualstack)
func endpointPort(addr string) (int, error) {
	_, port, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(port)
}

// ADDED by Hinata AWAIISHIMA (reseach: IPv4/IPv6 dualstack)
func withEndpointPort(addr string, port int) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return addr
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

func (srv *Server) setupListening() error {
	// ADDED by Hinata AWAIISHIMA BEG (reseach: IPv4/IPv6 dualstack)
	// Launch listeners. For wildcard listen addresses, we attempt both families.
	endpoints := listenEndpointsForAddr(srv.ListenAddr)
	wantedPort, wantedPortErr := endpointPort(srv.ListenAddr)
	shareDynamicPort := wantedPortErr == nil && wantedPort == 0

	listeners := make([]net.Listener, 0, len(endpoints))
	var firstPort int
	for _, ep := range endpoints {
		addr := ep.addr
		if shareDynamicPort && firstPort != 0 {
			if p, err := endpointPort(addr); err == nil && p == 0 {
				addr = withEndpointPort(addr, firstPort)
			}
		}
		// MODIFIED by Hinata AWAIISHIMA BEG (research: IPv4/IPv6 dualstack)
		// listener, err := srv.listenFunc(network, srv.ListenAddr)
		listener, err := srv.listenFunc(ep.network, addr)
		if err != nil {
			// return err
			srv.log.Debug("TCP listener setup failed", "network", ep.network, "addr", addr, "err", err)
			continue
		}
		// MODIFIED by Hinata AWAIISHIMA END (research: IPv4/IPv6 dualstack)
		if tcp, isTCP := listener.Addr().(*net.TCPAddr); isTCP && firstPort == 0 {
			firstPort = tcp.Port
		}
		listeners = append(listeners, listener)
	}
	if len(listeners) == 0 {
		return fmt.Errorf("failed to start TCP listener on %q", srv.ListenAddr)
	}
	// MODIFIED by Hinata AWAIISHIMA BEG (reseach: IPv4/IPv6 dualstack)
	srv.listeners = listeners
	// srv.listener = listener
	srv.listener = listeners[0]
	// srv.ListenAddr = listener.Addr().String()
	srv.ListenAddr = listeners[0].Addr().String()
	// MODIFIED by Hinata AWAIISHIMA END (reseach: IPv4/IPv6 dualstack)

	// Update the local node record and map TCP listening ports if NAT is configured.
	var (
		tcp4Port   int
		tcp6Port   int
		mappedPort = make(map[int]struct{})
	)
	for _, listener := range listeners {
		tcp, isTCP := listener.Addr().(*net.TCPAddr)
		if !isTCP {
			continue
		}
		addr := netutil.IPToAddr(tcp.IP)
		switch {
		case addr.Is4():
			if tcp4Port == 0 {
				tcp4Port = tcp.Port
			}
		case addr.Is6() && !addr.Is4In6():
			if tcp6Port == 0 {
				tcp6Port = tcp.Port
			}
		}
		// MODIFIED by Hinata AWAIISHIMA (research: IPv4/IPv6 dualstack)
		// if !tcp.IP.IsLoopback() && !tcp.IP.IsPrivate() {
		if _, seen := mappedPort[tcp.Port]; !seen && !tcp.IP.IsLoopback() && !tcp.IP.IsPrivate() {
			mappedPort[tcp.Port] = struct{}{}
			srv.portMappingRegister <- &portMapping{
				protocol: "TCP",
				name:     "ethereum p2p",
				port:     tcp.Port,
			}
		}
	}
	if tcp4Port != 0 {
		srv.tcpListenPort = tcp4Port
		srv.localnode.Set(enr.TCP(tcp4Port))
	} else if tcp6Port != 0 {
		srv.tcpListenPort = tcp6Port
		// Keep tcp key populated for compatibility even in IPv6-only listen setups.
		srv.localnode.Set(enr.TCP(tcp6Port))
	}
	if tcp6Port != 0 {
		srv.tcpListenPort6 = tcp6Port
		srv.localnode.Set(enr.TCP6(tcp6Port))
	}

	// MODIFIED by Hinata AWAIISHIMA BEG (research: IPv4/IPv6 dualstack)
	for _, listener := range listeners {
		srv.loopWG.Add(1)
		go srv.listenLoop(listener)
	}
	// MODIFIED by Hinata AWAIISHIMA END (research: IPv4/IPv6 dualstack)
	// ADDED by Hinata AWAIISHIMA END (reseach: IPv4/IPv6 dualstack)
	return nil
}

// MODIFIED by Hinata AWAIISHIMA (research: IPv4/IPv6 dualstack)
// func (srv *Server) setupUDPListening() (net.UDPConn, error) {
func (srv *Server) setupUDPListening() (discover.UDPConn, error) {
	// MODIFIED by Hinata AWAIISHIMA (research: IPv4/IPv6)
	// listenAddr := srv.ListenAddr
	listenAddr := srv.listenConfigAddr

	// Use an alternate listening address for UDP if
	// a custom discovery address is configured.
	if srv.DiscAddr != "" {
		listenAddr = srv.DiscAddr
	}
	endpoints := udpListenEndpointsForAddr(listenAddr)
	wantedPort, wantedPortErr := endpointPort(listenAddr)
	shareDynamicPort := wantedPortErr == nil && wantedPort == 0
	var firstPort int
	if shareDynamicPort {
		switch {
		case srv.tcpListenPort != 0:
			firstPort = srv.tcpListenPort
		case srv.tcpListenPort6 != 0:
			firstPort = srv.tcpListenPort6
		}
	}

	var (
		conns      []udpListenConn
		mappedPort = make(map[int]struct{})
	)
	for _, ep := range endpoints {
		addr := ep.addr
		if shareDynamicPort && firstPort != 0 {
			if p, err := endpointPort(addr); err == nil && p == 0 {
				addr = withEndpointPort(addr, firstPort)
			}
		}
		udpAddr, err := net.ResolveUDPAddr(ep.network, addr)
		if err != nil {
			srv.log.Debug("UDP listener setup failed", "network", ep.network, "addr", addr, "err", err)
			continue
		}
		conn, err := net.ListenUDP(ep.network, udpAddr)
		if err != nil {
			srv.log.Debug("UDP listener setup failed", "network", ep.network, "addr", addr, "err", err)
			continue
		}
		laddr := conn.LocalAddr().(*net.UDPAddr)
		if firstPort == 0 {
			firstPort = laddr.Port
		}
		srv.log.Debug("UDP listener up", "network", ep.network, "addr", laddr)
		if _, seen := mappedPort[laddr.Port]; !seen && !laddr.IP.IsLoopback() && !laddr.IP.IsPrivate() {
			mappedPort[laddr.Port] = struct{}{}
			srv.portMappingRegister <- &portMapping{
				protocol: "UDP",
				name:     "ethereum peer discovery",
				port:     laddr.Port,
			}
		}
		conns = append(conns, udpListenConn{network: ep.network, conn: conn})
	}
	if len(conns) == 0 {
		return nil, fmt.Errorf("failed to start UDP listener on %q", listenAddr)
	}

	var udpPort int
	for _, uc := range conns {
		laddr := uc.conn.LocalAddr().(*net.UDPAddr)
		if udpPort == 0 {
			udpPort = laddr.Port
		}
	}
	srv.localnode.SetFallbackUDP(udpPort)

	if len(conns) == 1 {
		return conns[0].conn, nil
	}
	return newDualStackUDPConn(conns), nil
}

// doPeerOp runs fn on the main loop.
func (srv *Server) doPeerOp(fn peerOpFunc) {
	select {
	case srv.peerOp <- fn:
		<-srv.peerOpDone
	case <-srv.quit:
	}
}

// run is the main loop of the server.
func (srv *Server) run() {
	srv.log.Info("Started P2P networking", "self", srv.localnode.Node().URLv4())
	defer srv.loopWG.Done()
	defer srv.nodedb.Close()
	defer srv.discmix.Close()
	defer srv.dialsched.stop()

	var (
		peers        = make(map[enode.ID]*Peer)
		inboundCount = 0
		trusted      = make(map[enode.ID]bool, len(srv.TrustedNodes))
	)
	// Put trusted nodes into a map to speed up checks.
	// Trusted peers are loaded on startup or added via AddTrustedPeer RPC.
	for _, n := range srv.TrustedNodes {
		trusted[n.ID()] = true
	}

running:
	for {
		select {
		case <-srv.quit:
			// The server was stopped. Run the cleanup logic.
			break running

		case n := <-srv.addtrusted:
			// This channel is used by AddTrustedPeer to add a node
			// to the trusted node set.
			srv.log.Trace("Adding trusted node", "node", n)
			trusted[n.ID()] = true
			if p, ok := peers[n.ID()]; ok {
				p.rw.set(trustedConn, true)
			}

		case n := <-srv.removetrusted:
			// This channel is used by RemoveTrustedPeer to remove a node
			// from the trusted node set.
			srv.log.Trace("Removing trusted node", "node", n)
			delete(trusted, n.ID())
			if p, ok := peers[n.ID()]; ok {
				p.rw.set(trustedConn, false)
			}

		case op := <-srv.peerOp:
			// This channel is used by Peers and PeerCount.
			op(peers)
			srv.peerOpDone <- struct{}{}

		case c := <-srv.checkpointPostHandshake:
			// A connection has passed the encryption handshake so
			// the remote identity is known (but hasn't been verified yet).
			if trusted[c.node.ID()] {
				// Ensure that the trusted flag is set before checking against MaxPeers.
				c.flags |= trustedConn
			}
			// TODO: track in-progress inbound node IDs (pre-Peer) to avoid dialing them.
			c.cont <- srv.postHandshakeChecks(peers, inboundCount, c)

		case c := <-srv.checkpointAddPeer:
			// At this point the connection is past the protocol handshake.
			// Its capabilities are known and the remote identity is verified.
			err := srv.addPeerChecks(peers, inboundCount, c)
			if err == nil {
				// The handshakes are done and it passed all checks.
				p := srv.launchPeer(c)
				peers[c.node.ID()] = p
				srv.log.Debug("Adding p2p peer", "peercount", len(peers), "id", p.ID(), "conn", c.flags, "addr", p.RemoteAddr(), "name", p.Name())
				srv.dialsched.peerAdded(c)
				if p.Inbound() {
					inboundCount++
					serveSuccessMeter.Mark(1)
					activeInboundPeerGauge.Inc(1)
				} else {
					dialSuccessMeter.Mark(1)
					activeOutboundPeerGauge.Inc(1)
				}
				activePeerGauge.Inc(1)
			}
			c.cont <- err

		case pd := <-srv.delpeer:
			// A peer disconnected.
			d := common.PrettyDuration(mclock.Now() - pd.created)
			delete(peers, pd.ID())
			srv.log.Debug("Removing p2p peer", "peercount", len(peers), "id", pd.ID(), "duration", d, "req", pd.requested, "err", pd.err)
			srv.dialsched.peerRemoved(pd.rw)
			if pd.Inbound() {
				inboundCount--
				activeInboundPeerGauge.Dec(1)
			} else {
				activeOutboundPeerGauge.Dec(1)
			}
			activePeerGauge.Dec(1)
		}
	}

	srv.log.Trace("P2P networking is spinning down")

	// Terminate discovery. If there is a running lookup it will terminate soon.
	if srv.discv4 != nil {
		srv.discv4.Close()
	}
	if srv.discv5 != nil {
		srv.discv5.Close()
	}
	// Disconnect all peers.
	for _, p := range peers {
		p.Disconnect(DiscQuitting)
	}
	// Wait for peers to shut down. Pending connections and tasks are
	// not handled here and will terminate soon-ish because srv.quit
	// is closed.
	for len(peers) > 0 {
		p := <-srv.delpeer
		p.log.Trace("<-delpeer (spindown)")
		delete(peers, p.ID())
	}
}

func (srv *Server) postHandshakeChecks(peers map[enode.ID]*Peer, inboundCount int, c *conn) error {
	switch {
	case !c.is(trustedConn) && len(peers) >= srv.MaxPeers:
		return DiscTooManyPeers
	case !c.is(trustedConn) && c.is(inboundConn) && inboundCount >= srv.MaxInboundConns():
		return DiscTooManyPeers
	case peers[c.node.ID()] != nil:
		return DiscAlreadyConnected
	case c.node.ID() == srv.localnode.ID():
		return DiscSelf
	default:
		return nil
	}
}

func (srv *Server) addPeerChecks(peers map[enode.ID]*Peer, inboundCount int, c *conn) error {
	// Drop connections with no matching protocols.
	if len(srv.Protocols) > 0 && countMatchingProtocols(srv.Protocols, c.caps) == 0 {
		return DiscUselessPeer
	}
	// Repeat the post-handshake checks because the
	// peer set might have changed since those checks were performed.
	return srv.postHandshakeChecks(peers, inboundCount, c)
}

// listenLoop runs in its own goroutine and accepts
// inbound connections.
// MODIFIED by Hinata AWAIISHIMA (research: IPv4/IPv6 dualstack)
// func (srv *Server) listenLoop() {
func (srv *Server) listenLoop(listener net.Listener) {
	// srv.log.Debug("TCP listener up", "addr", srv.listener.Addr())
	srv.log.Debug("TCP listener up", "addr", listener.Addr())

	// The slots channel limits accepts of new connections.
	tokens := defaultMaxPendingPeers
	if srv.MaxPendingPeers > 0 {
		tokens = srv.MaxPendingPeers
	}
	slots := make(chan struct{}, tokens)
	for i := 0; i < tokens; i++ {
		slots <- struct{}{}
	}

	// Wait for slots to be returned on exit. This ensures all connection goroutines
	// are down before listenLoop returns.
	defer srv.loopWG.Done()
	defer func() {
		for i := 0; i < cap(slots); i++ {
			<-slots
		}
	}()

	for {
		// Wait for a free slot before accepting.
		<-slots

		var (
			fd      net.Conn
			err     error
			lastLog time.Time
		)
		for {
			// fd, err = srv.listener.Accept()
			fd, err = listener.Accept()
			if netutil.IsTemporaryError(err) {
				if time.Since(lastLog) > 1*time.Second {
					srv.log.Debug("Temporary read error", "err", err)
					lastLog = time.Now()
				}
				time.Sleep(time.Millisecond * 200)
				continue
			} else if err != nil {
				srv.log.Debug("Read error", "err", err)
				slots <- struct{}{}
				return
			}
			break
		}

		remoteIP := netutil.AddrAddr(fd.RemoteAddr())
		if err := srv.checkInboundConn(remoteIP); err != nil {
			srv.log.Debug("Rejected inbound connection", "addr", fd.RemoteAddr(), "err", err)
			fd.Close()
			slots <- struct{}{}
			continue
		}
		if remoteIP.IsValid() {
			fd = newMeteredConn(fd)
			serveMeter.Mark(1)
			srv.log.Trace("Accepted connection", "addr", fd.RemoteAddr())
		}
		go func() {
			srv.SetupConn(fd, inboundConn, nil)
			slots <- struct{}{}
		}()
	}
}

func (srv *Server) checkInboundConn(remoteIP netip.Addr) error {
	if !remoteIP.IsValid() {
		// This case happens for internal test connections without remote address.
		return nil
	}
	// Reject connections that do not match NetRestrict.
	if srv.NetRestrict != nil && !srv.NetRestrict.ContainsAddr(remoteIP) {
		return errors.New("not in netrestrict list")
	}
	// Reject Internet peers that try too often.
	now := srv.clock.Now()
	srv.inboundHistory.expire(now, nil)
	// MODIFIED by Jakub Pajek (mobile connectivity)
	//if !netutil.AddrIsLAN(remoteIP) && srv.inboundHistory.contains(remoteIP.String()) {
	if !netutil.AddrIsLAN(remoteIP) && !netutil.AddrIsMobileLAN(remoteIP) && srv.inboundHistory.contains(remoteIP.String()) {
		return errors.New("too many attempts")
	}
	srv.inboundHistory.add(remoteIP.String(), now.Add(inboundThrottleTime))
	return nil
}

// SetupConn runs the handshakes and attempts to add the connection
// as a peer. It returns when the connection has been added as a peer
// or the handshakes have failed.
func (srv *Server) SetupConn(fd net.Conn, flags connFlag, dialDest *enode.Node) error {
	c := &conn{fd: fd, flags: flags, cont: make(chan error)}
	if dialDest == nil {
		c.transport = srv.newTransport(fd, nil)
	} else {
		c.transport = srv.newTransport(fd, dialDest.Pubkey())
	}

	err := srv.setupConn(c, dialDest)
	if err != nil {
		if !c.is(inboundConn) {
			markDialError(err)
		}
		c.close(err)
	}
	return err
}

func (srv *Server) setupConn(c *conn, dialDest *enode.Node) error {
	// Prevent leftover pending conns from entering the handshake.
	srv.lock.Lock()
	running := srv.running
	srv.lock.Unlock()
	if !running {
		return errServerStopped
	}

	// If dialing, figure out the remote public key.
	if dialDest != nil {
		dialPubkey := new(ecdsa.PublicKey)
		if err := dialDest.Load((*enode.Secp256k1)(dialPubkey)); err != nil {
			err = fmt.Errorf("%w: dial destination doesn't have a secp256k1 public key", errEncHandshakeError)
			srv.log.Trace("Setting up connection failed", "addr", c.fd.RemoteAddr(), "conn", c.flags, "err", err)
			return err
		}
	}

	// Run the RLPx handshake.
	remotePubkey, err := c.doEncHandshake(srv.PrivateKey)
	if err != nil {
		srv.log.Trace("Failed RLPx handshake", "addr", c.fd.RemoteAddr(), "conn", c.flags, "err", err)
		return fmt.Errorf("%w: %v", errEncHandshakeError, err)
	}
	if dialDest != nil {
		c.node = dialDest
	} else {
		c.node = nodeFromConn(remotePubkey, c.fd)
	}
	clog := srv.log.New("id", c.node.ID(), "addr", c.fd.RemoteAddr(), "conn", c.flags)
	err = srv.checkpoint(c, srv.checkpointPostHandshake)
	if err != nil {
		clog.Trace("Rejected peer", "err", err)
		return err
	}

	// Run the capability negotiation handshake.
	phs, err := c.doProtoHandshake(srv.ourHandshake)
	if err != nil {
		clog.Trace("Failed p2p handshake", "err", err)
		return &protoHandshakeError{err: err}
	}
	if id := c.node.ID(); !bytes.Equal(crypto.Keccak256(phs.ID), id[:]) {
		clog.Trace("Wrong devp2p handshake identity", "phsid", hex.EncodeToString(phs.ID))
		return DiscUnexpectedIdentity
	}
	c.caps, c.name = phs.Caps, phs.Name
	err = srv.checkpoint(c, srv.checkpointAddPeer)
	if err != nil {
		clog.Trace("Rejected peer", "err", err)
		return err
	}

	return nil
}

func nodeFromConn(pubkey *ecdsa.PublicKey, conn net.Conn) *enode.Node {
	var ip net.IP
	var port int
	if tcp, ok := conn.RemoteAddr().(*net.TCPAddr); ok {
		ip = tcp.IP
		port = tcp.Port
	}
	return enode.NewV4(pubkey, ip, port, port)
}

// checkpoint sends the conn to run, which performs the
// post-handshake checks for the stage (posthandshake, addpeer).
func (srv *Server) checkpoint(c *conn, stage chan<- *conn) error {
	select {
	case stage <- c:
	case <-srv.quit:
		return errServerStopped
	}
	return <-c.cont
}

func (srv *Server) launchPeer(c *conn) *Peer {
	p := newPeer(srv.log, c, srv.Protocols)
	if srv.EnableMsgEvents {
		// If message events are enabled, pass the peerFeed
		// to the peer.
		p.events = &srv.peerFeed
	}
	go srv.runPeer(p)
	return p
}

// runPeer runs in its own goroutine for each peer.
func (srv *Server) runPeer(p *Peer) {
	if srv.newPeerHook != nil {
		srv.newPeerHook(p)
	}
	srv.peerFeed.Send(&PeerEvent{
		Type:          PeerEventTypeAdd,
		Peer:          p.ID(),
		RemoteAddress: p.RemoteAddr().String(),
		LocalAddress:  p.LocalAddr().String(),
	})

	// Run the per-peer main loop.
	remoteRequested, err := p.run()

	// Announce disconnect on the main loop to update the peer set.
	// The main loop waits for existing peers to be sent on srv.delpeer
	// before returning, so this send should not select on srv.quit.
	srv.delpeer <- peerDrop{p, err, remoteRequested}

	// Broadcast peer drop to external subscribers. This needs to be
	// after the send to delpeer so subscribers have a consistent view of
	// the peer set (i.e. Server.Peers() doesn't include the peer when the
	// event is received).
	srv.peerFeed.Send(&PeerEvent{
		Type:          PeerEventTypeDrop,
		Peer:          p.ID(),
		Error:         err.Error(),
		RemoteAddress: p.RemoteAddr().String(),
		LocalAddress:  p.LocalAddr().String(),
	})
}

// NodeInfo represents a short summary of the information known about the host.
type NodeInfo struct {
	ID    string `json:"id"`    // Unique node identifier (also the encryption key)
	Name  string `json:"name"`  // Name of the node, including client type, version, OS, custom data
	Enode string `json:"enode"` // Enode URL for adding this peer from remote peers
	ENR   string `json:"enr"`   // Ethereum Node Record
	IP    string `json:"ip"`    // IP address of the node
	Ports struct {
		Discovery int `json:"discovery"` // UDP listening port for discovery protocol
		Listener  int `json:"listener"`  // TCP listening port for RLPx
	} `json:"ports"`
	ListenAddr string                 `json:"listenAddr"`
	Protocols  map[string]interface{} `json:"protocols"`
}

// NodeInfo gathers and returns a collection of metadata known about the host.
func (srv *Server) NodeInfo() *NodeInfo {
	// Gather and assemble the generic node infos
	node := srv.Self()
	info := &NodeInfo{
		Name:       srv.Name,
		Enode:      node.URLv4(),
		ID:         node.ID().String(),
		IP:         node.IPAddr().String(),
		ListenAddr: srv.ListenAddr,
		Protocols:  make(map[string]interface{}),
	}
	info.Ports.Discovery = node.UDP()
	info.Ports.Listener = node.TCP()
	info.ENR = node.String()

	// Gather all the running protocol infos (only once per protocol type)
	for _, proto := range srv.Protocols {
		if _, ok := info.Protocols[proto.Name]; !ok {
			nodeInfo := interface{}("unknown")
			if query := proto.NodeInfo; query != nil {
				nodeInfo = proto.NodeInfo()
			}
			info.Protocols[proto.Name] = nodeInfo
		}
	}
	return info
}

// PeersInfo returns an array of metadata objects describing connected peers.
func (srv *Server) PeersInfo() []*PeerInfo {
	// Gather all the generic and sub-protocol specific infos
	infos := make([]*PeerInfo, 0, srv.PeerCount())
	for _, peer := range srv.Peers() {
		if peer != nil {
			infos = append(infos, peer.Info())
		}
	}
	// Sort the result array alphabetically by node identifier
	slices.SortFunc(infos, func(a, b *PeerInfo) int {
		return cmp.Compare(a.ID, b.ID)
	})

	return infos
}

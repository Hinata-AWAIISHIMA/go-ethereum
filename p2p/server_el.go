package p2p

import (
	"errors"
	"net"

	"github.com/ethereum/go-ethereum/p2p/elstack"
)

var unknownRetryPolicyError = errors.New("unknown EL retry policy is set")

func (srv *Server) setupEL() error {
	if srv.EL == nil || !srv.EL.Use {
		return nil
	}

	delegate := elstack.NewELStackVpnDelegate()
	errCh := make(chan error)
	finishedCh := make(chan struct{})

	// goroutine loop to handling chans send to delegate

	go func() {
		var finished bool
		for {
			select {
			case <-srv.quit:
				elstack.StopEL(delegate)
				return
			case <-delegate.Done():
				return
			case addr := <-delegate.AddrCh:
				if finished {
					continue
				}
				finished = srv.applyELBindings(addr, finishedCh)
			case err := <-delegate.ErrCh:
				if finished {
					continue
				}
				finished = srv.elErrorHandler(delegate, err, finishedCh, errCh)
			}
		}
	}()

	// call elstack SetupEL()
	go elstack.SetupEL(srv.EL, delegate)

	select {
	case <-srv.quit:
		elstack.StopEL(delegate)
		return nil
	case <-finishedCh:
		return nil
	case err := <-errCh:
		return err
	}
}

func (srv *Server) applyELBindings(addr net.IP, finished chan struct{}) bool {
	_, port, err := net.SplitHostPort(srv.ListenAddr)
	if err != nil {
		srv.log.Warn("applyELBindings failed", "err", err)
		return false
	}
	srv.localnode.SetStaticIP(addr) // update staticIP to el_stack IPAddr
	srv.ListenAddr = net.JoinHostPort(addr.String(), port)
	srv.listenFunc = elstack.ListenELTCP
	srv.Dialer = elstack.ElStackTcpDialer{Timeout: defaultDialTimeout}
	srv.listenUDPFunc = elstack.ListenELUDP
	close(finished)
	return true
}

func (srv *Server) elErrorHandler(delegate *elstack.VpnDelegate, err error, finished chan struct{}, errCh chan error) bool {
	switch srv.EL.RetryPolicy {
	case elstack.ELRetryPolicyRetry:
		return false
	case elstack.ELRetryPolicyFailFast:
		errCh <- err
	case elstack.ELRetryPolicyFallback:
		close(finished)
	default:
		srv.log.Error("unknown EL retry policy is set")
		errCh <- unknownRetryPolicyError
	}
	elstack.StopEL(delegate)
	return true
}

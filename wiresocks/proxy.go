package wiresocks

import (
	"context"
	"io"
	"log/slog"
	"net/netip"
	"time"

	"github.com/bepass-org/proxy/pkg/mixed"
	"github.com/bepass-org/proxy/pkg/statute"
	"github.com/bepass-org/warp-plus/wireguard/device"
	"github.com/bepass-org/warp-plus/wireguard/tun/netstack"
)

// VirtualTun stores a reference to netstack network and DNS configuration
type VirtualTun struct {
	Tnet      *netstack.Net
	SystemDNS bool
	Logger    *slog.Logger
	Dev       *device.Device
	Ctx       context.Context
}

// StartProxy spawns a socks5 server.
func (vt *VirtualTun) StartProxy(bindAddress netip.AddrPort) {
	proxy := mixed.NewProxy(
		mixed.WithBindAddress(bindAddress.String()),
		// TODO
		// mixed.WithLogger(vt.Logger),
		mixed.WithContext(vt.Ctx),
		mixed.WithUserHandler(func(request *statute.ProxyRequest) error {
			return vt.generalHandler(request)
		}),
	)
	go func() {
		_ = proxy.ListenAndServe()
	}()
	go func() {
		for {
			select {
			case <-vt.Ctx.Done():
				vt.Stop()
				return
			default:
				time.Sleep(500 * time.Millisecond)
			}
		}
	}()
}

func (vt *VirtualTun) generalHandler(req *statute.ProxyRequest) error {
	vt.Logger.Debug("handling request", "protocol", req.Network, "destination", req.Destination)
	conn, err := vt.Tnet.Dial(req.Network, req.Destination)
	if err != nil {
		return err
	}
	// Close the connections when this function exits
	defer conn.Close()
	defer req.Conn.Close()
	// Channel to notify when copy operation is done
	done := make(chan error, 1)
	// Copy data from req.Conn to conn
	go func() {
		_, err := io.Copy(conn, req.Conn)
		done <- err
	}()
	// Copy data from conn to req.Conn
	go func() {
		_, err := io.Copy(req.Conn, conn)
		done <- err
	}()
	// Wait for one of the copy operations to finish
	err = <-done
	if err != nil {
		vt.Logger.Warn(err.Error())
	}

	// Close connections and wait for the other copy operation to finish
	conn.Close()
	req.Conn.Close()
	<-done

	return nil
}

func (vt *VirtualTun) Stop() {
	if vt.Dev != nil {
		if err := vt.Dev.Down(); err != nil {
			vt.Logger.Warn(err.Error())
		}
	}
}

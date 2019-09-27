package importer

//
// Copied from https://github.com/gliderlabs/ssh/blob/master/tcpip.go and modified a bit.
//

import (
	"github.com/chirino/ssh"
	gossh "golang.org/x/crypto/ssh"
	"io"
	"log"
	"net"
	"strconv"
	"sync"
)

type remoteForwardRequest struct {
	BindAddr string
	BindPort uint32
}

type remoteForwardSuccess struct {
	BindPort uint32
}

type remoteForwardCancelRequest struct {
	BindAddr string
	BindPort uint32
}

type remoteForwardChannelData struct {
	DestAddr   string
	DestPort   uint32
	OriginAddr string
	OriginPort uint32
}

// ForwardedTCPHandler can be enabled by creating a ForwardedTCPHandler and
// adding the HandleSSHRequest callback to the server's RequestHandlers under
// tcpip-forward and cancel-tcpip-forward.
type ForwardedTCPHandler struct {
	forwards map[string]net.Listener
	sync.Mutex
}

func (h *ForwardedTCPHandler) HandleSSHRequest(ctx ssh.Context, srv *ssh.Server, req *gossh.Request) (bool, []byte) {
	h.Lock()
	if h.forwards == nil {
		h.forwards = make(map[string]net.Listener)
	}
	h.Unlock()
	conn := ctx.Value(ssh.ContextKeyConn).(*gossh.ServerConn)
	switch req.Type {
	case "tcpip-forward":
		var reqPayload remoteForwardRequest
		if err := gossh.Unmarshal(req.Payload, &reqPayload); err != nil {
			// TODO: log parse failure
			return false, []byte{}
		}
		if srv.ReversePortForwardingCallback == nil || !srv.ReversePortForwardingCallback(ctx, reqPayload.BindAddr, reqPayload.BindPort) {
			return false, []byte("port forwarding is disabled")
		}
		addr := net.JoinHostPort(reqPayload.BindAddr, strconv.Itoa(int(reqPayload.BindPort)))
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			// TODO: log listen failure
			return false, []byte{}
		}
		log.Println("importer:listening on ", addr)
		_, destPortStr, _ := net.SplitHostPort(ln.Addr().String())
		destPort, _ := strconv.Atoi(destPortStr)
		h.Lock()
		h.forwards[addr] = ln
		h.Unlock()
		go func() {
			<-ctx.Done()
			h.Lock()
			ln, ok := h.forwards[addr]
			h.Unlock()
			if ok {
				ln.Close()
			}
		}()
		go func() {
			for {
				localConn, err := ln.Accept()
				if err != nil {
					log.Println("importer:accept error", err)
					break
				}
				log.Println("importer:connected exporter on: ", addr)
				originAddr, orignPortStr, _ := net.SplitHostPort(localConn.RemoteAddr().String())
				originPort, _ := strconv.Atoi(orignPortStr)
				payload := gossh.Marshal(&remoteForwardChannelData{
					DestAddr:   reqPayload.BindAddr,
					DestPort:   uint32(destPort),
					OriginAddr: originAddr,
					OriginPort: uint32(originPort),
				})
				go func() {
					sshConn, reqs, err := conn.OpenChannel("forwarded-tcpip", payload)
					if err != nil {
						// TODO: log failure to open channel
						log.Println("importer:tunnel dial error:", err)
						localConn.Close()
						return
					}

					log.Println("importer:tunnel to exporter:tunnel connected")
					go gossh.DiscardRequests(reqs)
					go func() {
						defer sshConn.Close()
						defer localConn.Close()
						_, err := io.Copy(sshConn, localConn)
						if err != nil {
							log.Println("importer:tunnel -> exporter:tunnel closed. error: ", err)
						} else {
							log.Println("importer:tunnel -> exporter:tunnel closed.")
						}

					}()
					go func() {
						defer sshConn.Close()
						defer localConn.Close()
						_, err := io.Copy(localConn, sshConn)
						if err != nil {
							log.Println("importer:tunnel <- exporter:tunnel closed. error: ", err)
						} else {
							log.Println("importer:tunnel <- exporter:tunnel closed.")
						}
					}()
				}()
			}
			h.Lock()
			delete(h.forwards, addr)
			h.Unlock()
		}()
		return true, gossh.Marshal(&remoteForwardSuccess{uint32(destPort)})

	case "cancel-tcpip-forward":
		var reqPayload remoteForwardCancelRequest
		if err := gossh.Unmarshal(req.Payload, &reqPayload); err != nil {
			// TODO: log parse failure
			return false, []byte{}
		}
		addr := net.JoinHostPort(reqPayload.BindAddr, strconv.Itoa(int(reqPayload.BindPort)))
		h.Lock()
		ln, ok := h.forwards[addr]
		h.Unlock()
		if ok {
			log.Println("importer:disconnected exporter on: ", addr)
			ln.Close()
		}
		return true, nil
	default:
		return false, nil
	}
}

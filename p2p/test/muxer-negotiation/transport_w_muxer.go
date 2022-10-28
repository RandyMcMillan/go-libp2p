package muxernegotiation

import (
	"context"
	"net"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/core/sec"
	noise "github.com/libp2p/go-libp2p/p2p/security/noise"
	tls "github.com/libp2p/go-libp2p/p2p/security/tls"
)

const (
	Tls   = tls.ID
	Noise = noise.ID
)

type MuxerEntity struct {
	id    string
	trans network.Multiplexer
}

type TransportWithMuxer struct {
	transport     sec.SecureTransport
	selectedMuxer string
}

var _ sec.SecureTransport = &TransportWithMuxer{}

func New(key crypto.PrivKey, muxers []MuxerEntity, secType string) (*TransportWithMuxer, error) {
	protos := make([]protocol.ID, 0, len(muxers))
	for _, entity := range muxers {
		protos = append(protos, protocol.ID(entity.id))
	}
	var err error
	var tr sec.SecureTransport
	if secType == Tls {
		tr, err = tls.New(key, protos)
	} else if secType == Noise {
		tr, err = noise.New(key, protos)
	}

	if err != nil || tr == nil {
		return nil, err
	}
	return &TransportWithMuxer{
		transport: tr,
	}, nil
}

func (t *TransportWithMuxer) SecureOutbound(ctx context.Context, insecure net.Conn, p peer.ID) (sec.SecureConn, error) {
	secConn, err := t.transport.SecureOutbound(ctx, insecure, p)
	if err != nil {
		return nil, err
	}
	t.selectedMuxer = secConn.ConnState().NextProto
	return secConn, nil
}

func (t *TransportWithMuxer) SecureInbound(ctx context.Context, insecure net.Conn, p peer.ID) (sec.SecureConn, error) {
	secConn, err := t.transport.SecureInbound(ctx, insecure, p)
	if err != nil {
		return nil, err
	}
	t.selectedMuxer = secConn.ConnState().NextProto
	return secConn, nil
}
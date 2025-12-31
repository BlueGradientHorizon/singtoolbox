package parsers

import (
	"singboxtest/utils"

	"github.com/sagernet/sing-box/option"
)

type VLESSParser struct{}

func (p VLESSParser) ParseProfile(connURI string) (*ProxyProfile, error) {
	connURI, err := utils.TryFixURI(connURI)
	if err != nil {
		return nil, err
	}

	uri, addr, port, err := extractCommonURIData(connURI, "vless")
	if err != nil {
		return nil, err
	}

	params := uri.Query()

	flow := params.Get("flow")
	if flow == "xtls-rprx-vision-udp443" {
		flow = "xtls-rprx-vision"
	}

	TLSOptions, err := buildOutboundTLSOptions(params, "vless")
	if err != nil {
		return nil, err
	}

	transportOptions, err := buildV2RayTransportOptions(params, "vless")
	if err != nil {
		return nil, err
	}

	o := &option.Outbound{
		Type: "vless",
		Options: &option.VLESSOutboundOptions{
			ServerOptions: option.ServerOptions{
				Server:     addr,
				ServerPort: port,
			},
			UUID: uri.User.Username(),
			OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{
				TLS: TLSOptions,
			},
			Transport: transportOptions,
			Flow:      flow,
		},
	}

	return &ProxyProfile{
		Outbound: o,
		ConnURI:  connURI,
	}, nil
}

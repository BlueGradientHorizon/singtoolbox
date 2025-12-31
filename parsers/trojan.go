package parsers

import (
	"singtoolbox/utils"

	"github.com/sagernet/sing-box/option"
)

type TrojanParser struct{}

func (p TrojanParser) ParseProfile(connURI string) (*ProxyProfile, error) {
	connURI, err := utils.TryFixURI(connURI)
	if err != nil {
		return nil, err
	}

	url, addr, port, err := extractCommonURIData(connURI, "trojan")
	if err != nil {
		return nil, err
	}

	params := url.Query()

	password := url.User.Username()

	TLSOptions, err := buildOutboundTLSOptions(params, "trojan")
	if err != nil {
		return nil, err
	}

	transportOptions, err := buildV2RayTransportOptions(params, "trojan")
	if err != nil {
		return nil, err
	}

	o := &option.Outbound{
		Type: "trojan",
		Options: &option.TrojanOutboundOptions{
			ServerOptions: option.ServerOptions{
				Server:     addr,
				ServerPort: port,
			},
			Password: password,
			OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{
				TLS: TLSOptions,
			},
			Transport: transportOptions,
		},
	}

	return &ProxyProfile{
		Outbound: o,
		ConnURI:  connURI,
	}, nil
}

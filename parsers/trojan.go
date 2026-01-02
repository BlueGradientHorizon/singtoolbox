package parsers

import (
	"errors"

	"github.com/bluegradienthorizon/singtoolbox/utils"

	"github.com/sagernet/sing-box/option"
)

type TrojanParser struct{}

func (p TrojanParser) ParseProfile(connURI string) (*ProxyProfile, error) {
	connURI, err := utils.TryFixURI(connURI)
	if err != nil {
		return nil, errors.New("TrojanParser.ParseProfile: " + err.Error())
	}

	url, addr, port, err := extractCommonURIData(connURI, "trojan")
	if err != nil {
		return nil, errors.New("TrojanParser.ParseProfile: " + err.Error())
	}

	params := url.Query()

	password := url.User.Username()

	TLSOptions, err := buildOutboundTLSOptions(params, "trojan")
	if err != nil {
		return nil, errors.New("TrojanParser.ParseProfile: " + err.Error())
	}

	transportOptions, err := buildV2RayTransportOptions(params, "trojan")
	if err != nil {
		return nil, errors.New("TrojanParser.ParseProfile: " + err.Error())
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

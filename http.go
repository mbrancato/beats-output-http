package http

import (
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/elastic/beats/libbeat/beat"
	"github.com/elastic/beats/libbeat/common"
	"github.com/elastic/beats/libbeat/logp"
	"github.com/elastic/beats/libbeat/outputs"
)

func init() {
	// outputs.RegisterOutputPlugin("http", New)
	outputs.RegisterType("http", makeHTTP)
}

var (
	debugf = logp.MakeDebug("http")
)

var (
	// ErrNotConnected indicates failure due to client having no valid connection
	ErrNotConnected = errors.New("not connected")

	// ErrJSONEncodeFailed indicates encoding failures
	ErrJSONEncodeFailed = errors.New("json encode failed")
)

// func <name> (params) return params {}
// func <name> (params) ( return params) {}
// func (object *)<name> (params) ( return params) {}
// func (object *) <name> (params) return params {}

// func (out *httpOutput) init(cfg *common.Config) error {
func makeHTTP(
	beat beat.Info,
	observer outputs.Observer,
	cfg *common.Config,
) (outputs.Group, error) {
	config := defaultConfig
	logp.Info("Initializing HTTP output")

	if err := cfg.Unpack(&config); err != nil {
		return outputs.Fail(err)
	}

	tlsConfig, err := outputs.LoadTLSConfig(config.TLS)
	if err != nil {
		return outputs.Fail(err)
	}

	hosts, err := outputs.ReadHostList(cfg)
	if err != nil {
		return outputs.Fail(err)
	}

	//new client code

	proxyURL, err := parseProxyURL(config.ProxyURL)
	if err != nil {
		return outputs.Fail(err)
	}
	if proxyURL != nil {
		logp.Info("Using proxy URL: %s", proxyURL)
	}

	params := config.Params
	if len(params) == 0 {
		params = nil
	}

	clients := make([]outputs.NetworkClient, len(hosts))

	for i, host := range hosts {
		// esURL, err := common.MakeURL(config.Protocol, config.Path, host, 80)

		logp.Info("Making client for host: " + host)
		hostURL, err := getURL(config.Protocol, 80, config.Path, host)
		if err != nil {
			logp.Err("Invalid host param set: %s, Error: %v", host, err)
			return outputs.Fail(err)
		}

		logp.Info("Final host URL: " + hostURL)

		var client outputs.NetworkClient
		client, err = NewClient(ClientSettings{
			URL:              hostURL,
			Proxy:            proxyURL,
			TLS:              tlsConfig,
			Username:         config.Username,
			Password:         config.Password,
			Parameters:       params,
			Timeout:          config.Timeout,
			CompressionLevel: config.CompressionLevel,
			BatchPublish:     config.BatchPublish,
		})
		if err != nil {
			return outputs.Fail(err)
		}

		// TOOD: Backoff to be implemented
		client = outputs.WithBackoff(client, 1*time.Second, 60*time.Second)
		clients[i] = client
	}

	//end new client code

	return outputs.SuccessNet(config.LoadBalance, config.BatchSize, config.MaxRetries, clients)

	/*
		TODO: check the code. looks like no alternative
		maxRetries := config.MaxRetries
		maxAttempts := maxRetries + 1 // maximum number of send attempts (-1 = infinite)
		if maxRetries < 0 {
			maxAttempts = 0
		}

		var waitRetry = time.Duration(1) * time.Second
		var maxWaitRetry = time.Duration(60) * time.Second

		loadBalance := config.LoadBalance
		m, err := modeutil.NewConnectionMode(clients, modeutil.Settings{
			Failover:     !loadBalance,
			MaxAttempts:  maxAttempts,
			Timeout:      config.Timeout,
			WaitRetry:    waitRetry,
			MaxWaitRetry: maxWaitRetry,
		})
		if err != nil {
			return err
		}

		out.mode = m

		return nil*/
}

/*func makeClientFactory(
	tls *transport.TLSConfig,
	config *httpConfig,
	out *httpOutput,
) func(string) (mode.ProtocolClient, error) {
	logp.Info("Making client factory")
	return func(host string) (mode.ProtocolClient, error) {
		logp.Info("Making client for host" + host)
		hostURL, err := getURL(config.Protocol, 80, config.Path, host)
		if err != nil {
			logp.Err("Invalid host param set: %s, Error: %v", host, err)
			return nil, err
		}

		var proxyURL *url.URL
		if config.ProxyURL != "" {
			proxyURL, err = parseProxyURL(config.ProxyURL)
			if err != nil {
				return nil, err
			}

			logp.Info("Using proxy URL: %s", proxyURL)
		}

		params := config.Params
		if len(params) == 0 {
			params = nil
		}

		headers := make(map[string]string)
		fields := strings.Split(config.Headers, "|")
		if len(fields) % 2 == 0 {
			for index := 0; index < len(fields); index+=2  {
				headers[fields[index]] = fields[index+1]
			}
		}

		return NewClient(ClientSettings{
			URL:              hostURL,
			Proxy:            proxyURL,
			TLS:              tls,
			Username:         config.Username,
			Password:         config.Password,
			Parameters:       params,
			Timeout:          config.Timeout,
			CompressionLevel: config.CompressionLevel,
			headers:          headers,
		})
	}
}*/

func parseProxyURL(raw string) (*url.URL, error) {
	if raw == "" {
		return nil, nil
	}

	url, err := url.Parse(raw)
	if err == nil && strings.HasPrefix(url.Scheme, "http") {
		return url, err
	}

	// Proxy was bogus. Try prepending "http://" to it and
	// see if that parses correctly.
	return url.Parse("http://" + raw)
}

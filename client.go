package http

import (
	"encoding/json"
	"expvar"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"

	"github.com/elastic/beats/libbeat/beat"
	"github.com/elastic/beats/libbeat/common"

	"github.com/elastic/beats/libbeat/logp"
	"github.com/elastic/beats/libbeat/outputs"
	"github.com/elastic/beats/libbeat/outputs/outil"
	"github.com/elastic/beats/libbeat/outputs/transport"
	"github.com/elastic/beats/libbeat/publisher"
)

// Client struct
type Client struct {
	Connection
	tlsConfig *transport.TLSConfig
	params    map[string]string

	// additional configs
	compressionLevel int
	proxyURL         *url.URL
	batchPublish     bool
	observer         outputs.Observer
	headers          map[string]string
}

// ClientSettings struct
type ClientSettings struct {
	URL                string
	Proxy              *url.URL
	TLS                *transport.TLSConfig
	Username, Password string
	Parameters         map[string]string
	Index              outil.Selector
	Pipeline           *outil.Selector
	Timeout            time.Duration
	CompressionLevel   int
	Observer           outputs.Observer
	BatchPublish       bool
	headers            map[string]string
	ContentType        string
}

// Connection struct
type Connection struct {
	URL         string
	Username    string
	Password    string
	http        *http.Client
	connected   bool
	encoder     bodyEncoder
	ContentType string
}

type eventraw map[string]json.RawMessage

type event struct {
	Timestamp time.Time     `json:"@timestamp"`
	Fields    common.MapStr `json:"-"`
}

type batchEvents struct {
	Size   int     `json:"size" struct:"size"`
	Events []event `json:"events" struct:"events"`
}

// Metrics that can retrieved through the expvar web interface.
var (
	ackedEvents            = expvar.NewInt("libbeatHttpPublishedAndAckedEvents")
	eventsNotAcked         = expvar.NewInt("libbeatHttpPublishedButNotAckedEvents")
	publishEventsCallCount = expvar.NewInt("libbeatHttpPublishEventsCallCount")
	statReadBytes          = expvar.NewInt("libbeatHttpPublishReadBytes")
	statWriteBytes         = expvar.NewInt("libbeatHttpPublishWriteBytes")
	statReadErrors         = expvar.NewInt("libbeatHttpPublishReadErrors")
	statWriteErrors        = expvar.NewInt("libbeatHttpPublishWriteErrors")
)

// NewClient instantiate a client.
func NewClient(s ClientSettings) (*Client, error) {
	proxy := http.ProxyFromEnvironment
	if s.Proxy != nil {
		proxy = http.ProxyURL(s.Proxy)
	}

	logp.Info("Http url: %s", s.URL)

	// TODO: add socks5 proxy support
	var dialer, tlsDialer transport.Dialer
	var err error

	dialer = transport.NetDialer(s.Timeout)
	tlsDialer, err = transport.TLSDialer(dialer, s.TLS, s.Timeout)
	if err != nil {
		return nil, err
	}

	if st := s.Observer; st != nil {
		dialer = transport.StatsDialer(dialer, st)
		tlsDialer = transport.StatsDialer(tlsDialer, st)
	}

	params := s.Parameters

	var encoder bodyEncoder
	compression := s.CompressionLevel
	if compression == 0 {
		encoder = newJSONEncoder(nil)
	} else {
		encoder, err = newGzipEncoder(compression, nil)
		if err != nil {
			return nil, err
		}
	}

	client := &Client{
		Connection: Connection{
			URL:         s.URL,
			Username:    s.Username,
			Password:    s.Password,
			ContentType: s.ContentType,
			http: &http.Client{
				Transport: &http.Transport{
					Dial:    dialer.Dial,
					DialTLS: tlsDialer.Dial,
					Proxy:   proxy,
				},
				Timeout: s.Timeout,
			},
			encoder: encoder,
		},
		params: params,

		compressionLevel: compression,
		proxyURL:         s.Proxy,
		batchPublish:     s.BatchPublish,
		headers:          s.headers,
	}

	return client, nil
}

// Clone clones a client.
func (client *Client) Clone() *Client {
	// when cloning the connection callback and params are not copied. A
	// client's close is for example generated for topology-map support. With params
	// most likely containing the ingest node pipeline and default callback trying to
	// create install a template, we don't want these to be included in the clone.
	c, _ := NewClient(
		ClientSettings{
			URL:              client.URL,
			Proxy:            client.proxyURL,
			TLS:              client.tlsConfig,
			Username:         client.Username,
			Password:         client.Password,
			Parameters:       nil, // XXX: do not pass params?
			Timeout:          client.http.Timeout,
			CompressionLevel: client.compressionLevel,
			BatchPublish:     client.batchPublish,
			headers:          client.headers,
			ContentType:      client.ContentType,
		},
	)
	return c
}

// Connect establishes a connection to the clients sink.
func (conn *Connection) Connect() error {
	conn.connected = true
	return nil
}

// Close closes a connection.
func (conn *Connection) Close() error {
	conn.connected = false
	return nil
}

func (client *Client) String() string {
	return client.URL
}

// Publish sends events to the clients sink.
func (client *Client) Publish(batch publisher.Batch) error {
	events := batch.Events()
	rest, err := client.publishEvents(events)
	if len(rest) == 0 {
		batch.ACK()
	} else {
		batch.RetryEvents(rest)
	}
	return err
}

// PublishEvents posts all events to the http endpoint. On error a slice with all
// events not published will be returned.
func (client *Client) publishEvents(
	data []publisher.Event,
) ([]publisher.Event, error) {
	begin := time.Now()
	publishEventsCallCount.Add(1)

	if len(data) == 0 {
		return nil, nil
	}

	if !client.connected {
		return data, ErrNotConnected
	}

	// logp.Info("complete data set: ", data)

	var failedEvents []publisher.Event

	sendErr := error(nil)

	if client.batchPublish {
		//Publish events in bulk
		debugf("Publishing events in batch")
		sendErr = client.BatchPublishEvent(data)
		if sendErr != nil {
			return nil, sendErr
		}
	} else {
		for index, event := range data {
			sendErr = client.PublishEvent(event)
			if sendErr != nil {
				// return the rest of the data with the error
				failedEvents = data[index:]
				break
			}
		}
	}

	debugf("PublishEvents: %d metrics have been published over HTTP in %v.",
		len(data),
		time.Now().Sub(begin))

	ackedEvents.Add(int64(len(data) - len(failedEvents)))
	eventsNotAcked.Add(int64(len(failedEvents)))
	if len(failedEvents) > 0 {
		return failedEvents, sendErr
	}

	return nil, nil
}

// BatchPublishEvent publish a single event to output.
func (client *Client) BatchPublishEvent(data []publisher.Event) error {
	if !client.connected {
		return ErrNotConnected
	}

	// var events []event
	var events = make([]eventraw, len(data))

	// debugf("Publish event: %s", event)
	for i, event := range data {
		//make event
		// events = append(events, makeEvent(&event.Content))
		events[i] = makeEvent(&event.Content)
	}

	status, _, err := client.request("POST", "", client.params, events, client.headers)
	if err != nil {
		logp.Warn("Fail to insert a single event: %s", err)
		if err == ErrJSONEncodeFailed {
			// don't retry unencodable values
			return nil
		}
	}

	switch {
	case status == 500 || status == 400: //server error or bad input, don't retry
		return nil
	case status >= 300:
		// retry
		return err
	}

	return nil
}

// PublishEvent publish a single event to output.
func (client *Client) PublishEvent(data publisher.Event) error {
	if !client.connected {
		return ErrNotConnected
	}

	event := data

	// debugf("Publish event: %s", event)

	status, _, err := client.request("POST", "", client.params, makeEvent(&event.Content), client.headers)
	if err != nil {
		logp.Warn("Fail to insert a single event: %s", err)
		if err == ErrJSONEncodeFailed {
			// don't retry unencodable values
			return nil
		}
	}

	switch {
	case status == 500 || status == 400: //server error or bad input, don't retry
		return nil
	case status >= 300:
		// retry
		return err
	}

	if !client.connected {
		return ErrNotConnected
	}

	return nil
}

func (conn *Connection) request(
	method, path string,
	params map[string]string,
	body interface{},
	headers map[string]string,
) (int, []byte, error) {
	url := makeURL(conn.URL, path, "", params)
	// debugf("%s %s %v", method, url, body)

	if body == nil {
		return conn.execRequest(method, url, nil, headers)
	}

	if err := conn.encoder.Marshal(body); err != nil {
		logp.Warn("Failed to json encode body (%v): %#v", err, body)
		return 0, nil, ErrJSONEncodeFailed
	}
	return conn.execRequest(method, url, conn.encoder.Reader(), headers)
}

func (conn *Connection) execRequest(method, url string, body io.Reader, headers map[string]string) (int, []byte, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		logp.Warn("Failed to create request: %v", err)
		return 0, nil, err
	}
	if body != nil {
		conn.encoder.AddHeader(&req.Header)
	}
	return conn.execHTTPRequest(req, headers)
}

func (conn *Connection) execHTTPRequest(req *http.Request, headers map[string]string) (int, []byte, error) {
	req.Header.Add("Accept", "application/json")
	req.Header.Add("Content-Type", conn.ContentType)

	for key, value := range headers {
		req.Header.Add(key, value)
	}

	if conn.Username != "" || conn.Password != "" {
		req.SetBasicAuth(conn.Username, conn.Password)
	}

	resp, err := conn.http.Do(req)
	if err != nil {
		conn.connected = false
		return 0, nil, err
	}
	defer closing(resp.Body)

	status := resp.StatusCode
	if status >= 300 {
		conn.connected = false
		return status, nil, fmt.Errorf("%v", resp.Status)
	}

	obj, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		conn.connected = false
		return status, nil, err
	}
	return status, obj, nil
}

func closing(c io.Closer) {
	err := c.Close()
	if err != nil {
		logp.Warn("Close failed with: %v", err)
	}
}

//this should ideally be in enc.go
func makeEvent(v *beat.Event) map[string]json.RawMessage {
	// Inline not supported, HT: https://stackoverflow.com/questions/49901287/embed-mapstringstring-in-go-json-marshaling-without-extra-json-property-inlin
	type event0 event // prevent recursion

	e := event{Timestamp: v.Timestamp.UTC(), Fields: v.Fields}

	b, err := json.Marshal(event0(e))
	if err != nil {
		logp.Warn("Error encoding event to JSON: %v", err)
	}

	var eventmap map[string]json.RawMessage
	err = json.Unmarshal(b, &eventmap)
	if err != nil {
		logp.Warn("Error decoding JSON to map: %v", err)
	}

	// Add the individual fields to the map, flatten "Fields"
	for j, k := range e.Fields {
		b, err = json.Marshal(k)
		if err != nil {
			logp.Warn("Error encoding map to JSON: %v", err)
		}
		eventmap[j] = b
	}

	return eventmap
}

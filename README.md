beats-output-http
=================

Outputter for the Elastic Beats platform that simply
POSTs events to an HTTP endpoint.

[![Build Status](https://travis-ci.org/raboof/beats-output-http.svg?branch=master)](https://travis-ci.org/raboof/beats-output-http)

Usage
=====

To add support for this output plugin to a beat, you
have to import this plugin into your main beats package (elastic/beats/filebeat/main.go),
like this:

```
package main

import (
	"os"

	"github.com/elastic/beats/filebeat/cmd"
	_ "github.com/sudhircirra/beats-output-http"
)

func main() {
	if err := cmd.RootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

```

Then configure the http output plugin in filebeat.yaml:

```
output:
  http:
    hosts: ["localhost:8002"]
    protocol: "http"
    path: "test/v1"
    # parameters: "xyz"
    max_retries: -1
    timeout: 10s
#    tls:
#        enabled: false
      #  verification_mode: "full"
      #  supported_protocols: [...]
      #  cipher_suites: [...]
      #  curve_types: [...]
      #  certificate_authorities: [...]
      #  certificate: ...
      #  key: ...
      #  key_passphrase: ...

```

beats-output-http
=================

Outputter for the Elastic Beats platform that simply
POSTs events to an HTTP endpoint.

Usage
=====

To add support for this output plugin to a beat, you have to import this
plugin into your main beats package (e.g. elastic/beats/filebeat/main.go),
like this:

```
package main

import (
  "os"

  "github.com/elastic/beats/filebeat/cmd"
  _ "github.com/thefire/beats-output-http"
)

func main() {
  if err := cmd.RootCmd.Execute(); err != nil {
    os.Exit(1)
  }
}

```

Then configure the http output plugin in your beat config (e.g. filebeat.yml):

```
output:
  http:
    hosts: ["host.example.com"]
    protocol: "http"
    path: "messages"
```

More details of config options can be found in the [configuration_example.yml](https://github.com/thefire/beats-output-http/blob/master/configuration_example.yml) file.

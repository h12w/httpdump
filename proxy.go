package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"sync"
	"time"

	"h12.io/config"
	"h12.io/mitm"
	"h12.io/mitm/proxy"
)

// Config is global config struct
type Config struct {
	Port  int    `long:"port" description:"listening port" default:"2080"`
	Proxy string `long:"proxy" description:"upstream http proxy"`
}

func main() {
	var cfg Config
	if err := config.Parse(&cfg); err != nil {
		if _, ok := err.(*config.HelpError); ok {
			fmt.Println(err)
			return
		}
		log.Fatal(err)
	}
	certs, err := mitm.NewCertPool("cert")
	if err != nil {
		log.Fatal(err)
	}

	roundTripper := &http.Transport{
		// TEST: for local testing
		// Dial: socks.DialSocksProxy(socks.SOCKS5, "127.0.0.1:1080"),
		Dial: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).Dial,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	if cfg.Proxy != "" {
		upstreamProxy, err := url.Parse(cfg.Proxy)
		if err != nil {
			log.Fatal(err)
		}
		roundTripper.Proxy = http.ProxyURL(upstreamProxy)
	}

	proxy := proxy.New(certs, roundTripper)
	var d dumper
	proxy.ResponseFilter = d.dumpResponse
	http.ListenAndServe(":"+strconv.Itoa(cfg.Port), http.HandlerFunc(proxy.Serve))
}

type dumper struct {
	mu sync.Mutex
}

func (d *dumper) dumpResponse(resp *http.Response) {
	d.mu.Lock()
	defer d.mu.Unlock()
	req := resp.Request
	fmt.Println("----- ----- ----- ----- >>>>>")
	buf, _ := httputil.DumpRequest(req, true)
	fmt.Println(string(buf))

	fmt.Println("<<<<< ----- ----- ----- -----")
	buf, _ = httputil.DumpResponse(resp, needResponseBody(resp))
	fmt.Println(string(buf))
	fmt.Println("")
	fmt.Println("")
}
func needResponseBody(resp *http.Response) bool {
	switch resp.Header.Get("Content-Type") {
	case
		"application/octet-stream",
		"image/gif",
		"image/x-icon",
		"image/jpeg",
		"image/png",
		"font/woff":
		return false
	}
	return true
}

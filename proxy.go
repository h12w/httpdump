package main

import (
	"compress/gzip"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"net/http/httputil"

	"appcoachs.net/x/log"
	"h12.me/config"
	"h12.me/errors"
	"h12.me/mitm"
)

// Config is global config struct
type Config struct {
	Port  int    `long:"port" description:"listening port" default:"2080"`
	Proxy string `long:"proxy" description:"http proxy"`
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

	proxy := newProxy(certs, roundTripper)
	http.ListenAndServe(":"+strconv.Itoa(cfg.Port), http.HandlerFunc(proxy.serve))
}

type proxy struct {
	certs        *mitm.CertPool
	roundTripper http.RoundTripper
}

func newProxy(certs *mitm.CertPool, roundTripper http.RoundTripper) *proxy {
	fp := &proxy{
		certs:        certs,
		roundTripper: roundTripper,
	}

	return fp
}

func (p *proxy) serve(w http.ResponseWriter, req *http.Request) {
	if req.Method == "GET" {
		p.serveHTTP(w, req)
	} else if req.Method == "CONNECT" {
		err := p.certs.ServeHTTPS(w, req, p.serveHTTP)
		if err != nil {
			log.Error(errors.Wrap(err))
		}
	}
}

// Hop-by-hop headers. These are removed when sent to the backend.
// http://www.w3.org/Protocols/rfc2616/rfc2616-sec13.html
var hopHeaders = []string{
	"Connection",
	"Proxy-Connection", // non-standard but still sent by libcurl and rejected by e.g. google
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Te",      // canonicalized version of "TE"
	"Trailer", // not Trailers per URL above; http://www.rfc-editor.org/errata_search.php?eid=4522
	"Transfer-Encoding",
	"Upgrade",
}

func (p *proxy) serveHTTP(w http.ResponseWriter, req *http.Request) {
	req.RequestURI = ""
	resp, err := p.roundTripper.RoundTrip(req)
	if err != nil {
		log.Error(errors.Wrap(err))
		return
	}

	for _, h := range hopHeaders {
		resp.Header.Del(h)
	}

	if resp.Header.Get("Content-Encoding") == "gzip" {
		body, err := newGzipReadCloser(resp.Body)
		if err == nil {
			resp.Body = body
			resp.ContentLength = -1
			resp.Header.Del("Content-Encoding")
			resp.Header.Del("Content-Length")
		}
	}
	defer resp.Body.Close()
	fmt.Println("----- ------ ----- ----- ----- -----")
	buf, _ := httputil.DumpRequest(req, true)
	fmt.Println(string(buf))
	buf, _ = httputil.DumpResponse(resp, needResponseBody(resp))
	fmt.Println(string(buf))
	fmt.Println("")
	fmt.Println("")

	for k, v := range resp.Header {
		w.Header()[k] = v
	}
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		log.Error(errors.Wrap(err))
	}
}
func needResponseBody(resp *http.Response) bool {
	switch resp.Header.Get("Content-Type") {
	case
		"application/octet-stream",
		"image/gif",
		"image/jpeg",
		"image/png",
		"font/woff":
		return false
	}
	return true
}

type gzipReadCloser struct {
	rc io.ReadCloser
	*gzip.Reader
}

func newGzipReadCloser(rc io.ReadCloser) (*gzipReadCloser, error) {
	reader, err := gzip.NewReader(rc)
	if err != nil {
		return nil, err
	}
	return &gzipReadCloser{
		rc:     rc,
		Reader: reader,
	}, nil
}

func (r *gzipReadCloser) Close() error {
	if err := r.rc.Close(); err != nil {
		return err
	}
	return r.Reader.Close()
}
